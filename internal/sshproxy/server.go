package sshproxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

type Server struct{}

func (Server) Run(ctx context.Context, transport net.Conn, hostKey ssh.Signer, upstream *ssh.Client) error {
	if hostKey == nil {
		return fmt.Errorf("ProxyCommand host key is unavailable")
	}
	if upstream == nil {
		return fmt.Errorf("upstream SSH client is unavailable")
	}
	configuration := &ssh.ServerConfig{NoClientAuth: true}
	configuration.AddHostKey(hostKey)
	serverConnection, channels, requests, err := ssh.NewServerConn(transport, configuration)
	if err != nil {
		return fmt.Errorf("accept ProxyCommand SSH client: %w", err)
	}
	defer serverConnection.Close()

	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = serverConnection.Close()
			_ = upstream.Close()
		case <-stopped:
		}
	}()
	defer close(stopped)
	go handleGlobalRequests(requests)

	var sessions sync.WaitGroup
	for incoming := range channels {
		if incoming.ChannelType() != "session" {
			_ = incoming.Reject(ssh.Prohibited, "JumpAccess ProxyCommand supports session channels only")
			continue
		}
		upstreamChannel, upstreamRequests, err := upstream.OpenChannel("session", nil)
		if err != nil {
			_ = incoming.Reject(ssh.ConnectionFailed, "upstream session unavailable")
			continue
		}
		localChannel, localRequests, err := incoming.Accept()
		if err != nil {
			_ = upstreamChannel.Close()
			continue
		}
		sessions.Add(1)
		go func() {
			defer sessions.Done()
			bridgeSession(localChannel, localRequests, upstreamChannel, upstreamRequests)
		}()
	}
	sessions.Wait()
	return nil
}

func handleGlobalRequests(requests <-chan *ssh.Request) {
	for request := range requests {
		accepted := request.Type == "keepalive@openssh.com"
		if request.WantReply {
			_ = request.Reply(accepted, nil)
		}
	}
}

func bridgeSession(local ssh.Channel, localRequests <-chan *ssh.Request, upstream ssh.Channel, upstreamRequests <-chan *ssh.Request) {
	defer local.Close()
	defer upstream.Close()

	go forwardRequests(localRequests, upstream, allowedLocalRequest)
	outputDone := make(chan struct{})
	upstreamRequestDone := make(chan struct{})
	go func() {
		defer close(upstreamRequestDone)
		forwardUpstreamRequests(upstreamRequests, local, outputDone)
	}()
	go copyInput(upstream, local)

	var output sync.WaitGroup
	output.Add(2)
	go func() {
		defer output.Done()
		_, _ = io.Copy(local, upstream)
	}()
	go func() {
		defer output.Done()
		_, _ = io.Copy(local.Stderr(), upstream.Stderr())
	}()
	output.Wait()
	_ = local.CloseWrite()
	close(outputDone)
	<-upstreamRequestDone
}

func forwardUpstreamRequests(requests <-chan *ssh.Request, destination ssh.Channel, outputDone <-chan struct{}) {
	type delayedRequest struct {
		requestType string
		wantReply   bool
		payload     []byte
	}
	var delayed []delayedRequest
	for request := range requests {
		if !allowedUpstreamRequest(request.Type) {
			if request.WantReply {
				_ = request.Reply(false, nil)
			}
			continue
		}
		if request.Type == "exit-status" || request.Type == "exit-signal" {
			delayed = append(delayed, delayedRequest{request.Type, request.WantReply, append([]byte(nil), request.Payload...)})
			if request.WantReply {
				_ = request.Reply(true, nil)
			}
			continue
		}
		accepted, err := destination.SendRequest(request.Type, request.WantReply, request.Payload)
		if request.WantReply {
			_ = request.Reply(err == nil && accepted, nil)
		}
	}
	<-outputDone
	for _, request := range delayed {
		_, _ = destination.SendRequest(request.requestType, request.wantReply, request.payload)
	}
}

func copyInput(destination ssh.Channel, source ssh.Channel) {
	_, _ = io.Copy(destination, source)
	_ = destination.CloseWrite()
}

func forwardRequests(requests <-chan *ssh.Request, destination ssh.Channel, allowed func(string) bool) {
	for request := range requests {
		accepted := false
		var err error
		if allowed(request.Type) {
			accepted, err = destination.SendRequest(request.Type, request.WantReply, request.Payload)
		}
		if request.WantReply {
			_ = request.Reply(err == nil && accepted, nil)
		}
	}
}

func allowedLocalRequest(requestType string) bool {
	switch requestType {
	case "env", "pty-req", "shell", "exec", "window-change", "signal", "break", "eow@openssh.com":
		return true
	default:
		return false
	}
}

func allowedUpstreamRequest(requestType string) bool {
	switch requestType {
	case "exit-status", "exit-signal", "xon-xoff", "eow@openssh.com":
		return true
	default:
		return false
	}
}
