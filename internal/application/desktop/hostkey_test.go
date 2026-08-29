package desktop

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHostKeyCoordinatorWaitsForExplicitDecision(t *testing.T) {
	prompts := make(chan HostKeyPrompt, 1)
	coordinator := HostKeyCoordinator{Emit: func(prompt HostKeyPrompt) { prompts <- prompt }}
	result := make(chan bool, 1)
	errorsResult := make(chan error, 1)
	go func() {
		accepted, err := coordinator.Confirm(context.Background(), "gateway.example.test:2222", "SHA256:abc")
		result <- accepted
		errorsResult <- err
	}()

	prompt := <-prompts
	if prompt.ID == "" || prompt.Host != "gateway.example.test:2222" || prompt.Fingerprint != "SHA256:abc" {
		t.Fatalf("prompt = %#v", prompt)
	}
	select {
	case <-result:
		t.Fatal("Confirm returned before a decision")
	case <-time.After(10 * time.Millisecond):
	}
	if err := coordinator.Resolve(prompt.ID, true); err != nil {
		t.Fatal(err)
	}
	if accepted := <-result; !accepted {
		t.Fatal("accepted = false, want true")
	}
	if err := <-errorsResult; err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Resolve(prompt.ID, false); err == nil {
		t.Fatal("Resolve accepted a completed prompt")
	}
}

func TestHostKeyCoordinatorStopsWaitingWhenSessionIsCancelled(t *testing.T) {
	prompts := make(chan HostKeyPrompt, 1)
	coordinator := HostKeyCoordinator{Emit: func(prompt HostKeyPrompt) { prompts <- prompt }}
	ctx, cancel := context.WithCancel(context.Background())
	errorsResult := make(chan error, 1)
	go func() {
		_, err := coordinator.Confirm(ctx, "gateway.example.test:2222", "SHA256:abc")
		errorsResult <- err
	}()
	prompt := <-prompts
	cancel()
	if err := <-errorsResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if err := coordinator.Resolve(prompt.ID, true); err == nil {
		t.Fatal("Resolve accepted a cancelled prompt")
	}
}
