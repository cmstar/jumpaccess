package sshclient

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"golang.org/x/crypto/ssh"
)

func TestRunnerConnectsWithJumpServerGatewayCredentialAndStreamsSession(t *testing.T) {
	address, hostSigner, done := startSSHServer(t, "JMS-connection-1", "connection-secret")
	host, portText, _ := net.SplitHostPort(address)
	var port int
	_, _ = fmt.Sscanf(portText, "%d", &port)
	var stdout, stderr bytes.Buffer
	var verifiedHost string
	runner := Runner{
		Stdin: &bytes.Buffer{}, Stdout: &stdout, Stderr: &stderr,
		HostKeyCallback: func(hostname string, _ net.Addr, key ssh.PublicKey) error {
			verifiedHost = hostname
			if !bytes.Equal(key.Marshal(), hostSigner.PublicKey().Marshal()) {
				return fmt.Errorf("wrong host key")
			}
			return nil
		},
		Timeout: time.Second,
	}
	connection := jumpserver.ClientConnection{
		Protocol: "ssh", Endpoint: jumpserver.Endpoint{Host: host, Port: port},
		Token: jumpserver.ConnectionCredential{ID: "connection-1", Value: "connection-secret"},
	}

	if err := runner.Run(context.Background(), connection); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if stdout.String() != "connected\n" || stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
	if verifiedHost != address {
		t.Fatalf("verified host = %q, want %q", verifiedHost, address)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func startSSHServer(t *testing.T, username, password string) (string, ssh.Signer, <-chan error) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	configuration := &ssh.ServerConfig{
		PasswordCallback: func(metadata ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if metadata.User() != username || string(pass) != password {
				return nil, fmt.Errorf("authentication rejected")
			}
			return nil, nil
		},
	}
	configuration.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		defer listener.Close()
		connection, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		_, channels, requests, err := ssh.NewServerConn(connection, configuration)
		if err != nil {
			done <- err
			return
		}
		go ssh.DiscardRequests(requests)
		for channelRequest := range channels {
			if channelRequest.ChannelType() != "session" {
				_ = channelRequest.Reject(ssh.UnknownChannelType, "session only")
				continue
			}
			channel, requests, err := channelRequest.Accept()
			if err != nil {
				done <- err
				return
			}
			for request := range requests {
				if request.Type != "shell" {
					_ = request.Reply(false, nil)
					continue
				}
				_ = request.Reply(true, nil)
				_, _ = channel.Write([]byte("connected\n"))
				status := make([]byte, 4)
				binary.BigEndian.PutUint32(status, 0)
				_, _ = channel.SendRequest("exit-status", false, status)
				_ = channel.Close()
			}
			done <- nil
			return
		}
	}()
	return listener.Addr().String(), signer, done
}
