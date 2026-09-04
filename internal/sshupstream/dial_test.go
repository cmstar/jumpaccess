package sshupstream

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"golang.org/x/crypto/ssh"
)

func TestDialCancellationInterruptsSSHHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		t.Cleanup(func() { conn.Close() })
		buffer := make([]byte, 1024)
		for {
			if _, err := conn.Read(buffer); err != nil {
				return
			}
		}
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		client, err := (Dialer{Timeout: 3 * time.Second, HostKeyCallback: func(string, net.Addr, ssh.PublicKey) error { return nil }}).Dial(ctx, jumpserver.ClientConnection{Protocol: "sftp", Endpoint: jumpserver.Endpoint{Host: host, Port: port}})
		if client != nil {
			client.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled handshake succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("SSH handshake ignored context cancellation")
	}
}
