package sftpsession

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"
)

var errSkipped = errors.New("SFTP entry skipped")

type recoveryError struct{ error }

type TransferRequest struct {
	SessionID   string   `json:"sessionId"`
	Direction   string   `json:"direction"`
	Sources     []string `json:"sources"`
	Destination string   `json:"destination"`
}
type Conflict struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}
type TransferEvent struct {
	ID          string    `json:"id"`
	BatchID     string    `json:"batchId"`
	SessionID   string    `json:"sessionId"`
	Direction   string    `json:"direction"`
	Name        string    `json:"name"`
	Source      string    `json:"source"`
	Destination string    `json:"destination"`
	Status      string    `json:"status"`
	Transferred int64     `json:"transferred"`
	Total       int64     `json:"total"`
	Error       string    `json:"error"`
	Conflict    *Conflict `json:"conflict,omitempty"`
}
type transfer struct {
	event           TransferEvent
	ctx             context.Context
	cancel          context.CancelFunc
	destinationRoot string
	choice          chan string
	lastProgress    time.Time
}

func (m *Manager) StartTransfer(request TransferRequest) ([]TransferEvent, error) {
	if request.Direction == "download" && request.Destination == "" {
		return nil, fmt.Errorf("SFTP download destination is required")
	}
	if request.Direction != "upload" && request.Direction != "download" {
		return nil, fmt.Errorf("invalid SFTP transfer direction")
	}
	if len(request.Sources) == 0 {
		return nil, fmt.Errorf("SFTP transfer sources are required")
	}
	if err := m.authorize(request.SessionID, request.Direction); err != nil {
		return nil, err
	}
	_, remoteDest, err := m.remote(request.SessionID, request.Destination)
	if err != nil {
		return nil, err
	}
	batchID, err := newID()
	if err != nil {
		return nil, err
	}
	jobs := make([]*transfer, 0, len(request.Sources))
	for _, source := range request.Sources {
		if source == "" {
			return nil, fmt.Errorf("SFTP transfer source is empty")
		}
		id, err := newID()
		if err != nil {
			return nil, err
		}
		destination := remoteDest
		name := path.Base(source)
		if request.Direction == "upload" {
			source, err = filepath.Abs(source)
			if err != nil {
				return nil, err
			}
			name = filepath.Base(source)
		} else {
			_, source, err = m.remote(request.SessionID, source)
			if err != nil {
				return nil, err
			}
			destination, err = filepath.Abs(request.Destination)
			if err != nil {
				return nil, err
			}
		}
		dest := path.Join(destination, name)
		if request.Direction == "download" {
			dest = filepath.Join(destination, name)
		}
		jobs = append(jobs, &transfer{event: TransferEvent{ID: id, BatchID: batchID, SessionID: request.SessionID, Direction: request.Direction, Name: name, Source: source, Destination: dest, Status: "queued"}, destinationRoot: destination, choice: make(chan string, 1)})
	}
	m.mu.Lock()
	s, ok := m.sessions[request.SessionID]
	if !ok || s.state.Status != StatusActive {
		m.mu.Unlock()
		return nil, ErrSessionNotActive
	}
	if m.transfers == nil {
		m.transfers = make(map[string]*transfer)
	}
	result := make([]TransferEvent, 0, len(jobs))
	for _, job := range jobs {
		job.ctx, job.cancel = context.WithCancel(s.ctx)
		m.transfers[job.event.ID] = job
		m.transferOrder = append(m.transferOrder, job.event.ID)
		result = append(result, job.event)
	}
	launch := !s.transferring
	s.transferring = true
	m.mu.Unlock()
	for _, event := range result {
		m.emitTransfer(event)
	}
	if launch {
		go m.runQueue(request.SessionID)
	}
	return result, nil
}
func (m *Manager) runQueue(sessionID string) {
	for {
		m.mu.Lock()
		s, ok := m.sessions[sessionID]
		if !ok {
			m.mu.Unlock()
			return
		}
		var job *transfer
		for _, id := range m.transferOrder {
			candidate := m.transfers[id]
			if candidate != nil && candidate.event.SessionID == sessionID && candidate.event.Status == "queued" {
				job = candidate
				break
			}
		}
		if job == nil {
			s.transferring = false
			m.mu.Unlock()
			return
		}
		client := s.client
		job.event.Status = "running"
		event := job.event
		m.mu.Unlock()
		m.emitTransfer(event)
		var err error
		if job.event.Direction == "upload" {
			err = m.uploadFile(job, client)
		} else {
			err = m.downloadFile(job, client)
		}
		m.mu.Lock()
		var recovery *recoveryError
		if errors.As(err, &recovery) {
			job.event.Status = "failed"
			job.event.Error = err.Error()
		} else if job.ctx.Err() != nil {
			job.event.Status = "cancelled"
			job.event.Error = ""
		} else if errors.Is(err, errSkipped) {
			job.event.Status = "skipped"
		} else if err != nil {
			job.event.Status = "failed"
			job.event.Error = err.Error()
		} else {
			job.event.Status = "completed"
		}
		job.event.Conflict = nil
		job.cancel()
		event = job.event
		m.mu.Unlock()
		m.emitTransfer(event)
	}
}
func (m *Manager) uploadFile(job *transfer, client RemoteClient) error {
	if client == nil {
		return ErrSessionNotActive
	}
	return m.uploadTree(job, client, job.event.Source, job.event.Destination)
}
func (m *Manager) uploadTree(job *transfer, client RemoteClient, sourcePath, destination string) error {
	if err := job.ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errSkipped
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return fmt.Errorf("SFTP upload requires a regular file or directory")
	}
	// Windows 的 Lstat 延迟解析文件 ID，必须在等待冲突选择前固定身份。
	if !os.SameFile(info, info) {
		return fmt.Errorf("cannot determine local upload source identity")
	}
	overwrite := false
	existing, statErr := client.Lstat(destination)
	if statErr == nil && !(info.IsDir() && existing.IsDir() && existing.Mode()&os.ModeSymlink == 0) {
		choice, err := m.awaitConflict(job, sourcePath, destination)
		if err != nil {
			return err
		}
		if choice == "skip" {
			return errSkipped
		}
		if choice == "keep-both" {
			name, err := unusedName(destination, client.Lstat)
			if err != nil {
				return err
			}
			destination = name
			existing = nil
			if sourcePath == job.event.Source {
				m.mu.Lock()
				job.event.Destination = name
				m.mu.Unlock()
			}
		}
		overwrite = choice == "overwrite"
		if overwrite && (info.IsDir() || existing.IsDir() || existing.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("cannot overwrite SFTP entries with incompatible types")
		}
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	openedInfo, err := source.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, openedInfo) {
		return fmt.Errorf("local upload source changed before it could be read")
	}
	stopSource := abortOnCancel(job.ctx, source)
	defer stopSource()
	if info.IsDir() {
		if existing == nil {
			if err := client.Mkdir(destination); err != nil {
				return err
			}
		}
		entries, err := source.ReadDir(-1)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !validRemoteName(entry.Name()) {
				return fmt.Errorf("invalid upload filename")
			}
			if err := m.uploadTree(job, client, filepath.Join(sourcePath, entry.Name()), path.Join(destination, entry.Name())); err != nil && !errors.Is(err, errSkipped) {
				return err
			}
		}
		return nil
	}
	temp := path.Join(path.Dir(destination), ".jumpaccess-"+job.event.ID+".part")
	dest, err := client.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	defer client.Remove(temp)
	stopDest := abortOnCancel(job.ctx, dest)
	defer stopDest()
	m.mu.Lock()
	job.event.Total += info.Size()
	m.mu.Unlock()
	_, copyErr := m.copyStream(job, dest, source)
	closeErr := dest.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := job.ctx.Err(); err != nil {
		return err
	}
	if overwrite {
		return m.replaceRemoteFile(job, client, temp, destination)
	}
	return client.Rename(temp, destination)
}

func (m *Manager) replaceRemoteFile(job *transfer, client RemoteClient, temp, destination string) error {
	if atomic, ok := client.(interface{ PosixRename(string, string) error }); ok {
		if err := atomic.PosixRename(temp, destination); err == nil {
			return nil
		}
	}
	if err := job.ctx.Err(); err != nil {
		return err
	}
	if err := m.authorize(job.event.SessionID, "delete"); err != nil {
		return fmt.Errorf("safe SFTP replacement requires delete permission: %w", err)
	}
	for _, p := range []string{temp, destination} {
		info, err := client.Lstat(p)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("SFTP replacement requires regular files")
		}
	}
	backupID, err := newID()
	if err != nil {
		return err
	}
	backup := path.Join(path.Dir(destination), ".jumpaccess-"+backupID+".backup")
	if _, err := client.Lstat(backup); err == nil {
		return fmt.Errorf("SFTP backup filename already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := client.Rename(destination, backup); err != nil {
		return err
	}
	if err := client.Rename(temp, destination); err != nil {
		if _, statErr := client.Lstat(destination); !errors.Is(statErr, os.ErrNotExist) {
			return &recoveryError{fmt.Errorf("SFTP replacement failed; original file remains at %q: %w", backup, err)}
		}
		if restoreErr := client.Rename(backup, destination); restoreErr != nil {
			return &recoveryError{fmt.Errorf("SFTP replacement failed and original file could not be restored from %q: %w", backup, errors.Join(err, restoreErr))}
		}
		return err
	}
	if err := client.Remove(backup); err != nil {
		return &recoveryError{fmt.Errorf("SFTP replacement completed but previous file remains at %q: %w", backup, err)}
	}
	return nil
}
func (m *Manager) copyStream(job *transfer, dst io.Writer, src io.Reader) (int64, error) {
	buffer := make([]byte, 64*1024)
	var copied int64
	for {
		if err := job.ctx.Err(); err != nil {
			return copied, err
		}
		n, readErr := src.Read(buffer)
		if n > 0 {
			written, err := dst.Write(buffer[:n])
			copied += int64(written)
			m.mu.Lock()
			job.event.Transferred += int64(written)
			emit := time.Since(job.lastProgress) >= 100*time.Millisecond
			if emit {
				job.lastProgress = time.Now()
			}
			event := job.event
			m.mu.Unlock()
			if emit {
				m.emitTransfer(event)
			}
			if err != nil {
				return copied, err
			}
			if written != n {
				return copied, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return copied, nil
		}
		if readErr != nil {
			return copied, readErr
		}
	}
}
func (m *Manager) emitTransfer(e TransferEvent) {
	if m.EmitTransfer != nil {
		m.EmitTransfer(e)
	}
}
func (m *Manager) ListTransfers(sessionID string) []TransferEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]TransferEvent, 0)
	for _, id := range m.transferOrder {
		if t := m.transfers[id]; t != nil && (sessionID == "" || t.event.SessionID == sessionID) {
			result = append(result, t.event)
		}
	}
	return result
}
func (m *Manager) CancelTransfer(id string) error {
	m.mu.Lock()
	job := m.transfers[id]
	if job == nil {
		m.mu.Unlock()
		return fmt.Errorf("SFTP transfer does not exist")
	}
	job.cancel()
	emit := job.event.Status == "queued"
	if emit {
		job.event.Status = "cancelled"
	}
	event := job.event
	m.mu.Unlock()
	if emit {
		m.emitTransfer(event)
	}
	return nil
}
func (m *Manager) RetryTransfer(id string) (TransferEvent, error) {
	m.mu.Lock()
	job := m.transfers[id]
	if job == nil || (job.event.Status != "failed" && job.event.Status != "cancelled") {
		m.mu.Unlock()
		return TransferEvent{}, fmt.Errorf("SFTP transfer cannot be retried")
	}
	s, ok := m.sessions[job.event.SessionID]
	if !ok || s.state.Status != StatusActive {
		m.mu.Unlock()
		return TransferEvent{}, ErrSessionNotActive
	}
	job.ctx, job.cancel = context.WithCancel(s.ctx)
	job.choice = make(chan string, 1)
	job.event.Status = "queued"
	job.event.Transferred = 0
	job.event.Total = 0
	job.event.Error = ""
	job.event.Conflict = nil
	job.lastProgress = time.Time{}
	delete(m.batchChoices, job.event.BatchID)
	event := job.event
	launch := !s.transferring
	s.transferring = true
	m.mu.Unlock()
	m.emitTransfer(event)
	if launch {
		go m.runQueue(event.SessionID)
	}
	return event, nil
}
func (m *Manager) ResolveConflict(id, choice string, applyToBatch bool) error {
	if choice != "skip" && choice != "overwrite" && choice != "keep-both" {
		return fmt.Errorf("invalid SFTP conflict choice")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.transfers[id]
	if job == nil || job.event.Status != "conflict" {
		return fmt.Errorf("SFTP transfer is not waiting for a conflict choice")
	}
	if applyToBatch {
		if m.batchChoices == nil {
			m.batchChoices = make(map[string]string)
		}
		m.batchChoices[job.event.BatchID] = choice
	}
	select {
	case job.choice <- choice:
		return nil
	default:
		return fmt.Errorf("SFTP conflict choice has already been submitted")
	}
}

func (m *Manager) awaitConflict(job *transfer, source, destination string) (string, error) {
	m.mu.Lock()
	if choice := m.batchChoices[job.event.BatchID]; choice != "" {
		m.mu.Unlock()
		return choice, nil
	}
	job.event.Status = "conflict"
	job.event.Conflict = &Conflict{Source: source, Destination: destination}
	event := job.event
	m.mu.Unlock()
	m.emitTransfer(event)
	var choice string
	select {
	case <-job.ctx.Done():
		return "", job.ctx.Err()
	case choice = <-job.choice:
	}
	m.mu.Lock()
	job.event.Status = "running"
	job.event.Conflict = nil
	event = job.event
	m.mu.Unlock()
	m.emitTransfer(event)
	return choice, nil
}

func unusedName(original string, lstat func(string) (os.FileInfo, error)) (string, error) {
	ext := path.Ext(original)
	base := original[:len(original)-len(ext)]
	for n := 1; n < 10000; n++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, n, ext)
		if _, err := lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("no available SFTP destination filename")
}
func (m *Manager) ClearCompleted(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	order := m.transferOrder[:0]
	for _, id := range m.transferOrder {
		job := m.transfers[id]
		if job == nil {
			continue
		}
		if (sessionID == "" || job.event.SessionID == sessionID) && (job.event.Status == "completed" || job.event.Status == "skipped" || job.event.Status == "cancelled") {
			delete(m.transfers, id)
		} else {
			order = append(order, id)
		}
	}
	m.transferOrder = order
}
func (m *Manager) HasActiveTransfers() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, job := range m.transfers {
		if activeTransfer(job.event.Status) {
			return true
		}
	}
	return false
}
func activeTransfer(status string) bool {
	return status == "queued" || status == "running" || status == "conflict"
}

func abortOnCancel(ctx context.Context, file io.Closer) func() bool {
	return context.AfterFunc(ctx, func() {
		if abortable, ok := file.(interface{ Abort() error }); ok {
			_ = abortable.Abort()
		} else {
			_ = file.Close()
		}
	})
}

func (m *Manager) cancelSessionTransfersLocked(sessionID string) []TransferEvent {
	var events []TransferEvent
	for _, job := range m.transfers {
		if job.event.SessionID != sessionID || !activeTransfer(job.event.Status) {
			continue
		}
		job.cancel()
		if job.event.Status == "queued" {
			job.event.Status = "cancelled"
			job.event.Error = ""
			events = append(events, job.event)
		}
	}
	return events
}
