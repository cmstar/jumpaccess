package sshproxy

import (
	"io"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// 上游已结束输出，但请求转发尚未返回时，本地通道必须保持打开。
func TestBridgeSessionWaitsForInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	outputRead := make(chan struct{}, 2)
	local := &bridgeTestChannel{reader: &bridgeEOFReader{}}
	upstream := &bridgeTestChannel{
		reader: &bridgeEOFReader{wait: started, read: outputRead},
		send: func() {
			close(started)
			<-release
		},
	}
	requests := make(chan *ssh.Request, 1)
	requests <- &ssh.Request{Type: "shell"}
	upstreamRequests := make(chan *ssh.Request)
	close(upstreamRequests)
	done := make(chan struct{})
	go func() {
		bridgeSession(local, requests, upstream, upstreamRequests)
		close(done)
	}()
	t.Cleanup(func() {
		defer close(requests)
		close(release)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("请求完成后会话未关闭")
		}
	})
	for range 2 {
		select {
		case <-outputRead:
		case <-time.After(time.Second):
			t.Fatal("上游输出未结束")
		}
	}
	select {
	case <-done:
		t.Fatal("请求转发完成前关闭了本地会话")
	case <-time.After(50 * time.Millisecond):
	}
}

type bridgeEOFReader struct {
	wait <-chan struct{}
	read chan<- struct{}
}

func (r *bridgeEOFReader) Read([]byte) (int, error) {
	if r.wait != nil {
		<-r.wait
	}
	if r.read != nil {
		r.read <- struct{}{}
	}
	return 0, io.EOF
}

type bridgeTestChannel struct {
	ssh.Channel
	reader io.Reader
	send   func()
}

func (c *bridgeTestChannel) Read(p []byte) (int, error)  { return c.reader.Read(p) }
func (c *bridgeTestChannel) Write(p []byte) (int, error) { return len(p), nil }
func (c *bridgeTestChannel) Close() error                { return nil }
func (c *bridgeTestChannel) CloseWrite() error           { return nil }
func (c *bridgeTestChannel) Stderr() io.ReadWriter {
	return struct {
		io.Reader
		io.Writer
	}{c.reader, io.Discard}
}
func (c *bridgeTestChannel) SendRequest(string, bool, []byte) (bool, error) {
	c.send()
	return true, nil
}
