package sshsession

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	connectapp "github.com/cmstar/jumpaccess/internal/application/connect"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/cmstar/jumpaccess/internal/sshclient"
	"github.com/cmstar/jumpaccess/internal/target"
	"golang.org/x/crypto/ssh"
)

type fakePreparer struct {
	mu       sync.Mutex
	requests []connectapp.Options
}

func (f *fakePreparer) Prepare(_ context.Context, options connectapp.Options) (connectapp.Prepared, error) {
	f.mu.Lock()
	f.requests = append(f.requests, options)
	f.mu.Unlock()
	return connectapp.Prepared{
		Selection: target.Selection{Profile: options.Target.Profile, Organization: options.Target.Organization, Asset: options.Target.Target},
		Asset:     jumpserver.AssetDetail{Asset: jumpserver.Asset{ID: options.Target.Target, Name: "web-01"}},
		Account:   jumpserver.Account{ID: options.Target.Account, Username: "root"},
		Connection: jumpserver.ClientConnection{
			Protocol: "ssh",
			Endpoint: jumpserver.Endpoint{Host: "gateway.example.test", Port: 2222},
			Token:    jumpserver.ConnectionCredential{ID: options.Target.Target, Value: "secret"},
		},
	}, nil
}

type fakeTerminalSession struct {
	mu        sync.Mutex
	writes    bytes.Buffer
	resizes   [][2]int
	closed    chan struct{}
	closeOnce sync.Once
}

func newFakeTerminalSession() *fakeTerminalSession {
	return &fakeTerminalSession{closed: make(chan struct{})}
}

func (s *fakeTerminalSession) Write(value []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes.Write(value)
}

func (s *fakeTerminalSession) Resize(columns, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resizes = append(s.resizes, [2]int{columns, rows})
	return nil
}

func (s *fakeTerminalSession) Wait() error {
	<-s.closed
	return nil
}

func (s *fakeTerminalSession) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

func TestManagerRunsIndependentSessionsAndBatchesOutput(t *testing.T) {
	preparer := &fakePreparer{}
	states := make(chan StateEvent, 16)
	outputs := make(chan OutputEvent, 16)
	opened := make(map[string]*fakeTerminalSession)
	writers := make(map[string]io.Writer)
	var openedMu sync.Mutex
	manager := &Manager{
		Prepare: preparer,
		HostKeyCallback: func(context.Context) (ssh.HostKeyCallback, error) {
			return ssh.InsecureIgnoreHostKey(), nil
		},
		Open: func(_ context.Context, options sshclient.OpenOptions) (TerminalSession, error) {
			session := newFakeTerminalSession()
			openedMu.Lock()
			opened[options.Connection.Token.ID] = session
			writers[options.Connection.Token.ID] = options.Stdout
			openedMu.Unlock()
			return session, nil
		},
		EmitState:     func(event StateEvent) { states <- event },
		EmitOutput:    func(event OutputEvent) { outputs <- event },
		BatchInterval: 5 * time.Millisecond,
		BatchSize:     1024,
	}

	first, err := manager.Start(context.Background(), StartRequest{
		Profile: "work", Organization: "org-1", Target: "asset-1", Account: "account-1", Columns: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Start(context.Background(), StartRequest{
		Profile: "work", Organization: "org-1", Target: "asset-2", Account: "account-2", Columns: 100, Rows: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == "" || second.ID == "" || first.ID == second.ID || first.Status != StatusConnecting || second.Status != StatusConnecting {
		t.Fatalf("initial states = %#v, %#v", first, second)
	}
	waitForStatuses(t, states, map[string]string{first.ID: StatusActive, second.ID: StatusActive})

	if err := manager.Write(first.ID, "whoami\n"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Resize(first.ID, 132, 42); err != nil {
		t.Fatal(err)
	}
	openedMu.Lock()
	firstSession := opened["asset-1"]
	firstWriter := writers["asset-1"]
	openedMu.Unlock()
	if firstSession == nil || firstWriter == nil {
		t.Fatal("first session was not opened")
	}
	firstSession.mu.Lock()
	if firstSession.writes.String() != "whoami\n" || len(firstSession.resizes) != 1 || firstSession.resizes[0] != [2]int{132, 42} {
		t.Fatalf("session input = %q, resizes = %#v", firstSession.writes.String(), firstSession.resizes)
	}
	firstSession.mu.Unlock()
	_, _ = firstWriter.Write([]byte("hello "))
	_, _ = firstWriter.Write([]byte("world"))
	select {
	case output := <-outputs:
		if output.ID != first.ID || output.Data != "hello world" {
			t.Fatalf("output = %#v", output)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for batched output")
	}

	if err := manager.Close(first.ID); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, states, first.ID, StatusClosed)
	if err := manager.Write(first.ID, "ignored"); err == nil {
		t.Fatal("Write accepted a closed session")
	}
	if err := manager.Close(second.ID); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, states, second.ID, StatusClosed)

	preparer.mu.Lock()
	defer preparer.mu.Unlock()
	if len(preparer.requests) != 2 || !preparer.requests[0].NonInteractive || preparer.requests[0].Target.Account != "account-1" {
		t.Fatalf("prepare requests = %#v", preparer.requests)
	}
}

func waitForStatus(t *testing.T, events <-chan StateEvent, id, status string) StateEvent {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case event := <-events:
			if event.ID == id && event.Status == status {
				return event
			}
		case <-deadline:
			t.Fatalf("timed out waiting for session %s status %s", id, status)
		}
	}
}

func waitForStatuses(t *testing.T, events <-chan StateEvent, expected map[string]string) {
	t.Helper()
	deadline := time.After(time.Second)
	seen := make(map[string]bool, len(expected))
	for len(seen) < len(expected) {
		select {
		case event := <-events:
			if status, exists := expected[event.ID]; exists && event.Status == status {
				seen[event.ID] = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for statuses %#v; seen %#v", expected, seen)
		}
	}
}
