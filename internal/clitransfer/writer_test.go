package clitransfer

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

type blockedWriter struct {
	entered chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (w *blockedWriter) Write([]byte) (int, error) {
	close(w.entered)
	<-w.closed
	return 0, io.ErrClosedPipe
}
func (w *blockedWriter) Close() error { w.once.Do(func() { close(w.closed) }); return nil }

func TestCancelUnblocksStalledRemoteWrite(t *testing.T) {
	parent, stop := context.WithCancel(context.Background())
	defer stop()
	destination := &blockedWriter{entered: make(chan struct{}), closed: make(chan struct{})}
	writer := newRemoteWriter(parent, destination)
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := writer.write(ctx, []byte("packet")); done <- err }()
	<-destination.entered
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled write succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel left writer blocked")
	}
	select {
	case <-destination.closed:
	default:
		t.Fatal("stalled transport was not closed")
	}
}

func TestDisconnectUnblocksPendingKeyboardWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	keyboard, keys := io.Pipe()
	defer keyboard.Close()
	defer keys.Close()
	incoming, server := io.Pipe()
	defer incoming.Close()
	defer server.Close()
	destination := &blockedWriter{entered: make(chan struct{}), closed: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- Run(ctx, keyboard, io.Discard, destination, incoming, Options{}) }()
	_, _ = keys.Write([]byte("command\r"))
	<-destination.entered
	_ = server.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("disconnect was blocked by keyboard write")
	}
}
