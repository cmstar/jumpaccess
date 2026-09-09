package sshclient

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cmstar/jumpaccess/internal/clitransfer"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

type Runner struct {
	Stdin             io.Reader
	Stdout            io.Writer
	Stderr            io.Writer
	HostKeyCallback   ssh.HostKeyCallback
	Timeout           time.Duration
	DownloadDirectory string
}

func (r Runner) Run(ctx context.Context, connection jumpserver.ClientConnection) error {
	terminal, input, err := r.terminalOptions()
	if err != nil {
		return err
	}
	stdout := r.Stdout
	var transferReader *io.PipeReader
	var transferWriter *io.PipeWriter
	if input != nil {
		transferReader, transferWriter = io.Pipe()
		defer transferReader.Close()
		defer transferWriter.Close()
		stdout = transferWriter
	}
	session, err := Open(ctx, OpenOptions{
		Connection:      connection,
		HostKeyCallback: r.HostKeyCallback,
		Timeout:         r.Timeout,
		Stdout:          stdout,
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
		transferContext, stopTransfer := context.WithCancel(ctx)
		defer stopTransfer()
		transferred := make(chan error, 1)
		go func() {
			transferred <- clitransfer.Run(transferContext, r.Stdin, r.Stdout, session, transferReader, clitransfer.Options{DownloadDirectory: r.DownloadDirectory})
		}()
		finished := make(chan error, 1)
		go func() {
			err := session.Wait()
			_ = transferWriter.Close()
			finished <- err
		}()
		select {
		case err := <-finished:
			transferErr := <-transferred
			if err != nil {
				return err
			}
			return transferErr
		case transferErr := <-transferred:
			// 解析器提前退出时，先解除 SSH 输出复制的管道阻塞，再等待会话结束。
			_ = transferReader.Close()
			_ = session.Close()
			err := <-finished
			if transferErr != nil {
				return transferErr
			}
			return err
		}
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
