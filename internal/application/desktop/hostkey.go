package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

type HostKeyPrompt struct {
	ID          string `json:"id"`
	Host        string `json:"host"`
	Fingerprint string `json:"fingerprint"`
}

type HostKeyCoordinator struct {
	Emit func(HostKeyPrompt)

	mu      sync.Mutex
	pending map[string]chan bool
}

func (c *HostKeyCoordinator) Confirm(ctx context.Context, host, fingerprint string) (bool, error) {
	if c.Emit == nil {
		return false, fmt.Errorf("SSH host key prompt is unavailable")
	}
	id, err := hostKeyPromptID()
	if err != nil {
		return false, err
	}
	decision := make(chan bool, 1)
	c.mu.Lock()
	if c.pending == nil {
		c.pending = make(map[string]chan bool)
	}
	c.pending[id] = decision
	c.mu.Unlock()
	defer c.remove(id)
	c.Emit(HostKeyPrompt{ID: id, Host: host, Fingerprint: fingerprint})
	select {
	case accepted := <-decision:
		return accepted, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func (c *HostKeyCoordinator) Resolve(id string, accepted bool) error {
	c.mu.Lock()
	decision, exists := c.pending[id]
	if exists {
		delete(c.pending, id)
	}
	c.mu.Unlock()
	if !exists {
		return fmt.Errorf("SSH host key prompt %q is not pending", id)
	}
	decision <- accepted
	return nil
}

func (c *HostKeyCoordinator) remove(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func hostKeyPromptID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate SSH host key prompt ID: %w", err)
	}
	return hex.EncodeToString(value), nil
}
