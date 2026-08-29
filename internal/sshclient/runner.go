package sshclient

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
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
	terminal, input, err := r.terminalOptions()
	if err != nil {
		return err
	}
	session, err := Open(ctx, OpenOptions{
		Connection:      connection,
		HostKeyCallback: r.HostKeyCallback,
		Timeout:         r.Timeout,
		Stdout:          r.Stdout,
		Stderr:          r.Stderr,
		Terminal:        terminal,
	})
	if err != nil {
		return err
	}
	defer session.Close()
	if input != nil {
		state, err := term.MakeRaw(int(input.Fd()))
		if err != nil {
			return fmt.Errorf("enable raw terminal mode: %w", err)
		}
		defer func() { _ = term.Restore(int(input.Fd()), state) }()
	}
	if r.Stdin != nil {
		go func() {
			_, _ = io.Copy(session, r.Stdin)
			_ = session.CloseInput()
		}()
	}
	return session.Wait()
}

func (r Runner) terminalOptions() (TerminalOptions, *os.File, error) {
	input, inputOK := r.Stdin.(*os.File)
	output, outputOK := r.Stdout.(*os.File)
	if !inputOK || !outputOK || !term.IsTerminal(int(input.Fd())) || !term.IsTerminal(int(output.Fd())) {
		return TerminalOptions{}, nil, nil
	}
	width, height, err := term.GetSize(int(output.Fd()))
	if err != nil {
		return TerminalOptions{}, nil, fmt.Errorf("read terminal size: %w", err)
	}
	terminalName := strings.TrimSpace(os.Getenv("TERM"))
	if terminalName == "" {
		terminalName = "xterm-256color"
	}
	return TerminalOptions{Name: terminalName, Columns: width, Rows: height}, input, nil
}
