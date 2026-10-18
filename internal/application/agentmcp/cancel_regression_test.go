package agentmcp

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// exec 被拒绝后的 channel 清理期间，迟到的取消不能再启动停止协程。
func TestCancelDuringFinalizationDoesNotStartStopWorker(t *testing.T) {
	e := NewEngine(EngineOptions{Instance: "test"})
	t.Cleanup(e.cancel)
	ctx, cancel := context.WithCancel(e.ctx)
	t.Cleanup(cancel)
	c := &connection{view: ConnectionView{ActiveExecutionID: "test:x:1"}}
	job := &execution{
		view:       ExecutionView{ID: "test:x:1", Status: "starting", DispatchState: "possibly_sent", AcceptedAt: e.opts.Now()},
		connection: c, ctx: ctx, cancel: cancel,
		changed: make(chan struct{}), done: make(chan struct{}), dispatchReady: make(chan struct{}),
		record: &requestRecord{done: make(chan struct{})},
	}
	close(job.dispatchReady)
	job.channel = &finalizationTestChannel{onClose: func() error {
		e.mu.Lock()
		e.cancelLocked(job, false)
		stopDone, requested := job.stopDone, job.view.CancelRequested
		e.mu.Unlock()
		// 旧实现会创建协程；先让它退出，避免断言失败时泄漏后台任务。
		if stopDone != nil {
			<-stopDone
		}
		if requested || stopDone != nil {
			t.Error("终态清理期间仍接受取消并启动停止协程")
		}
		return nil
	}}
	e.mu.Lock()
	e.requests.active = 1
	e.executions[job.view.ID] = job
	e.history.addLocked(job.view.ID, true, e).job = job
	e.mu.Unlock()
	e.finishExecution(job, "failed", "exec_rejected", fault("EXEC_REJECTED", "服务端拒绝 exec"))
	if job.view.Status != "failed" || job.view.Reason != "exec_rejected" || c.view.ActiveExecutionID != "" {
		t.Fatalf("收尾未保留原结果或释放执行位置: %+v", job.view)
	}
	if job.channel != nil || job.client != nil || job.connection != nil {
		t.Fatal("终态历史仍持有 SSH 运行资源")
	}
}

type finalizationTestChannel struct {
	ssh.Channel
	onClose func() error
}

func (c *finalizationTestChannel) Close() error { return c.onClose() }

// 用真实 SSH channel 延迟 exec 应答，检查取消不会早于远端接受执行。
func TestCancelWaitsForExecAcknowledgement(t *testing.T) {
	for _, strategy := range []string{"local", "remote_required"} {
		for _, response := range []string{"accepted", "rejected", "disconnected", "timeout"} {
			t.Run(strategy+"/"+response, func(t *testing.T) {
				t.Parallel()
				entered, release := make(chan struct{}), make(chan struct{})
				unblock := sync.OnceFunc(func() { close(release) })
				var signals, controlBytes atomic.Int32
				opts := poolOptions(t)
				opts.Limits.ChannelTimeout = 2 * time.Second
				if response == "timeout" {
					opts.Limits.ChannelTimeout = 400 * time.Millisecond
				}
				opts.Dial = testSSH(t, func(ch ssh.Channel, requests <-chan *ssh.Request) {
					defer ch.Close()
					req, ok := <-requests
					if !ok || req.Type != "exec" {
						return
					}
					var marker string
					if strategy == "remote_required" {
						payload, ok := cancelTestPayload(t, req.Payload)
						if !ok {
							return
						}
						marker = payload.Marker
						if payload.Probe {
							cancelTestProbe(t, ch, req, marker)
							return
						}
					}
					stdin := make(chan string, 1)
					go func() {
						data, _ := bufio.NewReader(ch).ReadString('\n')
						controlBytes.Add(int32(len(data)))
						stdin <- data
					}()
					signalled := make(chan struct{}, 1)
					go func() {
						for req := range requests {
							if req.Type == "signal" {
								signals.Add(1)
								select {
								case signalled <- struct{}{}:
								default:
								}
							}
							_ = req.Reply(false, nil)
						}
					}()
					close(entered)
					<-release
					if response == "disconnected" || response == "timeout" {
						return
					}
					_ = req.Reply(response == "accepted", nil)
					if response != "accepted" {
						return
					}
					if strategy == "remote_required" {
						select {
						case data := <-stdin:
							if data != marker+":stop\n" {
								t.Errorf("停止控制消息不属于本次执行：%q", data)
								return
							}
						case <-time.After(3 * time.Second):
							t.Error("exec 接受后未发送 stdin 停止消息")
							return
						}
						_, _ = fmt.Fprintf(ch.Stderr(), "\x1e%s:{\"event\":\"result\",\"exit_code\":null,\"exit_signal\":\"TERM\",\"cancelled\":true}\x1f", marker)
						exitSSH(ch, 0)
					} else {
						select {
						case <-signalled:
							_, _ = ch.SendRequest("exit-signal", false, ssh.Marshal(struct {
								Signal            string
								Core              bool
								Message, Language string
							}{Signal: "TERM"}))
						case <-time.After(3 * time.Second):
							t.Error("exec 接受后未发送 SSH signal")
						}
					}
				})
				e := NewEngine(opts)
				t.Cleanup(func() { e.Shutdown(context.Background()) })
				t.Cleanup(unblock)
				c := openTest(t, e, 1, "a")
				in := executeInput(e, c, 2)
				in.TimeoutStrategy = strategy
				job, err := e.Execute(context.Background(), in)
				if err != nil {
					t.Fatal(err)
				}
				awaitConcurrencySignal(t, entered, "exec 未到达测试服务")
				cancelTestRequest(t, e, job.ID)
				// 给取消发送者调度时间；服务端尚未回 exec ack，不应收到任何停止操作。
				time.Sleep(50 * time.Millisecond)
				if signals.Load() != 0 || controlBytes.Load() != 0 {
					t.Fatalf("exec 应答前发送了取消：signals=%d bytes=%d", signals.Load(), controlBytes.Load())
				}
				if response != "timeout" {
					unblock()
				}
				job = awaitExecution(t, e, job.ID)
				switch response {
				case "accepted":
					if strategy == "remote_required" && (job.Status != "cancelled" || !job.CancelConfirmed || signals.Load() != 0) {
						t.Fatalf("stdin 取消结果错误：%+v signals=%d", job, signals.Load())
					}
					if strategy == "local" && (job.ExitSignal != "TERM" || signals.Load() != 1 || controlBytes.Load() != 0) {
						t.Fatalf("SSH signal 结果错误：%+v signals=%d bytes=%d", job, signals.Load(), controlBytes.Load())
					}
				case "rejected":
					if job.Status != "failed" || job.Error == nil || job.Error.Code != "EXEC_REJECTED" || job.CancelConfirmed {
						t.Fatalf("exec 拒绝被误报或取消清理未完成：%+v", job)
					}
				default:
					if job.Status != "unknown" || job.Error == nil || job.Error.Code != "EXEC_ACK_LOST" || job.CancelConfirmed || !job.RemoteMayContinue {
						t.Fatalf("exec 应答丢失被误报或取消清理未完成：%+v", job)
					}
				}
				if response != "accepted" && (signals.Load() != 0 || controlBytes.Load() != 0) {
					t.Fatal("未确认接受 exec 的任务仍发送了停止操作")
				}
			})
		}
	}
}

// 信号被忽略时，只能在确认期限后报告未知，不能伪造取消成功。
func TestIgnoredCancelBecomesUnknownAfterConfirmationDeadline(t *testing.T) {
	t.Parallel()
	var commands, signals atomic.Int32
	signalSeen := make(chan struct{}, 1)
	opts := poolOptions(t)
	opts.Dial = testSSH(t, func(ch ssh.Channel, requests <-chan *ssh.Request) {
		defer ch.Close()
		for req := range requests {
			if req.Type == "exec" {
				_ = req.Reply(true, nil)
				if commands.Add(1) > 1 {
					_, _ = fmt.Fprint(ch, "next-ok")
					exitSSH(ch, 0)
					return
				}
				_, _ = fmt.Fprint(ch, "ready")
			} else if req.Type == "signal" {
				signals.Add(1)
				select {
				case signalSeen <- struct{}{}:
				default:
				}
			}
		}
	})
	e := NewEngine(opts)
	t.Cleanup(func() { e.Shutdown(context.Background()) })
	c := openTest(t, e, 1, "a")
	in := executeInput(e, c, 2)
	in.TimeoutMS = 30000
	job, err := e.Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	awaitOutput(t, e, job.ID, "ready")
	started := time.Now()
	cancelTestRequest(t, e, job.ID)
	awaitConcurrencySignal(t, signalSeen, "未收到首次停止请求")
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); cancelTestRequest(t, e, job.ID) }()
	}
	wg.Wait()
	job = cancelTestAwaitTerminal(t, e, job.ID, 13*time.Second)
	if job.Status != "unknown" || job.CancelConfirmed || !job.RemoteMayContinue || job.Reason != "cancel_unconfirmed" || job.CancelError == nil || job.CancelError.Code != "CANCEL_UNCONFIRMED" {
		t.Fatalf("忽略停止请求后的结果不诚实：%+v", job)
	}
	if time.Since(started) < 9*time.Second || signals.Load() != 1 {
		t.Fatalf("取消未共用确认期限或重复发送：elapsed=%s signals=%d", time.Since(started), signals.Load())
	}
	next, err := e.Execute(context.Background(), executeInput(e, c, 3))
	if err != nil {
		t.Fatal(err)
	}
	cancelTestRequest(t, e, job.ID)
	next = awaitExecution(t, e, next.ID)
	if next.Status != "completed" || next.ExitCode == nil || *next.ExitCode != 0 || next.CancelRequested || next.TransportGeneration <= job.TransportGeneration || outputText(next) != "next-ok" {
		t.Fatalf("未知结果后的重连或旧取消污染新任务：%+v", next)
	}
}

// 自然退出在取消确认期限前完成后，旧计时器不能关掉后续复用该 transport 的任务。
func TestNaturalExitCancelsOldStopConfirmationTimer(t *testing.T) {
	t.Parallel()
	var commands, signals atomic.Int32
	newStarted, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	opts := poolOptions(t)
	opts.Dial = testSSH(t, func(ch ssh.Channel, requests <-chan *ssh.Request) {
		defer ch.Close()
		first := false
		for {
			select {
			case req, ok := <-requests:
				if !ok {
					return
				}
				if req.Type == "exec" {
					first = commands.Add(1) == 1
					_ = req.Reply(true, nil)
					if first {
						_, _ = fmt.Fprint(ch, "ready")
					} else {
						close(newStarted)
					}
				} else if req.Type == "signal" {
					signals.Add(1)
					if first {
						// 模拟原命令自然退出；仅收到取消请求不能把正常 exit 0 改成 cancelled。
						exitSSH(ch, 0)
						return
					}
				}
			case <-release:
				exitSSH(ch, 0)
				return
			}
		}
	})
	e := NewEngine(opts)
	t.Cleanup(func() { e.Shutdown(context.Background()) })
	t.Cleanup(unblock)
	c := openTest(t, e, 1, "a")
	in := executeInput(e, c, 2)
	in.TimeoutMS = 30000
	old, err := e.Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	awaitOutput(t, e, old.ID, "ready")
	cancelTestRequest(t, e, old.ID)
	old = awaitExecution(t, e, old.ID)
	if old.Status != "completed" || old.ExitCode == nil || *old.ExitCode != 0 || old.CancelConfirmed {
		t.Fatalf("取消请求覆盖了自然退出证据：%+v", old)
	}
	in = executeInput(e, c, 3)
	in.TimeoutMS = 30000
	next, err := e.Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	awaitConcurrencySignal(t, newStarted, "后续任务未启动")
	for range 8 {
		cancelTestRequest(t, e, old.ID)
	}
	// 等过旧的十秒确认期限，检查实际 SSH transport 和运行状态均未被旧任务清理。
	time.Sleep(11 * time.Second)
	next, err = e.GetExecution(context.Background(), GetExecutionInput{ExecutionID: next.ID})
	if err != nil || next.Status != "running" || next.CancelRequested || next.TransportGeneration != old.TransportGeneration || signals.Load() != 1 {
		t.Fatalf("旧取消计时器污染复用任务：%+v %v signals=%d", next, err, signals.Load())
	}
	unblock()
	next = awaitExecution(t, e, next.ID)
	if next.Status != "completed" || next.ExitCode == nil || *next.ExitCode != 0 {
		t.Fatalf("后续任务未正常退出：%+v", next)
	}
}

func cancelTestRequest(t *testing.T, e *Engine, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	v, err := e.CancelExecution(ctx, CancelInput{ExecutionID: id})
	if err != nil || !v.CancelRequested {
		t.Errorf("取消请求未登记：%+v %v", v, err)
	}
}

func cancelTestAwaitTerminal(t *testing.T, e *Engine, id string, timeout time.Duration) ExecutionView {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		v, err := e.GetExecution(context.Background(), GetExecutionInput{ExecutionID: id, WaitMS: 100})
		if err != nil {
			t.Fatal(err)
		}
		if terminal(v.Status) {
			return v
		}
	}
	t.Fatal("取消确认期限后执行仍未结束")
	return ExecutionView{}
}

type cancelProbePayload struct {
	Marker string `json:"marker"`
	Probe  bool   `json:"probe"`
}

// 这里只模拟 wrapper 的线协议；Python 与 stdin 转发本身由 WSL 集成回归覆盖。
func cancelTestPayload(t *testing.T, wire []byte) (cancelProbePayload, bool) {
	t.Helper()
	var request struct{ Command string }
	var payload cancelProbePayload
	if err := ssh.Unmarshal(wire, &request); err != nil {
		t.Error(err)
		return payload, false
	}
	index := strings.LastIndex(request.Command, " '")
	if index < 0 {
		t.Error("缺少 wrapper 参数")
		return payload, false
	}
	data, err := base64.StdEncoding.DecodeString(strings.Trim(request.Command[index+1:], "'"))
	if err != nil {
		t.Error(err)
		return payload, false
	}
	if err = json.Unmarshal(data, &payload); err != nil || payload.Marker == "" {
		t.Errorf("无效 wrapper 参数：%v", err)
		return payload, false
	}
	return payload, true
}

func cancelTestProbe(t *testing.T, ch ssh.Channel, req *ssh.Request, marker string) {
	t.Helper()
	_ = req.Reply(true, nil)
	_, _ = fmt.Fprintf(ch.Stderr(), "\x1e%s:{\"event\":\"ready\"}\x1f", marker)
	line, err := bufio.NewReader(ch).ReadString('\n')
	if err != nil || line != marker+":stop\n" {
		t.Errorf("探测未收到对应控制帧：%q %v", line, err)
		return
	}
	_, _ = fmt.Fprintf(ch.Stderr(), "\x1e%s:{\"event\":\"probe\",\"supported\":true}\x1f", marker)
	exitSSH(ch, 0)
}
