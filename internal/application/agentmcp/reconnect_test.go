package agentmcp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestReconnectFailureCooldownKeepsLogicalIDAndOriginalReceipt(t *testing.T) {
	opts := poolOptions(t)
	dial := opts.Dial
	var calls atomic.Int32
	opts.Dial = func(ctx context.Context, r ResolvedTarget) (*ssh.Client, error) {
		if calls.Add(1) > 1 {
			return nil, errors.New("synthetic dial failure")
		}
		return dial(ctx, r)
	}
	e := NewEngine(opts)
	defer e.Shutdown(context.Background())
	c := openTest(t, e, 1, "a")
	e.mu.Lock()
	e.dropLocked(e.connections[c.ID], "test_idle")
	e.mu.Unlock()
	in := executeInput(e, c, 2)
	v, err := e.Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	v = awaitExecution(t, e, v.ID)
	if v.Status != "failed" || v.DispatchState != "not_sent" || v.ConnectAttempts != 1 || calls.Load() != 2 {
		t.Fatalf("failure %+v calls=%d", v, calls.Load())
	}
	r, err := e.Open(context.Background(), OpenInput{RequestKey: requestKey(e.RequestEpoch(), 3), Target: c.Target.Target})
	if err != nil || r.Connection == nil || r.Connection.ID != c.ID || r.Error.Code != "RECONNECT_COOLDOWN" {
		t.Fatalf("cooldown %+v %v", r, err)
	}
	if calls.Load() != 2 {
		t.Fatal("cooldown redialed")
	}
	retry, err := e.Execute(context.Background(), in)
	if err != nil || retry.ID != v.ID || retry.Status != "failed" {
		t.Fatal("retry lost original failure", err)
	}
}
