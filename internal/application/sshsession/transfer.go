package sshsession

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"
)

type TransferCapabilities struct {
	Checked  bool `json:"checked"`
	Upload   bool `json:"upload"`
	Download bool `json:"download"`
}

func (m *Manager) AcknowledgeOutput(id string, sequence uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session := m.sessions[id]; session != nil && session.outputSequence == sequence && session.outputAck != nil {
		close(session.outputAck)
		session.outputAck = nil
	}
}

func (m *Manager) WriteBinary(id, data string) error {
	if len(data) > 256*1024 {
		return fmt.Errorf("SSH binary chunk is too large")
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("invalid SSH binary data")
	}
	return m.writeData(id, string(decoded), true)
}

func (m *Manager) SetTransferActive(id string, active bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.sessions[id]
	if session == nil || session.state.Status != StatusActive {
		return ErrSessionNotActive
	}
	session.transferActive = active
	return nil
}

func (m *Manager) HasActiveTransfers() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, session := range m.sessions {
		if session.state.Status == StatusActive && session.transferActive {
			return true
		}
	}
	return false
}

// ProbeTransferCommands 使用独立 exec channel；不向用户正在操作的 PTY 插入命令。
func (m *Manager) ProbeTransferCommands(ctx context.Context, id string) TransferCapabilities {
	m.mu.Lock()
	session, ok := m.sessions[id]
	if !ok || session.terminal == nil || session.state.Status != StatusActive {
		m.mu.Unlock()
		return TransferCapabilities{}
	}
	terminal := session.terminal
	m.mu.Unlock()
	probe, ok := terminal.(interface {
		ProbeTransferCommands(context.Context) (bool, bool, error)
	})
	if !ok {
		return TransferCapabilities{}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	upload, download, err := probe.ProbeTransferCommands(ctx)
	return TransferCapabilities{Checked: err == nil, Upload: err == nil && upload, Download: err == nil && download}
}
