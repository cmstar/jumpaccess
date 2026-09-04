package sshupstream

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"golang.org/x/crypto/ssh"
)

type Dialer struct {
	HostKeyCallback ssh.HostKeyCallback
	Timeout         time.Duration
}

func (d Dialer) Dial(ctx context.Context, connection jumpserver.ClientConnection) (*ssh.Client, error) {
	if connection.Protocol != "ssh" && connection.Protocol != "sftp" {
		return nil, fmt.Errorf("SSH client received protocol %q", connection.Protocol)
	}
	if d.HostKeyCallback == nil {
		return nil, fmt.Errorf("SSH host key verifier is unavailable")
	}
	address := net.JoinHostPort(connection.Endpoint.Host, fmt.Sprintf("%d", connection.Endpoint.Port))
	networkDialer := net.Dialer{Timeout: d.Timeout}
	rawConnection, err := networkDialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("connect to JumpServer SSH gateway: %w", err)
	}
	stopCancellation := context.AfterFunc(ctx, func() { _ = rawConnection.Close() })
	defer stopCancellation()
	if d.Timeout > 0 {
		_ = rawConnection.SetDeadline(time.Now().Add(d.Timeout))
	}
	configuration := &ssh.ClientConfig{
		User:            connection.Username(),
		Auth:            []ssh.AuthMethod{ssh.Password(connection.Password())},
		HostKeyCallback: d.HostKeyCallback,
		Timeout:         d.Timeout,
	}
	clientConnection, channels, requests, err := ssh.NewClientConn(rawConnection, address, configuration)
	if ctx.Err() != nil {
		_ = rawConnection.Close()
		return nil, ctx.Err()
	}
	if err != nil {
		_ = rawConnection.Close()
		return nil, fmt.Errorf("establish JumpServer SSH connection: %w", err)
	}
	_ = rawConnection.SetDeadline(time.Time{})
	return ssh.NewClient(clientConnection, channels, requests), nil
}
