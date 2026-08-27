package sshclient

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/cmstar/jumpaccess/internal/sshupstream"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

type Runner struct {
	Stdin           io.Reader
	Stdout          io.Writer
	Stderr          io.Writer
	HostKeyCallback ssh.HostKeyCallback
	Timeout         time.Duration
}

func (r Runner) Run(ctx context.Context, connection jumpserver.ClientConnection) error {
	client, err := (sshupstream.Dialer{HostKeyCallback: r.HostKeyCallback, Timeout: r.Timeout}).Dial(ctx, connection)
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("create SSH session: %w", err)
	}
	defer session.Close()
	session.Stdin = r.Stdin
	session.Stdout = r.Stdout
	session.Stderr = r.Stderr

	restore, err := r.configureTerminal(session)
	if err != nil {
		return err
	}
	defer restore()
	if err := session.Shell(); err != nil {
		return fmt.Errorf("start SSH shell: %w", err)
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = client.Close()
		case <-done:
		}
	}()
	err = session.Wait()
	close(done)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	return nil
}

func (r Runner) configureTerminal(session *ssh.Session) (func(), error) {
	input, inputOK := r.Stdin.(*os.File)
	output, outputOK := r.Stdout.(*os.File)
	if !inputOK || !outputOK || !term.IsTerminal(int(input.Fd())) || !term.IsTerminal(int(output.Fd())) {
		return func() {}, nil
	}
	width, height, err := term.GetSize(int(output.Fd()))
	if err != nil {
		return nil, fmt.Errorf("read terminal size: %w", err)
	}
	terminalName := strings.TrimSpace(os.Getenv("TERM"))
	if terminalName == "" {
		terminalName = "xterm-256color"
	}
	if err := session.RequestPty(terminalName, height, width, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		return nil, fmt.Errorf("request SSH terminal: %w", err)
	}
	state, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return nil, fmt.Errorf("enable raw terminal mode: %w", err)
	}
	return func() { _ = term.Restore(int(input.Fd()), state) }, nil
}
