package sshproxy

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/cmstar/jumpaccess/internal/sshupstream"
	"golang.org/x/crypto/ssh"
)

func TestServerBridgesSessionRequestsStreamsAndExitStatus(t *testing.T) {
	address, upstreamSigner, upstreamDone := startUpstreamServer(t, true)
	host, portText, _ := net.SplitHostPort(address)
	var port int
	_, _ = fmt.Sscanf(portText, "%d", &port)
	upstream, err := (sshupstream.Dialer{
		HostKeyCallback: ssh.FixedHostKey(upstreamSigner.PublicKey()), Timeout: time.Second,
	}).Dial(context.Background(), jumpserver.ClientConnection{
		Protocol: "ssh", Endpoint: jumpserver.Endpoint{Host: host, Port: port},
		Token: jumpserver.ConnectionCredential{ID: "connection-1", Value: "connection-secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	localSigner := newSigner(t)
	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer proxyListener.Close()
	proxyDone := make(chan error, 1)
	connected := make(chan struct{})
	go func() {
		serverSide, acceptErr := proxyListener.Accept()
		if acceptErr != nil {
			proxyDone <- acceptErr
			return
		}
		proxyDone <- (Server{OnConnected: func() { close(connected) }}).Run(context.Background(), serverSide, localSigner, upstream)
	}()
	clientSide, err := net.Dial("tcp", proxyListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	outerConfig := &ssh.ClientConfig{
		User:            "outer-user-is-irrelevant",
		HostKeyCallback: ssh.FixedHostKey(localSigner.PublicKey()),
		Timeout:         time.Second,
	}
	outerConnection, channels, requests, err := ssh.NewClientConn(clientSide, "jumpaccess", outerConfig)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("OnConnected was not called after the outer SSH handshake")
	}
	outer := ssh.NewClient(outerConnection, channels, requests)
	if ok, _, err := outer.SendRequest("keepalive@openssh.com", true, nil); err != nil || !ok {
		t.Fatalf("keepalive = %v, %v", ok, err)
	}
	if channel, _, err := outer.OpenChannel("direct-tcpip", nil); err == nil {
		_ = channel.Close()
		t.Fatal("direct-tcpip channel was accepted")
	}

	session, err := outer.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	if err := session.Setenv("LANG", "C.UTF-8"); err != nil {
		t.Fatal(err)
	}
	if err := session.RequestPty("xterm", 24, 80, nil); err != nil {
		t.Fatal(err)
	}
	if err := session.Shell(); err != nil {
		t.Fatal(err)
	}
	err = session.Wait()
	var exitError *ssh.ExitError
	if !errors.As(err, &exitError) || exitError.ExitStatus() != 7 {
		t.Fatalf("Wait error = %v", err)
	}
	if stdout.String() != "upstream stdout\n" || stderr.String() != "upstream stderr\n" {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
	_ = outer.Close()
	select {
	case err := <-proxyDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy did not stop when outer client closed")
	}
	_ = upstream.Close()
	if err := <-upstreamDone; err != nil {
		t.Fatal(err)
	}
}

func TestServerCancellationClosesActiveUpstreamSession(t *testing.T) {
	address, upstreamSigner, upstreamDone := startUpstreamServer(t, false)
	host, portText, _ := net.SplitHostPort(address)
	var port int
	_, _ = fmt.Sscanf(portText, "%d", &port)
	upstream, err := (sshupstream.Dialer{HostKeyCallback: ssh.FixedHostKey(upstreamSigner.PublicKey()), Timeout: time.Second}).Dial(
		context.Background(),
		jumpserver.ClientConnection{Protocol: "ssh", Endpoint: jumpserver.Endpoint{Host: host, Port: port}, Token: jumpserver.ConnectionCredential{ID: "connection-1", Value: "connection-secret"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	localSigner := newSigner(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	proxyDone := make(chan error, 1)
	go func() {
		transport, acceptErr := listener.Accept()
		if acceptErr != nil {
			proxyDone <- acceptErr
			return
		}
		proxyDone <- (Server{}).Run(ctx, transport, localSigner, upstream)
	}()
	transport, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	clientConnection, channels, requests, err := ssh.NewClientConn(transport, "jumpaccess", &ssh.ClientConfig{User: "outer", HostKeyCallback: ssh.FixedHostKey(localSigner.PublicKey())})
	if err != nil {
		t.Fatal(err)
	}
	outer := ssh.NewClient(clientConnection, channels, requests)
	session, err := outer.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Shell(); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-proxyDone:
	case <-time.After(time.Second):
		t.Fatal("proxy did not stop after cancellation")
	}
	_ = outer.Close()
	_ = upstream.Close()
	select {
	case <-upstreamDone:
	case <-time.After(time.Second):
		t.Fatal("upstream server did not stop")
	}
}

func startUpstreamServer(t *testing.T, closeSession bool) (string, ssh.Signer, <-chan error) {
	t.Helper()
	signer := newSigner(t)
	configuration := &ssh.ServerConfig{
		PasswordCallback: func(metadata ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if metadata.User() != "JMS-connection-1" || string(password) != "connection-secret" {
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
		raw, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		connection, channels, requests, err := ssh.NewServerConn(raw, configuration)
		if err != nil {
			done <- err
			return
		}
		defer connection.Close()
		go ssh.DiscardRequests(requests)
		for incoming := range channels {
			if incoming.ChannelType() != "session" {
				_ = incoming.Reject(ssh.UnknownChannelType, "session only")
				continue
			}
			channel, channelRequests, err := incoming.Accept()
			if err != nil {
				done <- err
				return
			}
			go func() {
				for request := range channelRequests {
					switch request.Type {
					case "env", "pty-req":
						_ = request.Reply(true, nil)
					case "shell":
						_ = request.Reply(true, nil)
						if !closeSession {
							continue
						}
						_, _ = channel.Write([]byte("upstream stdout\n"))
						_, _ = channel.Stderr().Write([]byte("upstream stderr\n"))
						status := make([]byte, 4)
						binary.BigEndian.PutUint32(status, 7)
						_, _ = channel.SendRequest("exit-status", false, status)
						_ = channel.Close()
					default:
						_ = request.Reply(false, nil)
					}
				}
			}()
		}
		done <- nil
	}()
	return listener.Addr().String(), signer, done
}

func newSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}
