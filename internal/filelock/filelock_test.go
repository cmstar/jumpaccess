package filelock

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLockSerializesProcessesUsingSameFile(t *testing.T) {
	locker := Locker{Dir: t.TempDir(), RetryInterval: time.Millisecond}
	firstUnlock, err := locker.Lock(context.Background(), "oauth-work")
	if err != nil {
		t.Fatal(err)
	}

	acquired := make(chan func() error, 1)
	go func() {
		unlock, lockErr := locker.Lock(context.Background(), "oauth-work")
		if lockErr == nil {
			acquired <- unlock
		}
	}()
	select {
	case <-acquired:
		t.Fatal("second lock was acquired before the first was released")
	case <-time.After(20 * time.Millisecond):
	}
	if err := firstUnlock(); err != nil {
		t.Fatal(err)
	}
	select {
	case secondUnlock := <-acquired:
		if err := secondUnlock(); err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("second lock was not acquired after release")
	}
}

func TestLockHonorsCancellationWhileWaiting(t *testing.T) {
	locker := Locker{Dir: t.TempDir(), RetryInterval: time.Millisecond}
	unlock, err := locker.Lock(context.Background(), "oauth-work")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := locker.Lock(ctx, "oauth-work"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Lock error = %v", err)
	}
}

func TestLockRejectsUnsafeKey(t *testing.T) {
	locker := Locker{Dir: t.TempDir()}
	if _, err := locker.Lock(context.Background(), "../oauth"); err == nil {
		t.Fatal("unsafe lock key was accepted")
	}
}
