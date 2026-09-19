package sshclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"golang.org/x/crypto/ssh"
)

func TestOpenInterruptsStalledSessionNegotiation(t *testing.T) {
	for _, stage := range []string{"channel", "pty-req", "shell"} {
		for _, timeout := range []bool{false, true} {
			name := stage + "/cancel"
			if timeout {
				name = stage + "/timeout"
			}
			t.Run(name, func(t *testing.T) {
				options, reached := stalledSSHSession(t, stage)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if timeout {
					options.Timeout = 200 * time.Millisecond
				}
				done := make(chan error, 1)
				go func() {
					session, err := Open(ctx, options)
					if session != nil {
						_ = session.Close()
					}
					done <- err
				}()
				select {
				case <-reached:
				case <-time.After(time.Second):
					t.Fatal("未到达待测 SSH 协商阶段")
				}
				if !timeout {
					cancel()
				}
				select {
				case err := <-done:
					if err == nil {
						t.Fatal("被取消或超时的连接成功返回")
					}
				case <-time.After(time.Second):
					t.Fatal("SSH 会话协商未响应取消或连接超时")
				}
			})
		}
	}
}

func TestOpenTimeoutDoesNotCloseEstablishedSession(t *testing.T) {
	options, _ := stalledSSHSession(t, "active")
	options.Timeout = 200 * time.Millisecond
	session, err := Open(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	// 建连的 deadline 不属于活动会话。
	<-time.After(2 * options.Timeout)
	if _, err := session.ProbeLatency(); err != nil {
		t.Fatalf("连接超时影响了已建立会话: %v", err)
	}
}

func stalledSSHSession(t *testing.T, stage string) (OpenOptions, <-chan struct{}) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	configuration := &ssh.ServerConfig{NoClientAuth: true}
	configuration.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	reached := make(chan struct{})
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		t.Cleanup(func() { _ = connection.Close() })
		server, channels, requests, err := ssh.NewServerConn(connection, configuration)
		if err != nil {
			return
		}
		defer server.Close()
		go ssh.DiscardRequests(requests)
		channel, ok := <-channels
		if !ok {
			return
		}
		if stage == "channel" {
			close(reached)
			_ = server.Wait()
			return
		}
		_, sessionRequests, err := channel.Accept()
		if err != nil {
			return
		}
		for request := range sessionRequests {
			if request.Type == stage {
				close(reached)
				_ = server.Wait()
				return
			}
			_ = request.Reply(true, nil)
			if stage == "active" && request.Type == "shell" {
				close(reached)
				_ = server.Wait()
				return
			}
		}
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	return OpenOptions{
		Connection:      jumpserver.ClientConnection{Protocol: "ssh", Endpoint: jumpserver.Endpoint{Host: host, Port: port}},
		HostKeyCallback: ssh.FixedHostKey(signer.PublicKey()),
		Terminal:        TerminalOptions{Columns: 80, Rows: 24},
	}, reached
}
