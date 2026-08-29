package sshsession

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	connectapp "github.com/cmstar/jumpaccess/internal/application/connect"
	"github.com/cmstar/jumpaccess/internal/sshclient"
	"github.com/cmstar/jumpaccess/internal/target"
	"golang.org/x/crypto/ssh"
)

const (
	StatusConnecting = "connecting"
	StatusActive     = "active"
	StatusClosed     = "closed"
	StatusFailed     = "failed"
)

var ErrSessionNotActive = errors.New("SSH session is not active")

type StartRequest struct {
	Profile      string `json:"profile"`
	Organization string `json:"organization"`
	Target       string `json:"target"`
	Account      string `json:"account"`
	Columns      int    `json:"columns"`
	Rows         int    `json:"rows"`
}

type StateEvent struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Title        string `json:"title"`
	Profile      string `json:"profile"`
	Organization string `json:"organization"`
	Asset        string `json:"asset"`
	Account      string `json:"account"`
	Error        string `json:"error"`
}

type OutputEvent struct {
	ID   string `json:"id"`
	Data string `json:"data"`
}

type Preparer interface {
	Prepare(context.Context, connectapp.Options) (connectapp.Prepared, error)
}

type TerminalSession interface {
	io.WriteCloser
	Resize(columns, rows int) error
	Wait() error
}

type OpenFunc func(context.Context, sshclient.OpenOptions) (TerminalSession, error)

type Manager struct {
	Prepare         Preparer
	HostKeyCallback func(context.Context) (ssh.HostKeyCallback, error)
	Open            OpenFunc
	Timeout         time.Duration
	EmitState       func(StateEvent)
	EmitOutput      func(OutputEvent)
	BatchInterval   time.Duration
	BatchSize       int

	mu       sync.Mutex
	sessions map[string]*managedSession
}

type managedSession struct {
	state    StateEvent
	cancel   context.CancelFunc
	terminal TerminalSession
	output   *batchWriter
	dismiss  bool
}

func (m *Manager) Start(parent context.Context, request StartRequest) (StateEvent, error) {
	request.Target = strings.TrimSpace(request.Target)
	if request.Target == "" {
		return StateEvent{}, fmt.Errorf("SSH target is required")
	}
	if request.Columns <= 0 || request.Rows <= 0 {
		return StateEvent{}, fmt.Errorf("SSH terminal dimensions must be positive")
	}
	if m.Prepare == nil || m.HostKeyCallback == nil || m.Open == nil {
		return StateEvent{}, fmt.Errorf("SSH session service is unavailable")
	}
	id, err := newSessionID()
	if err != nil {
		return StateEvent{}, err
	}
	ctx, cancel := context.WithCancel(parent)
	state := StateEvent{
		ID: id, Status: StatusConnecting, Title: request.Target,
		Profile: request.Profile, Organization: request.Organization, Asset: request.Target, Account: request.Account,
	}
	m.mu.Lock()
	if m.sessions == nil {
		m.sessions = make(map[string]*managedSession)
	}
	m.sessions[id] = &managedSession{state: state, cancel: cancel}
	m.mu.Unlock()
	m.emitState(state)
	go m.run(ctx, id, request)
	return state, nil
}

func (m *Manager) run(ctx context.Context, id string, request StartRequest) {
	prepared, err := m.Prepare.Prepare(ctx, connectapp.Options{
		Target: target.Input{
			Target: request.Target, Profile: request.Profile,
			Organization: request.Organization, Account: request.Account,
		},
		NonInteractive: true,
	})
	if err != nil {
		m.finish(id, ctx, err)
		return
	}
	callback, err := m.HostKeyCallback(ctx)
	if err != nil {
		m.finish(id, ctx, err)
		return
	}
	output := newBatchWriter(m.batchInterval(), m.batchSize(), func(data string) {
		m.emitOutput(OutputEvent{ID: id, Data: data})
	})
	terminal, err := m.Open(ctx, sshclient.OpenOptions{
		Connection:      prepared.Connection,
		HostKeyCallback: callback,
		Timeout:         m.Timeout,
		Stdout:          output,
		Stderr:          output,
		Terminal: sshclient.TerminalOptions{
			Name: "xterm-256color", Columns: request.Columns, Rows: request.Rows,
		},
	})
	if err != nil {
		_ = output.Close()
		m.finish(id, ctx, err)
		return
	}
	state, active := m.activate(id, prepared, terminal, output)
	if !active {
		_ = terminal.Close()
		_ = output.Close()
		m.finish(id, ctx, ctx.Err())
		return
	}
	m.emitState(state)
	err = terminal.Wait()
	_ = output.Close()
	m.finish(id, ctx, err)
}

func (m *Manager) activate(id string, prepared connectapp.Prepared, terminal TerminalSession, output *batchWriter) (StateEvent, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, exists := m.sessions[id]
	if !exists || session.state.Status != StatusConnecting {
		return StateEvent{}, false
	}
	session.terminal = terminal
	session.output = output
	session.state.Status = StatusActive
	session.state.Profile = prepared.Selection.Profile
	session.state.Organization = prepared.Selection.Organization
	session.state.Asset = prepared.Asset.ID
	session.state.Account = prepared.Account.ID
	if session.state.Account == "" {
		session.state.Account = prepared.Account.Username
	}
	if prepared.Selection.Alias != "" {
		session.state.Title = prepared.Selection.Alias
	} else if prepared.Asset.Name != "" {
		session.state.Title = prepared.Asset.Name
	}
	return session.state, true
}

func (m *Manager) finish(id string, ctx context.Context, err error) {
	m.mu.Lock()
	session, exists := m.sessions[id]
	if !exists {
		m.mu.Unlock()
		return
	}
	if ctx.Err() != nil || err == nil {
		session.state.Status = StatusClosed
		session.state.Error = ""
	} else {
		session.state.Status = StatusFailed
		session.state.Error = err.Error()
	}
	session.terminal = nil
	session.output = nil
	state := session.state
	if session.dismiss {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	m.emitState(state)
}

func (m *Manager) Write(id, data string) error {
	m.mu.Lock()
	session, exists := m.sessions[id]
	if !exists || session.state.Status != StatusActive || session.terminal == nil {
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrSessionNotActive, id)
	}
	terminal := session.terminal
	m.mu.Unlock()
	_, err := terminal.Write([]byte(data))
	return err
}

func (m *Manager) Resize(id string, columns, rows int) error {
	if columns <= 0 || rows <= 0 {
		return fmt.Errorf("SSH terminal dimensions must be positive")
	}
	m.mu.Lock()
	session, exists := m.sessions[id]
	if !exists || session.state.Status != StatusActive || session.terminal == nil {
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrSessionNotActive, id)
	}
	terminal := session.terminal
	m.mu.Unlock()
	return terminal.Resize(columns, rows)
}

func (m *Manager) Close(id string) error {
	m.mu.Lock()
	session, exists := m.sessions[id]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("SSH session %q does not exist", id)
	}
	if session.state.Status == StatusClosed || session.state.Status == StatusFailed {
		delete(m.sessions, id)
		m.mu.Unlock()
		return nil
	}
	session.dismiss = true
	session.cancel()
	terminal := session.terminal
	m.mu.Unlock()
	if terminal != nil {
		return terminal.Close()
	}
	return nil
}

func (m *Manager) CloseAll() error {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	var result error
	for _, id := range ids {
		result = errors.Join(result, m.Close(id))
	}
	return result
}

func (m *Manager) List() []StateEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]StateEvent, 0, len(m.sessions))
	for _, session := range m.sessions {
		result = append(result, session.state)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].ID < result[right].ID })
	return result
}

func (m *Manager) emitState(event StateEvent) {
	if m.EmitState != nil {
		m.EmitState(event)
	}
}

func (m *Manager) emitOutput(event OutputEvent) {
	if m.EmitOutput != nil {
		m.EmitOutput(event)
	}
}

func (m *Manager) batchInterval() time.Duration {
	if m.BatchInterval > 0 {
		return m.BatchInterval
	}
	return 16 * time.Millisecond
}

func (m *Manager) batchSize() int {
	if m.BatchSize > 0 {
		return m.BatchSize
	}
	return 32 * 1024
}

func newSessionID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate SSH session ID: %w", err)
	}
	return hex.EncodeToString(value), nil
}

type batchWriter struct {
	mu       sync.Mutex
	data     []byte
	timer    *time.Timer
	interval time.Duration
	limit    int
	emit     func(string)
	closed   bool
}

func newBatchWriter(interval time.Duration, limit int, emit func(string)) *batchWriter {
	return &batchWriter{interval: interval, limit: limit, emit: emit}
}

func (w *batchWriter) Write(value []byte) (int, error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	w.data = append(w.data, value...)
	flush := len(w.data) >= w.limit
	if flush {
		data := w.takeLocked()
		w.mu.Unlock()
		w.emitData(data)
		return len(value), nil
	}
	if w.timer == nil {
		w.timer = time.AfterFunc(w.interval, w.flush)
	}
	w.mu.Unlock()
	return len(value), nil
}

func (w *batchWriter) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	data := w.takeLocked()
	w.mu.Unlock()
	w.emitData(data)
	return nil
}

func (w *batchWriter) flush() {
	w.mu.Lock()
	data := w.takeLocked()
	w.mu.Unlock()
	w.emitData(data)
}

func (w *batchWriter) takeLocked() []byte {
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	data := w.data
	w.data = nil
	return data
}

func (w *batchWriter) emitData(data []byte) {
	if len(data) > 0 && w.emit != nil {
		w.emit(string(data))
	}
}
