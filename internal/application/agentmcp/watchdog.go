package agentmcp

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

//go:embed watchdog.py
var watchdogScript string

func remoteCommand(in ExecuteInput, marker string) string {
	payload, _ := json.Marshal(map[string]any{"marker": marker, "command": in.Command, "cwd": in.Cwd, "env": in.Env, "timeout_ms": in.TimeoutMS})
	return pythonCommand(payload)
}
func pythonCommand(payload []byte) string {
	return "exec python3 -c " + shellQuote(watchdogScript) + " " + shellQuote(base64.StdEncoding.EncodeToString(payload))
}

func stopMessage(marker string) string { return marker + ":stop\n" }

type controlResult struct {
	Event      string `json:"event"`
	ExitCode   *int   `json:"exit_code"`
	ExitSignal string `json:"exit_signal"`
	TimedOut   bool   `json:"timed_out"`
	Cancelled  bool   `json:"cancelled"`
}

// 探测独占本连接的执行位置；结果只写回发起探测的实际连接代次。
func (e *Engine) probeDeadline(ctx context.Context, job *execution, client *ssh.Client) error {
	e.mu.Lock()
	capability := job.connection.view.RemoteDeadlineCapability
	e.mu.Unlock()
	if capability == "supported" {
		return nil
	}
	if capability == "unsupported" {
		return fault("REMOTE_DEADLINE_UNAVAILABLE", "目标未提供可验证的 POSIX/Python 3 远端时限与停止控制能力")
	}
	probeCtx, cancel := context.WithTimeout(ctx, e.opts.Limits.ChannelTimeout)
	defer cancel()
	ch, requests, err := e.openChannel(probeCtx, client, job.connection)
	if err != nil {
		return fault("REMOTE_DEADLINE_UNAVAILABLE", "远端时限探测通道不可用")
	}
	defer e.closeChannel(ch, client, job.connection)
	payload, _ := json.Marshal(map[string]any{"probe": true, "marker": job.marker})
	buffer := limitedProbe{ready: make(chan struct{}), marker: job.marker}
	type probeResult struct{ supported, transportFailed bool }
	finished := make(chan probeResult, 1)
	go func() {
		ok, err := ch.SendRequest("exec", true, ssh.Marshal(struct{ Command string }{pythonCommand(payload)}))
		if err != nil || !ok {
			finished <- probeResult{transportFailed: err != nil}
			return
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = io.Copy(io.Discard, ch) }()
		go func() { defer wg.Done(); _, _ = io.Copy(&buffer, ch.Stderr()) }()
		exited := make(chan probeResult, 1)
		go func() {
			result := probeResult{transportFailed: true}
			for req := range requests {
				if req.Type == "exit-status" {
					var v struct{ Code uint32 }
					if ssh.Unmarshal(req.Payload, &v) == nil {
						result = probeResult{supported: v.Code == 0}
					}
				}
				_ = req.Reply(false, nil)
			}
			wg.Wait()
			exited <- result
		}()
		select {
		case <-buffer.ready:
			if _, err := io.WriteString(ch, stopMessage(job.marker)); err != nil {
				finished <- probeResult{transportFailed: true}
				return
			}
		case result := <-exited:
			result.supported = false
			finished <- result
			return
		case <-probeCtx.Done():
			return
		}
		select {
		case result := <-exited:
			result.supported = result.supported && strings.Contains(buffer.text(), "\x1e"+job.marker+`:{"event":"probe","supported":true}`+"\x1f")
			finished <- result
		case <-probeCtx.Done():
		}
	}()
	ok := false
	select {
	case result := <-finished:
		if result.transportFailed || probeCtx.Err() != nil {
			e.invalidate(job.connection, client)
			return fault("REMOTE_DEADLINE_UNAVAILABLE", "远端控制探测通信失败或取消，原始命令未发送")
		}
		ok = result.supported
	case <-probeCtx.Done():
		e.invalidate(job.connection, client)
		return fault("REMOTE_DEADLINE_UNAVAILABLE", "远端时限探测超时或取消，原始命令未发送")
	}
	e.mu.Lock()
	if job.connection.client == client {
		if ok {
			job.connection.view.ExecCapability = "supported"
			job.connection.view.RemoteDeadlineCapability = "supported"
		} else {
			job.connection.view.RemoteDeadlineCapability = "unsupported"
		}
	}
	e.mu.Unlock()
	if !ok {
		return fault("REMOTE_DEADLINE_UNAVAILABLE", "目标未提供可验证的 POSIX/Python 3 远端时限与停止控制能力")
	}
	return nil
}

type limitedProbe struct {
	mu     sync.Mutex
	value  []byte
	ready  chan struct{}
	marker string
	once   sync.Once
}

func (b *limitedProbe) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := min(len(p), 8192-len(b.value))
	b.value = append(b.value, p[:n]...)
	if strings.Contains(string(b.value), "\x1e"+b.marker+`:{"event":"ready"}`+"\x1f") {
		b.once.Do(func() { close(b.ready) })
	}
	return len(p), nil
}
func (b *limitedProbe) text() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.value) }
