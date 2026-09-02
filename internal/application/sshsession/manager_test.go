package sshsession

import (
	"bytes"
	"context"
	"errors"
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

type prepareFunc func(context.Context, connectapp.Options) (connectapp.Prepared, error)

func (f prepareFunc) Prepare(ctx context.Context, options connectapp.Options) (connectapp.Prepared, error) {
	return f(ctx, options)
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
	mu         sync.Mutex
	writes     bytes.Buffer
	resizes    [][2]int
	latency    time.Duration
	latencyErr error
	closed     chan struct{}
	closeOnce  sync.Once
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

func (s *fakeTerminalSession) ProbeLatency() (time.Duration, error) {
	return s.latency, s.latencyErr
}

func (s *fakeTerminalSession) Wait() error {
	<-s.closed
	return nil
}

func (s *fakeTerminalSession) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

func TestManagerLatencyIntervalDefaultsToThreeSeconds(t *testing.T) {
	manager := &Manager{}
	if got := manager.latencyInterval(); got != 3*time.Second {
		t.Fatalf("latency interval = %s, want 3s", got)
	}
}

func TestManagerEmitsGatewayLatencyForActiveSession(t *testing.T) {
	states := make(chan StateEvent, 4)
	latencies := make(chan LatencyEvent, 2)
	terminal := newFakeTerminalSession()
	terminal.latency = 149 * time.Millisecond
	manager := &Manager{
		Prepare: &fakePreparer{},
		HostKeyCallback: func(context.Context) (ssh.HostKeyCallback, error) {
			return ssh.InsecureIgnoreHostKey(), nil
		},
		Open:            func(context.Context, sshclient.OpenOptions) (TerminalSession, error) { return terminal, nil },
		EmitState:       func(event StateEvent) { states <- event },
		EmitLatency:     func(event LatencyEvent) { latencies <- event },
		LatencyInterval: time.Hour,
	}

	session, err := manager.Start(context.Background(), StartRequest{
		Profile: "work", Organization: "org-1", Target: "asset-1", Account: "account-1", Columns: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, states, session.ID, StatusActive)

	select {
	case event := <-latencies:
		if event.ID != session.ID || !event.Available || event.Milliseconds != 149 {
			t.Fatalf("latency event = %#v, want available 149 ms for %q", event, session.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SSH gateway latency")
	}

	if err := manager.Close(session.ID); err != nil {
		t.Fatal(err)
	}
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
	requests := make(map[string]connectapp.Options, len(preparer.requests))
	for _, request := range preparer.requests {
		requests[request.Target.Target] = request
	}
	firstRequest, firstExists := requests["asset-1"]
	secondRequest, secondExists := requests["asset-2"]
	if len(preparer.requests) != 2 || !firstExists || !secondExists || !firstRequest.NonInteractive || !secondRequest.NonInteractive || firstRequest.Target.Account != "account-1" || secondRequest.Target.Account != "account-2" {
		t.Fatalf("prepare requests = %#v", preparer.requests)
	}
}

func TestManagerUsesResizeReceivedWhileSessionIsConnecting(t *testing.T) {
	prepareStarted := make(chan struct{})
	releasePrepare := make(chan struct{})
	states := make(chan StateEvent, 4)
	opened := make(chan sshclient.OpenOptions, 1)
	terminal := newFakeTerminalSession()
	basePreparer := &fakePreparer{}
	manager := &Manager{
		Prepare: prepareFunc(func(ctx context.Context, options connectapp.Options) (connectapp.Prepared, error) {
			close(prepareStarted)
			select {
			case <-releasePrepare:
				return basePreparer.Prepare(ctx, options)
			case <-ctx.Done():
				return connectapp.Prepared{}, ctx.Err()
			}
		}),
		HostKeyCallback: func(context.Context) (ssh.HostKeyCallback, error) {
			return ssh.InsecureIgnoreHostKey(), nil
		},
		Open: func(_ context.Context, options sshclient.OpenOptions) (TerminalSession, error) {
			opened <- options
			return terminal, nil
		},
		EmitState: func(event StateEvent) { states <- event },
	}

	session, err := manager.Start(context.Background(), StartRequest{
		Profile: "work", Organization: "org-1", Target: "asset-1", Account: "account-1", Columns: 120, Rows: 34,
	})
	if err != nil {
		t.Fatal(err)
	}
	<-prepareStarted
	if err := manager.Resize(session.ID, 156, 48); err != nil {
		t.Fatalf("Resize while connecting returned error: %v", err)
	}
	close(releasePrepare)
	waitForStatus(t, states, session.ID, StatusActive)

	select {
	case options := <-opened:
		if options.Terminal.Columns != 156 || options.Terminal.Rows != 48 {
			t.Fatalf("opened terminal dimensions = %dx%d, want 156x48", options.Terminal.Columns, options.Terminal.Rows)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for terminal open")
	}
	if err := manager.Close(session.ID); err != nil {
		t.Fatal(err)
	}
}

func TestManagerExposesStableResolvedSessionMetadata(t *testing.T) {
	states := make(chan StateEvent, 4)
	terminal := newFakeTerminalSession()
	manager := &Manager{
		Prepare: prepareFunc(func(context.Context, connectapp.Options) (connectapp.Prepared, error) {
			return connectapp.Prepared{
				Selection: target.Selection{
					Profile:      "work",
					Organization: "org-1",
					Alias:        "production-web",
				},
				Asset: jumpserver.AssetDetail{Asset: jumpserver.Asset{
					ID:   "asset-1",
					Name: "web-01",
				}},
				Account: jumpserver.Account{ID: "account-1", Username: "root"},
				Connection: jumpserver.ClientConnection{
					Protocol: "ssh",
					Endpoint: jumpserver.Endpoint{Host: "gateway.example.test", Port: 2222},
					Token:    jumpserver.ConnectionCredential{ID: "asset-1", Value: "secret"},
				},
			}, nil
		}),
		HostKeyCallback: func(context.Context) (ssh.HostKeyCallback, error) {
			return ssh.InsecureIgnoreHostKey(), nil
		},
		Open:      func(context.Context, sshclient.OpenOptions) (TerminalSession, error) { return terminal, nil },
		EmitState: func(event StateEvent) { states <- event },
	}

	started, err := manager.Start(context.Background(), StartRequest{
		Profile: "work", Organization: "org-1", Target: "production-web", Account: "account-1", Columns: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.Target != "production-web" {
		t.Fatalf("connecting target = %q, want production-web", started.Target)
	}
	active := waitForStatus(t, states, started.ID, StatusActive)
	if active.Target != "production-web" || active.Alias != "production-web" {
		t.Fatalf("resolved target metadata = %#v", active)
	}
	if active.AssetID != "asset-1" || active.AssetName != "web-01" || active.Asset != "asset-1" {
		t.Fatalf("resolved asset metadata = %#v", active)
	}
	if active.Title != "production-web" {
		t.Fatalf("resolved title = %q, want production-web", active.Title)
	}
	if err := manager.Close(started.ID); err != nil {
		t.Fatal(err)
	}
}

func TestManagerKeepsRemoteClosedSessionUntilExplicitClose(t *testing.T) {
	states := make(chan StateEvent, 4)
	terminal := newFakeTerminalSession()
	manager := newTestManager(&fakePreparer{}, terminal, states)

	started, err := manager.Start(context.Background(), StartRequest{
		Profile: "work", Organization: "org-1", Target: "asset-1", Account: "account-1", Columns: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, states, started.ID, StatusActive)
	if err := terminal.Close(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, states, started.ID, StatusClosed)

	listed := manager.List()
	if len(listed) != 1 || listed[0].ID != started.ID || listed[0].Status != StatusClosed {
		t.Fatalf("sessions after remote close = %#v", listed)
	}
	if err := manager.Close(started.ID); err != nil {
		t.Fatal(err)
	}
	if listed = manager.List(); len(listed) != 0 {
		t.Fatalf("sessions after explicit close = %#v, want none", listed)
	}
}

func TestManagerKeepsFailedSessionUntilExplicitClose(t *testing.T) {
	states := make(chan StateEvent, 4)
	manager := &Manager{
		Prepare: prepareFunc(func(context.Context, connectapp.Options) (connectapp.Prepared, error) {
			return connectapp.Prepared{}, errors.New("prepare failed")
		}),
		HostKeyCallback: func(context.Context) (ssh.HostKeyCallback, error) {
			return ssh.InsecureIgnoreHostKey(), nil
		},
		Open: func(context.Context, sshclient.OpenOptions) (TerminalSession, error) {
			return newFakeTerminalSession(), nil
		},
		EmitState: func(event StateEvent) { states <- event },
	}

	started, err := manager.Start(context.Background(), StartRequest{
		Profile: "work", Organization: "org-1", Target: "asset-1", Account: "account-1", Columns: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitForStatus(t, states, started.ID, StatusFailed)
	if failed.Error != "prepare failed" {
		t.Fatalf("failed session error = %q, want prepare failed", failed.Error)
	}
	listed := manager.List()
	if len(listed) != 1 || listed[0].ID != started.ID || listed[0].Status != StatusFailed {
		t.Fatalf("sessions after failure = %#v", listed)
	}
	if err := manager.Close(started.ID); err != nil {
		t.Fatal(err)
	}
	if listed = manager.List(); len(listed) != 0 {
		t.Fatalf("sessions after explicit close = %#v, want none", listed)
	}
}

func newTestManager(preparer Preparer, terminal TerminalSession, states chan<- StateEvent) *Manager {
	return &Manager{
		Prepare: preparer,
		HostKeyCallback: func(context.Context) (ssh.HostKeyCallback, error) {
			return ssh.InsecureIgnoreHostKey(), nil
		},
		Open:      func(context.Context, sshclient.OpenOptions) (TerminalSession, error) { return terminal, nil },
		EmitState: func(event StateEvent) { states <- event },
	}
}

func TestManagerCloseAllClosesEveryActiveTerminal(t *testing.T) {
	first := newFakeTerminalSession()
	second := newFakeTerminalSession()
	manager := &Manager{sessions: map[string]*managedSession{
		"first":  {state: StateEvent{ID: "first", Status: StatusActive}, cancel: func() {}, terminal: first},
		"second": {state: StateEvent{ID: "second", Status: StatusActive}, cancel: func() {}, terminal: second},
	}}

	if err := manager.CloseAll(); err != nil {
		t.Fatal(err)
	}
	for name, closed := range map[string]<-chan struct{}{"first": first.closed, "second": second.closed} {
		select {
		case <-closed:
		case <-time.After(time.Second):
			t.Fatalf("terminal %s was not closed", name)
		}
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
