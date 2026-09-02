package sshclient

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/cmstar/jumpaccess/internal/sshupstream"
	"golang.org/x/crypto/ssh"
)

type TerminalOptions struct {
	Name          string
	Columns, Rows int
}

type OpenOptions struct {
	Connection      jumpserver.ClientConnection
	HostKeyCallback ssh.HostKeyCallback
	Timeout         time.Duration
	Stdout          io.Writer
	Stderr          io.Writer
	Terminal        TerminalOptions
}

// Session 是已经启动 shell 的双向 SSH 会话，供 CLI 和 GUI 共同使用。
type Session struct {
	ctx       context.Context
	client    *ssh.Client
	remote    *ssh.Session
	stdin     io.WriteCloser
	finished  chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func Open(ctx context.Context, options OpenOptions) (*Session, error) {
	client, err := (sshupstream.Dialer{
		HostKeyCallback: options.HostKeyCallback,
		Timeout:         options.Timeout,
	}).Dial(ctx, options.Connection)
	if err != nil {
		return nil, err
	}
	remote, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("create SSH session: %w", err)
	}
	stdout := options.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := options.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	remote.Stdout = stdout
	remote.Stderr = stderr
	stdin, err := remote.StdinPipe()
	if err != nil {
		_ = remote.Close()
		_ = client.Close()
		return nil, fmt.Errorf("open SSH input stream: %w", err)
	}
	if err := requestTerminal(remote, options.Terminal); err != nil {
		_ = remote.Close()
		_ = client.Close()
		return nil, err
	}
	if err := remote.Shell(); err != nil {
		_ = remote.Close()
		_ = client.Close()
		return nil, fmt.Errorf("start SSH shell: %w", err)
	}
	session := &Session{
		ctx:      ctx,
		client:   client,
		remote:   remote,
		stdin:    stdin,
		finished: make(chan struct{}),
	}
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close()
		case <-session.finished:
		}
	}()
	return session, nil
}

func requestTerminal(session *ssh.Session, options TerminalOptions) error {
	if options.Columns == 0 && options.Rows == 0 {
		return nil
	}
	if options.Columns <= 0 || options.Rows <= 0 {
		return fmt.Errorf("SSH terminal dimensions must be positive")
	}
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = "xterm-256color"
	}
	if err := session.RequestPty(name, options.Rows, options.Columns, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		return fmt.Errorf("request SSH terminal: %w", err)
	}
	return nil
}

func (s *Session) Write(data []byte) (int, error) {
	return s.stdin.Write(data)
}

func (s *Session) CloseInput() error {
	return s.stdin.Close()
}

func (s *Session) Resize(columns, rows int) error {
	if columns <= 0 || rows <= 0 {
		return fmt.Errorf("SSH terminal dimensions must be positive")
	}
	if err := s.remote.WindowChange(rows, columns); err != nil {
		return fmt.Errorf("resize SSH terminal: %w", err)
	}
	return nil
}

// ProbeLatency 测量一次到 JumpServer SSH 网关的协议往返时间。
// 网关拒绝未知请求时仍然完成了应答，因此也构成有效测量。
func (s *Session) ProbeLatency() (time.Duration, error) {
	started := time.Now()
	_, _, err := s.client.SendRequest("keepalive@openssh.com", true, nil)
	if err != nil {
		return 0, fmt.Errorf("measure JumpServer SSH gateway latency: %w", err)
	}
	return time.Since(started), nil
}

func (s *Session) Wait() error {
	err := s.remote.Wait()
	_ = s.Close()
	if s.ctx.Err() != nil {
		return s.ctx.Err()
	}
	return err
}

func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		close(s.finished)
		_ = s.stdin.Close()
		s.closeErr = s.remote.Close()
		if err := s.client.Close(); s.closeErr == nil {
			s.closeErr = err
		}
	})
	return s.closeErr
}
