package agentmcp

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// 使用真实 SSH 握手与 channel，测试不访问 JumpServer。
func testSSH(t *testing.T, handle func(ssh.Channel, <-chan *ssh.Request)) func(context.Context, ResolvedTarget) (*ssh.Client, error) {
	t.Helper()
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(key)
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var clients []net.Conn
	t.Cleanup(func() {
		listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range clients {
			c.Close()
		}
	})
	go func() {
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			clients = append(clients, raw)
			mu.Unlock()
			go func() {
				_, channels, requests, err := ssh.NewServerConn(raw, config)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(requests)
				for channel := range channels {
					c, requests, err := channel.Accept()
					if err != nil {
						continue
					}
					go handle(c, requests)
				}
			}()
		}
	}()
	return func(ctx context.Context, _ ResolvedTarget) (*ssh.Client, error) {
		raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
		if err != nil {
			return nil, err
		}
		stop := context.AfterFunc(ctx, func() { raw.Close() })
		defer stop()
		c, channels, requests, err := ssh.NewClientConn(raw, listener.Addr().String(), &ssh.ClientConfig{User: "test", HostKeyCallback: ssh.InsecureIgnoreHostKey()})
		if err != nil {
			raw.Close()
			return nil, err
		}
		return ssh.NewClient(c, channels, requests), nil
	}
}

func poolOptions(t *testing.T) EngineOptions {
	return EngineOptions{Instance: "instance", Resolve: func(_ context.Context, target Target) (ResolvedTarget, error) {
		return ResolvedTarget{Target: target, Site: "https://example.invalid", CredentialIdentity: "login"}, nil
	}, Validate: func(context.Context, ResolvedTarget) error { return nil }, Dial: testSSH(t, func(c ssh.Channel, requests <-chan *ssh.Request) { defer c.Close(); ssh.DiscardRequests(requests) })}
}

func openTest(t *testing.T, e *Engine, n int, asset string) ConnectionView {
	t.Helper()
	r, err := e.Open(context.Background(), OpenInput{RequestKey: requestKey(e.RequestEpoch(), n), Target: Target{Profile: "p", Organization: "o", AssetID: asset, AccountID: "a"}})
	if err != nil || r.Error != nil || r.Connection == nil || r.Status != "completed" {
		t.Fatalf("open: %+v %v", r, err)
	}
	return *r.Connection
}

func TestPoolReuseCloseAndIdleLRU(t *testing.T) {
	opts := poolOptions(t)
	now := time.Now()
	opts.Now = func() time.Time { return now }
	opts.MaintenanceInterval = time.Hour
	opts.Limits = Limits{Connections: 2, Transports: 1, Closed: 1, IdleTimeout: time.Minute}
	var dials atomic.Int32
	dial := opts.Dial
	opts.Dial = func(ctx context.Context, r ResolvedTarget) (*ssh.Client, error) { dials.Add(1); return dial(ctx, r) }
	e := NewEngine(opts)
	defer e.Shutdown(context.Background())
	a := openTest(t, e, 1, "a")
	if b := openTest(t, e, 2, "a"); b.ID != a.ID || dials.Load() != 1 {
		t.Fatal("healthy connection not reused")
	}
	now = now.Add(time.Second)
	b := openTest(t, e, 3, "b")
	a, _ = e.GetConnection(context.Background(), ConnectionInput{ConnectionID: a.ID})
	if a.TransportConnected {
		t.Fatal("transport capacity did not release idle socket")
	}
	now = now.Add(time.Second)
	c := openTest(t, e, 4, "c")
	a, _ = e.GetConnection(context.Background(), ConnectionInput{ConnectionID: a.ID})
	if a.Status != "closed" || a.CloseReason != "capacity" {
		t.Fatalf("logical LRU: %+v", a)
	}
	now = now.Add(2 * time.Minute)
	e.maintain()
	c, _ = e.GetConnection(context.Background(), ConnectionInput{ConnectionID: c.ID})
	if c.Status != "disconnected" || c.TransportConnected {
		t.Fatalf("idle: %+v", c)
	}
	if _, err := e.CloseConnection(context.Background(), CloseInput{ConnectionID: b.ID}); err != nil {
		t.Fatal(err)
	}
	_, err := e.GetConnection(context.Background(), ConnectionInput{ConnectionID: a.ID})
	assertFault(t, err, "CONNECTION_NOT_FOUND")
	_, err = e.GetConnection(context.Background(), ConnectionInput{ConnectionID: "previous:c:1"})
	assertFault(t, err, "INSTANCE_CHANGED")
}

func TestPoolConcurrentOpenAndClosedLateDial(t *testing.T) {
	opts := poolOptions(t)
	dial := opts.Dial
	gate, started := make(chan struct{}), make(chan struct{})
	var count atomic.Int32
	opts.Dial = func(ctx context.Context, r ResolvedTarget) (*ssh.Client, error) {
		if count.Add(1) == 1 {
			close(started)
		}
		select {
		case <-gate:
			return dial(ctx, r)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	e := NewEngine(opts)
	defer e.Shutdown(context.Background())
	var wg sync.WaitGroup
	for i := 1; i <= 8; i++ {
		wg.Add(1)
		go func(n int) { defer wg.Done(); openTest(t, e, n, "a") }(i)
	}
	<-started
	// 所有打开操作都应加入同一次拨号。
	time.Sleep(20 * time.Millisecond)
	close(gate)
	wg.Wait()
	if count.Load() != 1 {
		t.Fatalf("dials=%d", count.Load())
	}
	all, _ := e.ListConnections(context.Background(), struct{}{})
	if len(all) != 1 {
		t.Fatalf("connections=%d", len(all))
	}
	if _, err := e.CloseConnection(context.Background(), CloseInput{ConnectionID: all[0].ID}); err != nil {
		t.Fatal(err)
	}
	if got := openTest(t, e, 10, "a"); got.ID == all[0].ID {
		t.Fatal("closed ID revived")
	}
}
