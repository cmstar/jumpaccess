package sshclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"testing"
	"time"
)

func TestSessionProbeUsesIndependentExecAndHonorsTimeout(t *testing.T) {
	for _, mode := range []string{"supported", "rejected", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
			signer, _ := ssh.NewSignerFromKey(privateKey)
			configuration := &ssh.ServerConfig{NoClientAuth: true}
			configuration.AddHostKey(signer)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			command := make(chan string, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				connection, err := listener.Accept()
				if err != nil {
					return
				}
				defer connection.Close()
				server, channels, requests, err := ssh.NewServerConn(connection, configuration)
				if err != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				for channel := range channels {
					remote, requests, err := channel.Accept()
					if err != nil {
						return
					}
					for request := range requests {
						if request.Type != "exec" {
							_ = request.Reply(false, nil)
							continue
						}
						var payload struct{ Command string }
						_ = ssh.Unmarshal(request.Payload, &payload)
						command <- payload.Command
						if mode == "timeout" {
							continue
						}
						_ = request.Reply(mode == "supported", nil)
						if mode == "supported" {
							_, _ = io.WriteString(remote, "JUMPACCESS_ZMODEM:10:END\n")
							_, _ = remote.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
						}
						_ = remote.Close()
					}
				}
			}()
			client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{User: "test", HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			upload, download, err := (&Session{client: client}).ProbeTransferCommands(ctx)
			if mode == "supported" {
				if err != nil || !upload || download {
					t.Fatalf("probe = %t %t %v", upload, download, err)
				}
			} else if err == nil {
				t.Fatal("unavailable probe reported success")
			}
			if got := <-command; got != transferProbeCommand {
				t.Fatalf("unexpected command %q", got)
			}
			_ = client.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("probe channel did not close")
			}
		})
	}
}

func TestTransferProbeRequiresExactFinalAssetResponse(t *testing.T) {
	for _, flags := range []string{"00", "01", "10", "11"} {
		upload, download, err := parseTransferProbe("JUMPACCESS_ZMODEM:" + flags + ":END\n")
		if err != nil || upload != (flags[0] == '1') || download != (flags[1] == '1') {
			t.Fatalf("probe %q: %t %t %v", flags, upload, download, err)
		}
	}
	for _, invalid := range []string{"", "command rejected", "echo JUMPACCESS_ZMODEM:11:END", "JUMPACCESS_ZMODEM:11:END\nmenu>"} {
		if _, _, err := parseTransferProbe(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
}
