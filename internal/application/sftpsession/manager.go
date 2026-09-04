package sftpsession

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	connectapp "github.com/cmstar/jumpaccess/internal/application/connect"
	"github.com/cmstar/jumpaccess/internal/target"
	"golang.org/x/crypto/ssh"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrSessionNotActive = errors.New("SFTP session is not active")

type Manager struct {
	Prepare         Preparer
	HostKeyCallback func(context.Context) (ssh.HostKeyCallback, error)
	Open            OpenFunc
	Timeout         time.Duration
	EmitState       func(StateEvent)
	EmitTransfer    func(TransferEvent)
	mu              sync.Mutex
	sessions        map[string]*managedSession
	transfers       map[string]*transfer
	transferOrder   []string
	batchChoices    map[string]string
}
type managedSession struct {
	state        StateEvent
	client       RemoteClient
	ctx          context.Context
	cancel       context.CancelFunc
	transferring bool
}

func (m *Manager) Start(parent context.Context, request StartRequest) (StateEvent, error) {
	request.Target = strings.TrimSpace(request.Target)
	if request.Target == "" {
		return StateEvent{}, fmt.Errorf("SFTP target is required")
	}
	if m.Prepare == nil || m.HostKeyCallback == nil || m.Open == nil {
		return StateEvent{}, fmt.Errorf("SFTP session service is unavailable")
	}
	id, err := newID()
	if err != nil {
		return StateEvent{}, err
	}
	ctx, cancel := context.WithCancel(parent)
	state := StateEvent{ID: id, Status: StatusConnecting, Title: request.Target, Profile: request.Profile, Organization: request.Organization, Target: request.Target, Account: request.Account}
	m.mu.Lock()
	if m.sessions == nil {
		m.sessions = make(map[string]*managedSession)
	}
	m.sessions[id] = &managedSession{state: state, ctx: ctx, cancel: cancel}
	m.mu.Unlock()
	m.emitState(state)
	go m.run(ctx, id, request)
	return state, nil
}

func (m *Manager) run(ctx context.Context, id string, request StartRequest) {
	prepared, err := m.Prepare.Prepare(ctx, connectapp.Options{Target: target.Input{Target: request.Target, Profile: request.Profile, Organization: request.Organization, Account: request.Account}, NonInteractive: true, Protocol: "sftp"})
	if err != nil {
		m.finish(id, err)
		return
	}
	callback, err := m.HostKeyCallback(ctx)
	if err != nil {
		m.finish(id, err)
		return
	}
	client, err := m.Open(ctx, OpenOptions{Connection: prepared.Connection, Asset: prepared.Asset, Account: prepared.Account, HostKeyCallback: callback, Timeout: m.Timeout})
	if err != nil {
		m.finish(id, err)
		return
	}
	directory, err := client.InitialDirectory(request.Directory)
	if err != nil {
		_ = client.Close()
		m.finish(id, err)
		return
	}
	m.mu.Lock()
	session, exists := m.sessions[id]
	if !exists || ctx.Err() != nil {
		m.mu.Unlock()
		_ = client.Close()
		return
	}
	session.client = client
	session.state.Status = StatusActive
	session.state.Profile = prepared.Selection.Profile
	session.state.Organization = prepared.Selection.Organization
	session.state.Alias = prepared.Selection.Alias
	session.state.AssetID = prepared.Asset.ID
	session.state.Asset = prepared.Asset.ID
	session.state.Permissions = accountPermissions(prepared.Account.Actions)
	session.state.AssetName = prepared.Asset.Name
	session.state.Account = prepared.Account.ID
	session.state.Directory = directory
	if session.state.Account == "" {
		session.state.Account = prepared.Account.Username
	}
	if prepared.Selection.Alias != "" {
		session.state.Title = prepared.Selection.Alias
	} else if prepared.Asset.Name != "" {
		session.state.Title = prepared.Asset.Name
	}
	state := session.state
	m.mu.Unlock()
	m.emitState(state)
	err = client.Wait()
	_ = client.Close()
	m.finish(id, err)
}

func (m *Manager) finish(id string, err error) {
	m.mu.Lock()
	session, exists := m.sessions[id]
	if !exists {
		m.mu.Unlock()
		return
	}
	if err != nil && session.ctx.Err() == nil {
		session.state.Status = StatusFailed
		session.state.Error = err.Error()
	} else {
		session.state.Status = StatusClosed
		session.state.Error = ""
	}
	session.cancel()
	transfers := m.cancelSessionTransfersLocked(id)
	session.client = nil
	state := session.state
	m.mu.Unlock()
	m.emitState(state)
	for _, event := range transfers {
		m.emitTransfer(event)
	}
}

func (m *Manager) List() []StateEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]StateEvent, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s.state)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	s, exists := m.sessions[id]
	if !exists {
		m.mu.Unlock()
		return nil
	}
	delete(m.sessions, id)
	s.cancel()
	transfers := m.cancelSessionTransfersLocked(id)
	client := s.client
	s.state.Status = StatusClosed
	s.state.Error = ""
	state := s.state
	m.mu.Unlock()
	m.emitState(state)
	for _, event := range transfers {
		m.emitTransfer(event)
	}
	if client != nil {
		return client.Close()
	}
	return nil
}
func (m *Manager) CloseAll() error {
	var err error
	for _, s := range m.List() {
		err = errors.Join(err, m.Close(s.ID))
	}
	return err
}
func (m *Manager) emitState(e StateEvent) {
	if m.EmitState != nil {
		m.EmitState(e)
	}
}
func newID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
