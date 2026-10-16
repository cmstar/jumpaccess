package agentmcp

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func awaitConcurrency(t *testing.T, ready func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal(message)
}

func awaitConcurrencySignal(t *testing.T, ready <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal(message)
	}
}

func TestConcurrentExecuteSameKeySendsOnceAndDifferentKeyIsBusy(t *testing.T) {
	gate, sent := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	var sends atomic.Int32
	e, c := execFixture(t, func(ch ssh.Channel, _ string) {
		if sends.Add(1) == 1 {
			close(sent)
		}
		<-gate
		fmt.Fprint(ch, "one command")
		exitSSH(ch, 0)
	})
	t.Cleanup(release)
	in := executeInput(e, c, 2)
	const clients = 32
	type result struct {
		view ExecutionView
		err  error
	}
	start := make(chan struct{})
	results := make(chan result, clients)
	for range clients {
		go func() {
			<-start
			v, err := e.Execute(context.Background(), in)
			results <- result{v, err}
		}()
	}
	close(start)
	var executionID string
	for range clients {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		if executionID == "" {
			executionID = r.view.ID
		}
		if r.view.ID != executionID {
			t.Fatalf("同键并发请求产生多个执行：%q、%q", executionID, r.view.ID)
		}
	}
	awaitConcurrencySignal(t, sent, "未收到原始 exec 请求")
	_, err := e.Execute(context.Background(), executeInput(e, c, 3))
	assertFault(t, err, "CONNECTION_BUSY")
	if sends.Load() != 1 {
		t.Fatalf("同键并发请求发送 %d 次命令", sends.Load())
	}
	release()
	v := awaitExecution(t, e, executionID)
	if v.Status != "completed" || v.ExitCode == nil || *v.ExitCode != 0 {
		t.Fatalf("执行结果：%+v", v)
	}
	// 终态重试也只返回原记录，不建立新通道。
	v, err = e.Execute(context.Background(), in)
	if err != nil || v.ID != executionID || sends.Load() != 1 {
		t.Fatalf("终态重试重新执行：%+v %v sends=%d", v, err, sends.Load())
	}
}

func TestCancelledReconnectWaiterDoesNotCancelOtherOpen(t *testing.T) {
	opts := poolOptions(t)
	var sends, dials atomic.Int32
	dial := testSSH(t, func(ch ssh.Channel, requests <-chan *ssh.Request) {
		defer ch.Close()
		for req := range requests {
			if req.Type == "exec" {
				sends.Add(1)
				_ = req.Reply(true, nil)
				exitSSH(ch, 0)
				return
			}
			_ = req.Reply(false, nil)
		}
	})
	entered, gate := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	opts.Dial = func(ctx context.Context, target ResolvedTarget) (*ssh.Client, error) {
		if dials.Add(1) == 2 {
			close(entered)
			select {
			case <-gate:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return dial(ctx, target)
	}
	e := NewEngine(opts)
	t.Cleanup(func() { e.Shutdown(context.Background()) })
	t.Cleanup(release)
	c := openTest(t, e, 1, "a")
	e.mu.Lock()
	e.dropLocked(e.connections[c.ID], "test_disconnect")
	e.mu.Unlock()
	job, err := e.Execute(context.Background(), executeInput(e, c, 2))
	if err != nil {
		t.Fatal(err)
	}
	awaitConcurrencySignal(t, entered, "执行未进入重连")
	type openResult struct {
		value OpenResult
		err   error
	}
	opened := make(chan openResult, 1)
	go func() {
		v, err := e.Open(context.Background(), OpenInput{Target: c.Target.Target, RequestKey: requestKey(e.RequestEpoch(), 3)})
		opened <- openResult{v, err}
	}()
	awaitConcurrency(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.connections[c.ID].refs > 0
	}, "第二个打开请求未加入共享重连")
	_, err = e.CancelExecution(context.Background(), CancelInput{ExecutionID: job.ID})
	if err != nil {
		t.Fatal(err)
	}
	job = awaitExecution(t, e, job.ID)
	if job.Status != "cancelled" || job.DispatchState != "not_sent" || !job.CancelConfirmed {
		t.Fatalf("重连等待取消结果：%+v", job)
	}
	release()
	var other openResult
	select {
	case other = <-opened:
	case <-time.After(3 * time.Second):
		t.Fatal("另一个等待者未完成重连")
	}
	if other.err != nil || other.value.Error != nil || other.value.Status != "completed" || other.value.Connection == nil {
		t.Fatalf("取消一个等待者影响另一个等待者：%+v %v", other.value, other.err)
	}
	if other.value.Connection.ID != c.ID || other.value.Connection.TransportGeneration != c.TransportGeneration+1 || dials.Load() != 2 || sends.Load() != 0 {
		t.Fatalf("共享重连状态错误：%+v dials=%d sends=%d", other.value.Connection, dials.Load(), sends.Load())
	}
	// 已取消请求的重试不得在恢复后的传输上重新发送。
	retried, err := e.Execute(context.Background(), executeInput(e, c, 2))
	if err != nil || retried.ID != job.ID || retried.Status != "cancelled" || sends.Load() != 0 {
		t.Fatalf("已取消请求在重连后重放：%+v %v", retried, err)
	}
}

func TestOldExecutionCancellationDoesNotAffectNewExecution(t *testing.T) {
	t.Run("same_transport", func(t *testing.T) { testOldExecutionIsolation(t, false) })
	t.Run("reconnected_transport", func(t *testing.T) { testOldExecutionIsolation(t, true) })
}

func testOldExecutionIsolation(t *testing.T, disconnect bool) {
	opts := poolOptions(t)
	var commands, signals, dials atomic.Int32
	started, gate := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	opts.Dial = testSSH(t, func(ch ssh.Channel, requests <-chan *ssh.Request) {
		defer ch.Close()
		for req := range requests {
			if req.Type != "exec" {
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			if commands.Add(1) == 1 {
				if !disconnect {
					exitSSH(ch, 0)
				}
				return
			}
			close(started)
			for {
				select {
				case req, ok := <-requests:
					if !ok {
						return
					}
					if req.Type == "signal" {
						signals.Add(1)
					}
					_ = req.Reply(false, nil)
				case <-gate:
					exitSSH(ch, 0)
					return
				}
			}
		}
	})
	dial := opts.Dial
	opts.Dial = func(ctx context.Context, target ResolvedTarget) (*ssh.Client, error) {
		dials.Add(1)
		return dial(ctx, target)
	}
	e := NewEngine(opts)
	t.Cleanup(func() { e.Shutdown(context.Background()) })
	t.Cleanup(release)
	c := openTest(t, e, 1, "a")
	old, err := e.Execute(context.Background(), executeInput(e, c, 2))
	if err != nil {
		t.Fatal(err)
	}
	old = awaitExecution(t, e, old.ID)
	if disconnect && (old.Status != "unknown" || !old.RemoteMayContinue) {
		t.Fatalf("发送后断连误报确定结果：%+v", old)
	}
	next, err := e.Execute(context.Background(), executeInput(e, c, 3))
	if err != nil {
		t.Fatal(err)
	}
	awaitConcurrencySignal(t, started, "新命令未发送")
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mode := "request_stop"
			if i%2 == 1 {
				mode = "detach"
			}
			var v ExecutionView
			var err error
			if i%3 == 0 {
				v, err = e.Execute(context.Background(), executeInput(e, c, 2))
			} else {
				v, err = e.CancelExecution(context.Background(), CancelInput{ExecutionID: old.ID, Mode: mode})
			}
			if err != nil || v.ID != old.ID || v.Status != old.Status {
				t.Errorf("旧请求重试或取消改变终态：%+v %v", v, err)
			}
		}()
	}
	wg.Wait()
	next, err = e.GetExecution(context.Background(), GetExecutionInput{ExecutionID: next.ID})
	if err != nil || terminal(next.Status) || next.CancelRequested || signals.Load() != 0 {
		t.Fatalf("旧取消干扰新命令：%+v %v signals=%d", next, err, signals.Load())
	}
	release()
	next = awaitExecution(t, e, next.ID)
	if next.Status != "completed" || next.ExitCode == nil || *next.ExitCode != 0 || commands.Load() != 2 || signals.Load() != 0 {
		t.Fatalf("新命令结果错误：%+v commands=%d signals=%d", next, commands.Load(), signals.Load())
	}
	wantDials := int32(1)
	if disconnect {
		wantDials++
	}
	if dials.Load() != wantDials || next.TransportGeneration != uint64(wantDials) {
		t.Fatalf("传输未按预期复用或重连：dials=%d generation=%d", dials.Load(), next.TransportGeneration)
	}
}

func TestCloseRacesWithAdmittedCommandBeforeSend(t *testing.T) {
	for _, mode := range []string{"drain", "force"} {
		t.Run(mode, func(t *testing.T) {
			opts := poolOptions(t)
			var sends atomic.Int32
			opts.Dial = testSSH(t, func(ch ssh.Channel, requests <-chan *ssh.Request) {
				defer ch.Close()
				for req := range requests {
					if req.Type == "exec" {
						sends.Add(1)
						_ = req.Reply(true, nil)
						exitSSH(ch, 0)
						return
					}
					_ = req.Reply(false, nil)
				}
			})
			var block atomic.Bool
			entered, gate := make(chan struct{}), make(chan struct{})
			release := sync.OnceFunc(func() { close(gate) })
			enter := sync.OnceFunc(func() { close(entered) })
			opts.Validate = func(ctx context.Context, _ ResolvedTarget) error {
				if block.Load() {
					enter()
					select {
					case <-gate:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				return nil
			}
			e := NewEngine(opts)
			t.Cleanup(func() { e.Shutdown(context.Background()) })
			t.Cleanup(release)
			c := openTest(t, e, 1, "a")
			block.Store(true)
			job, err := e.Execute(context.Background(), executeInput(e, c, 2))
			if err != nil {
				t.Fatal(err)
			}
			awaitConcurrencySignal(t, entered, "已登记执行未进入发送前身份校验")
			type closeResult struct {
				view ConnectionView
				err  error
			}
			closed := make(chan closeResult, 1)
			go func() {
				v, err := e.CloseConnection(context.Background(), CloseInput{ConnectionID: c.ID, Mode: mode})
				closed <- closeResult{v, err}
			}()
			awaitConcurrency(t, func() bool {
				e.mu.Lock()
				defer e.mu.Unlock()
				return e.connections[c.ID].closing
			}, "连接未进入关闭状态")
			_, err = e.Execute(context.Background(), executeInput(e, c, 3))
			assertFault(t, err, "CONNECTION_CLOSED")
			if mode == "drain" {
				// 已登记的命令可以完成；关闭仅阻止后续准入。
				release()
			}
			job = awaitExecution(t, e, job.ID)
			var result closeResult
			select {
			case result = <-closed:
			case <-time.After(3 * time.Second):
				t.Fatal("连接未在执行结束后关闭")
			}
			if result.err != nil || result.view.Status != "closed" {
				t.Fatalf("关闭结果：%+v %v", result.view, result.err)
			}
			if mode == "drain" {
				if job.Status != "completed" || job.ExitCode == nil || *job.ExitCode != 0 || sends.Load() != 1 {
					t.Fatalf("drain 丢失已登记命令：%+v sends=%d", job, sends.Load())
				}
			} else if job.Status != "cancelled" || job.DispatchState != "not_sent" || job.RemoteMayContinue || sends.Load() != 0 {
				t.Fatalf("force 未在发送前取消：%+v sends=%d", job, sends.Load())
			}
		})
	}
}
