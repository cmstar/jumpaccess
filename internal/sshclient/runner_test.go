package sshclient

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"golang.org/x/crypto/ssh"
)

func TestOpenStreamsInputOutputAndResizesTerminal(t *testing.T) {
	address, hostSigner, requests, done := startInteractiveSSHServer(t, "JMS-connection-2", "connection-secret")
	host, portText, _ := net.SplitHostPort(address)
	var port int
	_, _ = fmt.Sscanf(portText, "%d", &port)
	var stdout, stderr bytes.Buffer
	callback := func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if !bytes.Equal(key.Marshal(), hostSigner.PublicKey().Marshal()) {
			return fmt.Errorf("wrong host key")
		}
		return nil
	}

	session, err := Open(context.Background(), OpenOptions{
		Connection: jumpserver.ClientConnection{
			Protocol: "ssh", Endpoint: jumpserver.Endpoint{Host: host, Port: port},
			Token: jumpserver.ConnectionCredential{ID: "connection-2", Value: "connection-secret"},
		},
		HostKeyCallback: callback,
		Timeout:         time.Second,
		Stdout:          &stdout,
		Stderr:          &stderr,
		Terminal:        TerminalOptions{Name: "xterm-256color", Columns: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer session.Close()
	if _, err := session.Write([]byte("whoami\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if err := session.Resize(120, 36); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}
	if err := session.Wait(); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}

	if stdout.String() != "echo:whoami\n" || stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
	gotRequests := <-requests
	if gotRequests.Term != "xterm-256color" || gotRequests.InitialColumns != 80 || gotRequests.InitialRows != 24 {
		t.Fatalf("PTY request = %#v", gotRequests)
	}
	if gotRequests.ResizedColumns != 120 || gotRequests.ResizedRows != 36 {
		t.Fatalf("resize request = %#v", gotRequests)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type terminalRequests struct {
	Term                        string
	InitialColumns, InitialRows uint32
	ResizedColumns, ResizedRows uint32
}

type ptyRequest struct {
	Term                      string
	Columns, Rows             uint32
	WidthPixels, HeightPixels uint32
	Modes                     string
}

type windowChangeRequest struct {
	Columns, Rows             uint32
	WidthPixels, HeightPixels uint32
}

func startInteractiveSSHServer(t *testing.T, username, password string) (string, ssh.Signer, <-chan terminalRequests, <-chan error) {
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
	requestsResult := make(chan terminalRequests, 1)
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
		channelRequest := <-channels
		channel, channelRequests, err := channelRequest.Accept()
		if err != nil {
			done <- err
			return
		}
		var got terminalRequests
		for request := range channelRequests {
			switch request.Type {
			case "pty-req":
				var value ptyRequest
				if err := ssh.Unmarshal(request.Payload, &value); err != nil {
					done <- err
					return
				}
				got.Term, got.InitialColumns, got.InitialRows = value.Term, value.Columns, value.Rows
				_ = request.Reply(true, nil)
			case "shell":
				_ = request.Reply(true, nil)
			case "window-change":
				var value windowChangeRequest
				if err := ssh.Unmarshal(request.Payload, &value); err != nil {
					done <- err
					return
				}
				got.ResizedColumns, got.ResizedRows = value.Columns, value.Rows
				input := make([]byte, len("whoami\n"))
				if _, err := io.ReadFull(channel, input); err != nil {
					done <- err
					return
				}
				_, _ = channel.Write([]byte("echo:" + string(input)))
				status := make([]byte, 4)
				binary.BigEndian.PutUint32(status, 0)
				_, _ = channel.SendRequest("exit-status", false, status)
				_ = channel.Close()
				requestsResult <- got
				done <- nil
				return
			default:
				_ = request.Reply(false, nil)
			}
		}
	}()
	return listener.Addr().String(), signer, requestsResult, done
}

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
