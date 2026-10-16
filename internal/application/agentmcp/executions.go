package agentmcp

import (
	"container/list"
	"context"
	"crypto/rand"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"
)

type ExecuteInput struct {
	ConnectionID    string            `json:"connection_id"`
	RequestKey      string            `json:"request_key"`
	Command         string            `json:"command"`
	Cwd             string            `json:"cwd"`
	Env             map[string]string `json:"env,omitempty"`
	TimeoutMS       int               `json:"timeout_ms,omitempty"`
	TimeoutStrategy string            `json:"timeout_strategy,omitempty"`
	WaitMS          *int              `json:"wait_ms,omitempty"`
}
type GetExecutionInput struct {
	ExecutionID string `json:"execution_id"`
	Cursor      uint64 `json:"cursor,omitempty"`
	MaxBytes    int    `json:"max_bytes,omitempty"`
	WaitMS      int    `json:"wait_ms,omitempty"`
}
type ListExecutionsInput struct {
	ConnectionID string `json:"connection_id,omitempty"`
	Status       string `json:"status,omitempty"`
}
type ExecutionView struct {
	ID                  string         `json:"execution_id"`
	ConnectionID        string         `json:"connection_id"`
	Target              ResolvedTarget `json:"target"`
	TransportGeneration uint64         `json:"transport_generation"`
	Status              string         `json:"status"`
	DispatchState       string         `json:"dispatch_state"`
	RemoteState         string         `json:"remote_state"`
	RemoteMayContinue   bool           `json:"remote_may_continue"`
	AcceptedAt          time.Time      `json:"accepted_at"`
	PrepareDeadlineAt   time.Time      `json:"prepare_deadline_at"`
	StartedAt           *time.Time     `json:"started_at,omitempty"`
	FinishedAt          *time.Time     `json:"finished_at,omitempty"`
	ExitCode            *int           `json:"exit_code"`
	ExitSignal          string         `json:"exit_signal,omitempty"`
	Error               *Fault         `json:"error,omitempty"`
	Reason              string         `json:"reason,omitempty"`
	TimeoutMS           int            `json:"timeout_ms"`
	TimeoutStrategy     string         `json:"timeout_strategy"`
	CancelRequested     bool           `json:"cancel_requested"`
	CancelConfirmed     bool           `json:"cancel_confirmed"`
	Output              OutputView     `json:"output"`
}
type execution struct {
	view           ExecutionView
	input          ExecuteInput
	record         *requestRecord
	connection     *connection
	ctx            context.Context
	cancel         context.CancelFunc
	changed        chan struct{}
	done           chan struct{}
	channel        ssh.Channel
	client         *ssh.Client
	output         list.List
	outputBytes    int
	outputSequence uint64
	droppedBytes   int64
	incomplete     bool
	marker         string
	contextFailed  bool
}

func terminal(status string) bool {
	switch status {
	case "completed", "failed", "timed_out", "cancelled", "unknown":
		return true
	}
	return false
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func normalizeExecution(in ExecuteInput) (ExecuteInput, int, error) {
	wait := 1000
	if in.WaitMS != nil {
		wait = *in.WaitMS
	}
	in.WaitMS = nil
	in.RequestKey = ""
	if in.TimeoutMS == 0 {
		in.TimeoutMS = 300000
	}
	if in.TimeoutStrategy == "" {
		in.TimeoutStrategy = "remote_required"
	}
	if len(in.Command) == 0 || len(in.Command) > 64<<10 || !utf8.ValidString(in.Command) || strings.ContainsRune(in.Command, 0) || !strings.HasPrefix(in.Cwd, "/") || strings.ContainsRune(in.Cwd, 0) || len(in.Cwd) > 4096 {
		return in, wait, fault("INVALID_ARGUMENT", "命令需为不含 NUL 的 UTF-8 文本（最多 64 KiB），cwd 必须为绝对路径")
	}
	if wait < 0 || wait > 5000 || in.TimeoutMS < 1 || in.TimeoutMS > 3600000 || (in.TimeoutStrategy != "local" && in.TimeoutStrategy != "remote_required") {
		return in, wait, fault("INVALID_ARGUMENT", "等待窗口、时限或时限策略无效")
	}
	env := make(map[string]string, len(in.Env))
	size := 0
	for k, v := range in.Env {
		size += len(k) + len(v)
		if !envName.MatchString(k) || strings.ContainsRune(v, 0) || !utf8.ValidString(v) {
			return in, wait, fault("INVALID_ARGUMENT", "环境变量名或值无效")
		}
		env[k] = v
	}
	if size > 64<<10 || len(env) > 256 {
		return in, wait, fault("INVALID_ARGUMENT", "环境变量过大")
	}
	in.Env = env
	return in, wait, nil
}

func (e *Engine) Execute(ctx context.Context, in ExecuteInput) (ExecutionView, error) {
	key := in.RequestKey
	normalized, wait, err := normalizeExecution(in)
	if err != nil {
		return ExecutionView{}, err
	}
	if err := ctx.Err(); err != nil {
		return ExecutionView{}, err
	}
	e.mu.Lock()
	var job *execution
	record, fresh, err := e.requests.begin("execute", key, normalized, func() (string, error) {
		if e.closing {
			return "", fault("SERVICE_DRAINING", "服务正在停止")
		}
		c, err := e.findConnection(in.ConnectionID)
		if err != nil {
			return "", err
		}
		if c.closing || c.view.Status == "closed" {
			return "", fault("CONNECTION_CLOSED", "连接已关闭或正在关闭")
		}
		if c.view.ActiveExecutionID != "" {
			return "", fault("CONNECTION_BUSY", "该共享连接已有活动命令")
		}
		e.pruneExecutionsLocked()
		if len(e.executions) >= e.opts.Limits.Executions {
			if len(e.executionHistory) == 0 {
				return "", fault("LIMIT_EXCEEDED", "执行记录全部活跃")
			}
			e.evictExecution(e.executionHistory[0])
			e.executionHistory = e.executionHistory[1:]
		}
		now := e.opts.Now()
		id := e.nextID("x")
		jobCtx, cancel := context.WithCancel(e.ctx)
		job = &execution{view: ExecutionView{ID: id, ConnectionID: c.view.ID, Target: c.view.Target, Status: "starting", DispatchState: "not_sent", RemoteState: "not_started", AcceptedAt: now, PrepareDeadlineAt: time.Now().Add(60 * time.Second), TimeoutMS: normalized.TimeoutMS, TimeoutStrategy: normalized.TimeoutStrategy}, input: normalized, connection: c, ctx: jobCtx, cancel: cancel, changed: make(chan struct{}), done: make(chan struct{}), marker: rand.Text()}
		c.view.ActiveExecutionID = id
		c.view.LastUsedAt = now
		e.executions[id] = job
		return id, nil
	})
	if err == nil {
		if fresh {
			job.record = record
			go e.runExecution(job)
		} else {
			job = e.executions[record.ResourceID]
		}
	}
	e.mu.Unlock()
	if err != nil {
		return ExecutionView{}, err
	}
	if job == nil {
		return ExecutionView{}, fault("EXECUTION_EXPIRED", "执行详细结果已回收，不会重新执行")
	}
	e.waitExecution(ctx, job, wait, false)
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.executionView(job, 0, 64<<10), nil
}

func (e *Engine) GetExecution(ctx context.Context, in GetExecutionInput) (ExecutionView, error) {
	if in.WaitMS < 0 || in.WaitMS > 5000 || (in.MaxBytes != 0 && (in.MaxBytes < outputBlockSize || in.MaxBytes > 1<<20)) {
		return ExecutionView{}, fault("INVALID_ARGUMENT", "输出页须为 32 KiB–1 MiB，等待窗口为 0–5000ms")
	}
	if in.MaxBytes == 0 {
		in.MaxBytes = 64 << 10
	}
	e.mu.Lock()
	if err := e.checkID(in.ExecutionID); err != nil {
		e.mu.Unlock()
		return ExecutionView{}, err
	}
	job := e.executions[in.ExecutionID]
	if job == nil {
		e.mu.Unlock()
		if e.requests.knows(in.ExecutionID) {
			return ExecutionView{}, fault("EXECUTION_EXPIRED", "执行详细结果已回收；原命令不会重跑")
		}
		return ExecutionView{}, fault("EXECUTION_NOT_FOUND", "执行不存在或详细结果已回收")
	}
	if in.Cursor > job.outputSequence {
		e.mu.Unlock()
		return ExecutionView{}, fault("INVALID_ARGUMENT", "输出游标超出本执行范围")
	}
	wait := in.WaitMS
	if job.outputSequence > in.Cursor {
		wait = 0
	}
	e.mu.Unlock()
	e.waitExecution(ctx, job, wait, true)
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.executionView(job, in.Cursor, in.MaxBytes), nil
}
func (e *Engine) waitExecution(ctx context.Context, job *execution, ms int, anyChange bool) {
	if ms == 0 {
		return
	}
	e.mu.Lock()
	if terminal(job.view.Status) {
		e.mu.Unlock()
		return
	}
	changed := job.done
	if anyChange {
		changed = job.changed
	}
	e.mu.Unlock()
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-changed:
	case <-timer.C:
	}
}
func (e *Engine) ListExecutions(_ context.Context, in ListExecutionsInput) ([]ExecutionView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if in.ConnectionID != "" {
		if err := e.checkID(in.ConnectionID); err != nil {
			return nil, err
		}
	}
	out := []ExecutionView{}
	for _, job := range e.executions {
		if (in.ConnectionID == "" || job.view.ConnectionID == in.ConnectionID) && (in.Status == "" || job.view.Status == in.Status) {
			v := job.view
			v.Output = OutputView{Chunks: []OutputChunk{}}
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AcceptedAt.Before(out[j].AcceptedAt) })
	return out, nil
}
func (e *Engine) executionView(job *execution, cursor uint64, maxBytes int) ExecutionView {
	v := job.view
	v.Output = e.outputView(job, cursor, maxBytes)
	return v
}
func (e *Engine) notifyExecution(job *execution) {
	close(job.changed)
	job.changed = make(chan struct{})
}
func (e *Engine) finishExecution(job *execution, status, reason string, err *Fault) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if terminal(job.view.Status) {
		return
	}
	now := e.opts.Now()
	job.view.Status = status
	job.view.Reason = reason
	job.view.Error = err
	job.view.FinishedAt = &now
	if status == "unknown" {
		job.view.RemoteState = "unknown"
		job.view.RemoteMayContinue = true
		job.incomplete = true
	} else if job.view.ExitCode != nil || job.view.ExitSignal != "" {
		job.view.RemoteState = "finished"
	}
	if job.connection.view.ActiveExecutionID == job.view.ID {
		job.connection.view.ActiveExecutionID = ""
		job.connection.view.LastUsedAt = now
	}
	job.input = ExecuteInput{}
	job.cancel()
	close(job.done)
	e.notifyExecution(job)
	e.requests.finish(job.record, err)
	e.executionHistory = append(e.executionHistory, job)
	e.pruneExecutionsLocked()
}
func (e *Engine) evictExecution(job *execution) {
	for job.output.Len() > 0 {
		e.removeOutput(job.output.Front().Value.(*outputBlock), false)
	}
	delete(e.executions, job.view.ID)
}
func (e *Engine) pruneExecutionsLocked() {
	for len(e.executionHistory) > 0 && (len(e.executions) > e.opts.Limits.Executions || e.opts.Now().Sub(*e.executionHistory[0].view.FinishedAt) >= e.opts.Limits.HistoryTTL) {
		job := e.executionHistory[0]
		e.executionHistory[0] = nil
		e.executionHistory = e.executionHistory[1:]
		e.evictExecution(job)
	}
}

type openedChannel struct {
	channel  ssh.Channel
	requests <-chan *ssh.Request
	err      error
}

func (e *Engine) openChannel(ctx context.Context, client *ssh.Client, c *connection) (ssh.Channel, <-chan *ssh.Request, error) {
	result := make(chan openedChannel)
	go func() {
		ch, requests, err := client.OpenChannel("session", nil)
		select {
		case result <- openedChannel{ch, requests, err}:
		case <-ctx.Done():
			if ch != nil {
				ch.Close()
			}
		}
	}()
	select {
	case r := <-result:
		return r.channel, r.requests, r.err
	case <-ctx.Done():
		e.invalidate(c, client)
		return nil, nil, ctx.Err()
	}
}
func (e *Engine) invalidate(c *connection, client *ssh.Client) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if c.client == client {
		e.dropLocked(c, "execution_io")
	} else {
		client.Close()
	}
}

func (e *Engine) runExecution(job *execution) {
	prepare, cancel := context.WithDeadline(job.ctx, job.view.PrepareDeadlineAt)
	defer cancel()
	if err := e.opts.Validate(prepare, job.view.Target); err != nil {
		e.finishExecution(job, "failed", "identity", asFault(err, "IDENTITY_CHANGED", "无法确认原登录身份"))
		return
	}
	client, generation, err := e.transport(prepare, job.connection)
	if err != nil {
		e.finishExecution(job, "failed", "prepare", asFault(err, "CONNECT_FAILED", "无法恢复连接"))
		return
	}
	if job.input.TimeoutStrategy == "remote_required" {
		e.finishExecution(job, "failed", "capability", fault("REMOTE_DEADLINE_UNAVAILABLE", "当前代次尚未验证远端时限能力"))
		return
	}
	channelCtx, channelCancel := context.WithTimeout(prepare, e.opts.Limits.ChannelTimeout)
	defer channelCancel()
	ch, requests, err := e.openChannel(channelCtx, client, job.connection)
	if err != nil {
		e.finishExecution(job, "failed", "channel", fault("CHANNEL_FAILED", "无法打开执行通道"))
		return
	}
	defer ch.Close()
	e.mu.Lock()
	job.channel = ch
	job.client = client
	job.view.TransportGeneration = generation
	if job.ctx.Err() != nil {
		e.mu.Unlock()
		e.finishExecution(job, "cancelled", "before_send", nil)
		return
	}
	job.view.DispatchState = "possibly_sent"
	job.view.RemoteState = "unknown"
	e.mu.Unlock()
	command := localCommand(job.input, job.marker)
	ack := make(chan struct {
		ok  bool
		err error
	}, 1)
	go func() {
		ok, err := ch.SendRequest("exec", true, ssh.Marshal(struct{ Command string }{command}))
		ack <- struct {
			ok  bool
			err error
		}{ok, err}
	}()
	select {
	case r := <-ack:
		if r.err != nil {
			e.invalidate(job.connection, client)
			e.finishExecution(job, "unknown", "exec_ack", fault("EXEC_ACK_LOST", "执行应答丢失，不能重放"))
			return
		}
		if !r.ok {
			e.mu.Lock()
			job.view.RemoteState = "not_started"
			e.mu.Unlock()
			e.finishExecution(job, "failed", "exec_rejected", fault("EXEC_REJECTED", "服务端拒绝 exec"))
			return
		}
	case <-channelCtx.Done():
		e.invalidate(job.connection, client)
		e.finishExecution(job, "unknown", "exec_ack", fault("EXEC_ACK_LOST", "执行应答超时，不能重放"))
		return
	}
	e.mu.Lock()
	now := e.opts.Now()
	job.view.DispatchState = "acknowledged"
	job.view.Status = "running"
	job.view.RemoteState = "running"
	job.view.StartedAt = &now
	job.connection.view.ExecCapability = "supported"
	e.notifyExecution(job)
	e.mu.Unlock()
	_ = ch.CloseWrite()
	e.collectExecution(job, ch, requests)
}

func localCommand(in ExecuteInput, marker string) string {
	keys := make([]string, 0, len(in.Env))
	for k := range in.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	env := "env"
	for _, k := range keys {
		env += " " + shellQuote(k+"="+in.Env[k])
	}
	script := "cd " + shellQuote(in.Cwd) + " || { printf '%s' " + shellQuote("\x1e"+marker+":context_failed\x1f") + " >&2; exit 125; }; exec sh -c " + shellQuote(in.Command)
	return env + " sh -c " + shellQuote(script)
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func (e *Engine) collectExecution(job *execution, ch ssh.Channel, requests <-chan *ssh.Request) {
	stdout, stderr := make(chan struct{}), make(chan struct{})
	go func() { _, _ = io.Copy(executionWriter{e, job, "stdout"}, ch); close(stdout) }()
	go func() {
		writer := &controlWriter{engine: e, job: job}
		_, _ = io.Copy(writer, ch.Stderr())
		writer.flush()
		close(stderr)
	}()
	deadline := time.NewTimer(time.Duration(job.view.TimeoutMS) * time.Millisecond)
	defer deadline.Stop()
	var drain *time.Timer
	var drainC <-chan time.Time
	exited := false
	for requests != nil || stdout != nil || stderr != nil {
		select {
		case req, ok := <-requests:
			if !ok {
				requests = nil
				continue
			}
			_ = req.Reply(false, nil)
			e.mu.Lock()
			if req.Type == "exit-status" {
				var status struct{ Code uint32 }
				if ssh.Unmarshal(req.Payload, &status) == nil {
					code := int(status.Code)
					job.view.ExitCode = &code
					exited = true
				}
			}
			if req.Type == "exit-signal" {
				var signal struct {
					Signal            string
					Core              bool
					Message, Language string
				}
				if ssh.Unmarshal(req.Payload, &signal) == nil {
					job.view.ExitSignal = signal.Signal
					exited = true
				}
			}
			e.mu.Unlock()
			if exited && drain == nil {
				drain = time.NewTimer(5 * time.Second)
				defer drain.Stop()
				drainC = drain.C
			}
		case <-stdout:
			stdout = nil
		case <-stderr:
			stderr = nil
		case <-drainC:
			e.mu.Lock()
			job.incomplete = true
			e.mu.Unlock()
			ch.Close()
			requests = nil
			stdout = nil
			stderr = nil
		case <-deadline.C:
			ch.Close()
			e.invalidate(job.connection, job.client)
			if !exited {
				e.finishExecution(job, "unknown", "local_deadline", nil)
				return
			}
			e.mu.Lock()
			job.incomplete = true
			e.mu.Unlock()
			requests = nil
			stdout = nil
			stderr = nil
		case <-job.ctx.Done():
			ch.Close()
			e.invalidate(job.connection, job.client)
			if !exited {
				e.finishExecution(job, "unknown", "tracking_stopped", nil)
				return
			}
			e.mu.Lock()
			job.incomplete = true
			e.mu.Unlock()
			requests = nil
			stdout = nil
			stderr = nil
		}
	}
	if exited {
		e.mu.Lock()
		contextFailed := job.contextFailed
		if contextFailed {
			job.view.RemoteState = "not_started"
			job.view.ExitCode = nil
		}
		e.mu.Unlock()
		if contextFailed {
			e.finishExecution(job, "failed", "context", fault("EXECUTION_CONTEXT_FAILED", "无法切换到命令指定目录，原始命令未开始"))
		} else {
			e.finishExecution(job, "completed", "exit", nil)
		}
	} else {
		e.invalidate(job.connection, job.client)
		e.finishExecution(job, "unknown", "connection_lost", fault("EXECUTION_UNKNOWN", "缺少退出结果，远端可能继续运行"))
	}
}
