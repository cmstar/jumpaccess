package agentmcp

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func requestKey(epoch uint64, n int) string {
	return fmt.Sprintf("instance:%d:00000000-0000-4000-8000-%012d", epoch, n)
}

func TestRequestsConcurrentRetryAndConflict(t *testing.T) {
	r := newRequests("instance", 2, 2)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			record, _, err := r.begin("execute", requestKey(1, 1), map[string]string{"command": "echo ok"}, func() (string, error) {
				accepted.Add(1)
				return "execution", nil
			})
			if err != nil || record.ResourceID != "execution" {
				t.Errorf("retry: %+v %v", record, err)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted %d times", accepted.Load())
	}
	_, _, err := r.begin("execute", requestKey(1, 1), "different", nil)
	assertFault(t, err, "IDEMPOTENCY_CONFLICT")
}

func TestRequestsFIFOEpochAndActiveProtection(t *testing.T) {
	r := newRequests("instance", 2, 4)
	begin := func(epoch uint64, n int) *requestRecord {
		record, _, err := r.begin("execute", requestKey(epoch, n), n, func() (string, error) { return fmt.Sprint(n), nil })
		if err != nil {
			t.Fatal(err)
		}
		return record
	}
	active, a, b := begin(1, 1), begin(1, 2), begin(1, 3)
	r.finish(b, nil)
	r.finish(a, nil)
	// FIFO 按终结顺序；重试不能更新队列位置。
	begin(1, 3)
	c := begin(1, 4)
	r.finish(c, nil)
	if r.epoch() != 2 {
		t.Fatalf("epoch=%d", r.epoch())
	}
	_, _, err := r.begin("execute", requestKey(1, 3), 3, nil)
	assertFault(t, err, "REQUEST_EXPIRED")
	_, _, err = r.begin("execute", requestKey(1, 99), 99, nil)
	assertFault(t, err, "REQUEST_EXPIRED") // 旧代号下首次到达也不受理。
	if got := begin(1, 1); got != active {
		t.Fatal("active record evicted")
	}
	if got := begin(1, 2); got != a {
		t.Fatal("wrong FIFO order")
	}
	d := begin(2, 5)
	r.finish(d, nil)
	if r.epoch() != 2 {
		t.Fatal("evicting old epoch must not advance again")
	}
	if len(r.records) != 3 || r.active != 1 {
		t.Fatalf("unbounded indexes: %d %d", len(r.records), r.active)
	}
}

func TestRequestsAdmissionAndImmutableReceipt(t *testing.T) {
	r := newRequests("instance", 1, 1)
	a, _, err := r.begin("open", requestKey(1, 1), 1, func() (string, error) { return "conn", nil })
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = r.begin("open", requestKey(1, 2), 2, nil)
	assertFault(t, err, "LIMIT_EXCEEDED")
	r.finish(a, fault("CONNECT_FAILED", "failed"))
	r.finish(a, nil)
	got, fresh, err := r.begin("open", requestKey(1, 1), 1, nil)
	if err != nil || fresh || got.Failure.Code != "CONNECT_FAILED" {
		t.Fatalf("receipt changed: %+v %v", got, err)
	}
	_, _, err = r.begin("open", requestKey(1, 2), 2, func() (string, error) { return "", fault("CONNECTION_BUSY", "busy") })
	assertFault(t, err, "CONNECTION_BUSY")
	if len(r.records) != 1 {
		t.Fatal("rejected admission reserved key")
	}
	_, _, err = r.begin("open", "other:1:00000000-0000-4000-8000-000000000001", 1, nil)
	assertFault(t, err, "INSTANCE_CHANGED")
	_, _, err = r.begin("open", requestKey(2, 2), 2, nil)
	assertFault(t, err, "INVALID_REQUEST_KEY")
	_, _, err = r.begin("open", "instance:1:not-a-uuid", 2, nil)
	assertFault(t, err, "INVALID_REQUEST_KEY")
}

func assertFault(t *testing.T, err error, code string) {
	t.Helper()
	f, ok := err.(*Fault)
	if !ok || f.Code != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
}
