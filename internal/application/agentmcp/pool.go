package agentmcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type Target struct {
	Profile      string `json:"profile"`
	Organization string `json:"organization,omitempty"`
	AssetID      string `json:"asset_id,omitempty"`
	AccountID    string `json:"account_id,omitempty"`
	Alias        string `json:"alias,omitempty"`
}

type ResolvedTarget struct {
	Target
	Site               string `json:"site"`
	CredentialIdentity string `json:"credential_identity"`
}

type Limits struct {
	Connections, Transports, Closed, Executions, Requests, ActiveRequests int
	OutputBytes, TotalOutputBytes                                         int
	IdleTimeout, HistoryTTL, ConnectTimeout, ChannelTimeout, Cooldown     time.Duration
}

func (l Limits) defaults() Limits {
	if l.Connections <= 0 {
		l.Connections = 64
	}
	if l.Transports <= 0 {
		l.Transports = 16
	}
	if l.Closed <= 0 {
		l.Closed = 256
	}
	if l.Executions <= 0 {
		l.Executions = 256
	}
	if l.Requests <= 0 {
		l.Requests = 10000
	}
	if l.ActiveRequests <= 0 {
		l.ActiveRequests = 128
	}
	if l.OutputBytes <= 0 {
		l.OutputBytes = 8 << 20
	}
	if l.TotalOutputBytes <= 0 {
		l.TotalOutputBytes = 64 << 20
	}
	if l.IdleTimeout <= 0 {
		l.IdleTimeout = 10 * time.Minute
	}
	if l.HistoryTTL <= 0 {
		l.HistoryTTL = 30 * time.Minute
	}
	if l.ConnectTimeout <= 0 {
		l.ConnectTimeout = 30 * time.Second
	}
	if l.ChannelTimeout <= 0 {
		l.ChannelTimeout = 10 * time.Second
	}
	if l.Cooldown <= 0 {
		l.Cooldown = 3 * time.Second
	}
	return l
}

type EngineOptions struct {
	Instance            string
	Resolve             func(context.Context, Target) (ResolvedTarget, error)
	Validate            func(context.Context, ResolvedTarget) error
	Dial                func(context.Context, ResolvedTarget) (*ssh.Client, error)
	Limits              Limits
	Now                 func() time.Time
	MaintenanceInterval time.Duration
}

// Engine 归属于一次服务实例；HTTP 请求结束不取消它拥有的工作。
type Engine struct {
	mu          sync.Mutex
	opts        EngineOptions
	ctx         context.Context
	cancel      context.CancelFunc
	requests    *requests
	sequence    uint64
	closing     bool
	connections map[string]*connection
	closed      []*connection
	opens       map[string]*openOperation
	openHistory []*openOperation
}

type ConnectionView struct {
	ID                       string         `json:"connection_id"`
	Target                   ResolvedTarget `json:"target"`
	Status                   string         `json:"status"`
	TransportGeneration      uint64         `json:"transport_generation"`
	TransportConnected       bool           `json:"transport_connected"`
	CreatedAt                time.Time      `json:"created_at"`
	LastUsedAt               time.Time      `json:"last_used_at"`
	ActiveExecutionID        string         `json:"active_execution_id,omitempty"`
	ClosedAt                 *time.Time     `json:"closed_at,omitempty"`
	CloseReason              string         `json:"close_reason,omitempty"`
	LastError                *Fault         `json:"last_error,omitempty"`
	ExecCapability           string         `json:"exec_capability"`
	RemoteDeadlineCapability string         `json:"remote_deadline_capability"`
}

type connection struct {
	view     ConnectionView
	client   *ssh.Client
	dial     *dialAttempt
	refs     int
	ping     bool
	lastPing time.Time
	closing  bool
}
type dialAttempt struct {
	done   chan struct{}
	cancel context.CancelFunc
	err    error
}
type openOperation struct {
	id           string
	record       *requestRecord
	connectionID string
	reused       bool
	ended        time.Time
}

type OpenInput struct {
	Target     Target `json:"target"`
	RequestKey string `json:"request_key"`
	Reuse      *bool  `json:"reuse,omitempty"`
}
type OpenResult struct {
	Status     string          `json:"status"`
	Connection *ConnectionView `json:"connection,omitempty"`
	Reused     bool            `json:"reused"`
	Error      *Fault          `json:"error,omitempty"`
}
type ConnectionInput struct {
	ConnectionID string `json:"connection_id"`
}
type CloseInput struct {
	ConnectionID string `json:"connection_id"`
	Mode         string `json:"mode,omitempty"`
}

func NewEngine(opts EngineOptions) *Engine {
	opts.Limits = opts.Limits.defaults()
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.MaintenanceInterval <= 0 {
		opts.MaintenanceInterval = time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{opts: opts, ctx: ctx, cancel: cancel, requests: newRequests(opts.Instance, opts.Limits.Requests, opts.Limits.ActiveRequests), connections: make(map[string]*connection), opens: make(map[string]*openOperation)}
	go func() {
		timer := time.NewTicker(opts.MaintenanceInterval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				e.maintain()
			}
		}
	}()
	return e
}
func (e *Engine) RequestEpoch() uint64 { return e.requests.epoch() }
func (e *Engine) nextID(kind string) string {
	e.sequence++
	return fmt.Sprintf("%s:%s:%d", e.opts.Instance, kind, e.sequence)
}
func (e *Engine) checkID(id string) error {
	parts := strings.Split(id, ":")
	if len(parts) != 3 {
		return fault("INVALID_ARGUMENT", "业务 ID 格式无效")
	}
	if parts[0] != e.opts.Instance {
		return fault("INSTANCE_CHANGED", "服务实例已改变，旧 ID 无效")
	}
	return nil
}

func (e *Engine) Open(ctx context.Context, in OpenInput) (OpenResult, error) {
	if err := ctx.Err(); err != nil {
		return OpenResult{}, err
	}
	reuse := in.Reuse == nil || *in.Reuse
	e.mu.Lock()
	record, fresh, err := e.requests.begin("open", in.RequestKey, struct {
		Target Target
		Reuse  bool
	}{in.Target, reuse}, func() (string, error) {
		if e.closing {
			return "", fault("SERVICE_DRAINING", "服务正在停止")
		}
		return e.nextID("open"), nil
	})
	if err != nil {
		e.mu.Unlock()
		return OpenResult{}, err
	}
	op := e.opens[record.ResourceID]
	if fresh {
		op = &openOperation{id: record.ResourceID, record: record}
		e.opens[op.id] = op
		go e.open(op, in.Target, reuse)
	}
	e.mu.Unlock()
	if op == nil {
		return OpenResult{}, fault("CONNECTION_EXPIRED", "打开操作的详细记录已回收，不会重复连接")
	}
	// 打开过程可超过一次 HTTP 等待窗口，同键重试查询原操作。
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-record.done:
	case <-ctx.Done():
	case <-timer.C:
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	result := OpenResult{Status: "opening", Reused: op.reused}
	select {
	case <-record.done:
		result.Status = "completed"
		result.Error = record.Failure
		if record.Failure != nil {
			result.Status = "failed"
		}
	default:
	}
	if c := e.connections[op.connectionID]; c != nil {
		v := e.view(c)
		result.Connection = &v
	} else if op.connectionID != "" {
		result.Error = fault("CONNECTION_EXPIRED", "逻辑连接历史已回收")
	}
	return result, nil
}

func (e *Engine) open(op *openOperation, input Target, reuse bool) {
	ctx, cancel := context.WithTimeout(e.ctx, e.opts.Limits.ConnectTimeout)
	defer cancel()
	target, err := e.opts.Resolve(ctx, input)
	var c *connection
	if err == nil {
		e.mu.Lock()
		if e.closing {
			err = fault("SERVICE_DRAINING", "服务正在停止")
		} else {
			if reuse {
				for _, candidate := range e.connections {
					if candidate.view.Target == target && !candidate.closing && candidate.view.Status != "closed" {
						c = candidate
						op.reused = true
						break
					}
				}
			}
			if c == nil {
				if e.liveCount() >= e.opts.Limits.Connections {
					if old := e.idleLRU(nil, false); old != nil {
						e.closeLocked(old, "capacity")
					} else {
						err = fault("LIMIT_EXCEEDED", "逻辑连接已满且均忙碌")
					}
				}
				if err == nil {
					now := e.opts.Now()
					c = &connection{view: ConnectionView{ID: e.nextID("c"), Target: target, Status: "disconnected", CreatedAt: now, LastUsedAt: now, ExecCapability: "unknown", RemoteDeadlineCapability: "unknown"}}
					e.connections[c.view.ID] = c
				}
			}
			if c != nil {
				op.connectionID = c.view.ID
				c.refs++
				c.view.LastUsedAt = e.opts.Now()
			}
		}
		e.mu.Unlock()
		if err == nil {
			_, _, err = e.transport(ctx, c)
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if c != nil {
		c.refs--
	}
	op.ended = e.opts.Now()
	e.requests.finish(op.record, asFault(err, "CONNECT_FAILED", "无法建立连接，请检查身份、权限、网络和主机信任"))
	e.openHistory = append(e.openHistory, op)
	e.pruneLocked()
}

// transport 的拨号由连接拥有；一个请求的取消不取消其他等待者。
func (e *Engine) transport(ctx context.Context, c *connection) (*ssh.Client, uint64, error) {
	e.mu.Lock()
	if e.closing || c.closing || c.view.Status == "closed" {
		e.mu.Unlock()
		return nil, 0, fault("CONNECTION_CLOSED", "连接已关闭或正在关闭")
	}
	if c.client != nil {
		client, generation := c.client, c.view.TransportGeneration
		e.mu.Unlock()
		return client, generation, nil
	}
	dial := c.dial
	if dial == nil {
		if e.transportCount() >= e.opts.Limits.Transports {
			if old := e.idleLRU(c, true); old != nil {
				e.dropLocked(old, "capacity")
			} else {
				e.mu.Unlock()
				return nil, 0, fault("LIMIT_EXCEEDED", "SSH 连接已满且均忙碌")
			}
		}
		dialCtx, cancel := context.WithTimeout(e.ctx, e.opts.Limits.ConnectTimeout)
		dial = &dialAttempt{done: make(chan struct{}), cancel: cancel}
		c.dial = dial
		c.view.Status = "connecting"
		go e.dial(dialCtx, c, dial)
	}
	e.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case <-dial.done:
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if dial.err != nil {
		return nil, 0, dial.err
	}
	if c.client == nil || c.closing {
		return nil, 0, fault("CONNECTION_CLOSED", "连接已关闭")
	}
	return c.client, c.view.TransportGeneration, nil
}

func (e *Engine) dial(ctx context.Context, c *connection, dial *dialAttempt) {
	defer dial.cancel()
	err := e.opts.Validate(ctx, c.view.Target)
	var client *ssh.Client
	if err == nil {
		client, err = e.opts.Dial(ctx, c.view.Target)
	}
	if err == nil {
		err = e.opts.Validate(ctx, c.view.Target)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err == nil && (ctx.Err() != nil || e.closing || c.closing || c.view.Status == "closed" || c.dial != dial) {
		err = fault("CONNECTION_CLOSED", "建连期间连接已关闭")
	}
	if err != nil {
		if client != nil {
			client.Close()
		}
		dial.err = err
		if c.view.Status != "closed" {
			c.view.Status = "disconnected"
			c.view.LastError = asFault(err, "CONNECT_FAILED", "SSH 建连失败")
		}
	} else {
		c.client = client
		c.view.TransportGeneration++
		c.view.Status = "ready"
		c.view.LastError = nil
		c.view.ExecCapability = "unknown"
		c.view.RemoteDeadlineCapability = "unknown"
		c.lastPing = e.opts.Now()
		go func() {
			_ = client.Wait()
			e.mu.Lock()
			defer e.mu.Unlock()
			if c.client == client {
				e.dropLocked(c, "disconnected")
			}
		}()
	}
	if c.dial == dial {
		c.dial = nil
	}
	close(dial.done)
}

func (e *Engine) GetConnection(_ context.Context, in ConnectionInput) (ConnectionView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := e.findConnection(in.ConnectionID)
	if err != nil {
		return ConnectionView{}, err
	}
	return e.view(c), nil
}
func (e *Engine) ListConnections(_ context.Context, _ struct{}) ([]ConnectionView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]ConnectionView, 0, len(e.connections))
	for _, c := range e.connections {
		out = append(out, e.view(c))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (e *Engine) findConnection(id string) (*connection, error) {
	if err := e.checkID(id); err != nil {
		return nil, err
	}
	c := e.connections[id]
	if c == nil {
		return nil, fault("CONNECTION_NOT_FOUND", "连接不存在或历史已回收")
	}
	return c, nil
}
func (e *Engine) view(c *connection) ConnectionView {
	v := c.view
	v.TransportConnected = c.client != nil
	return v
}
func (e *Engine) CloseConnection(_ context.Context, in CloseInput) (ConnectionView, error) {
	if in.Mode != "" && in.Mode != "if_idle" {
		return ConnectionView{}, fault("INVALID_ARGUMENT", "当前关闭模式无效")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := e.findConnection(in.ConnectionID)
	if err != nil {
		return ConnectionView{}, err
	}
	if c.view.Status == "closed" {
		return e.view(c), nil
	}
	if !idle(c) {
		return ConnectionView{}, fault("CONNECTION_BUSY", "连接正在使用，不能按空闲方式关闭")
	}
	e.closeLocked(c, "explicit")
	return e.view(c), nil
}
func idle(c *connection) bool {
	return c.view.Status != "closed" && !c.closing && c.refs == 0 && c.dial == nil && !c.ping && c.view.ActiveExecutionID == ""
}
func (e *Engine) idleLRU(exclude *connection, transportOnly bool) *connection {
	var best *connection
	for _, c := range e.connections {
		if c == exclude || !idle(c) || (transportOnly && c.client == nil) {
			continue
		}
		if best == nil || c.view.LastUsedAt.Before(best.view.LastUsedAt) {
			best = c
		}
	}
	return best
}
func (e *Engine) liveCount() int {
	n := 0
	for _, c := range e.connections {
		if c.view.Status != "closed" {
			n++
		}
	}
	return n
}
func (e *Engine) transportCount() int {
	n := 0
	for _, c := range e.connections {
		if c.client != nil || c.dial != nil {
			n++
		}
	}
	return n
}
func (e *Engine) dropLocked(c *connection, reason string) {
	c.ping = false
	if c.client != nil {
		c.client.Close()
		c.client = nil
	}
	if c.view.Status != "closed" {
		c.view.Status = "disconnected"
	}
	c.view.ExecCapability = "unknown"
	c.view.RemoteDeadlineCapability = "unknown"
}
func (e *Engine) closeLocked(c *connection, reason string) {
	if c.view.Status == "closed" {
		return
	}
	c.closing = true
	if c.dial != nil {
		c.dial.cancel()
	}
	e.dropLocked(c, reason)
	now := e.opts.Now()
	c.view.Status = "closed"
	c.view.ClosedAt = &now
	c.view.CloseReason = reason
	e.closed = append(e.closed, c)
	e.pruneLocked()
}
func (e *Engine) pruneLocked() {
	now := e.opts.Now()
	for len(e.closed) > 0 && (len(e.closed) > e.opts.Limits.Closed || now.Sub(*e.closed[0].view.ClosedAt) >= e.opts.Limits.HistoryTTL) {
		c := e.closed[0]
		delete(e.connections, c.view.ID)
		e.closed[0] = nil
		e.closed = e.closed[1:]
	}
	for len(e.openHistory) > 0 && (len(e.openHistory) > e.opts.Limits.Requests || now.Sub(e.openHistory[0].ended) >= e.opts.Limits.HistoryTTL) {
		op := e.openHistory[0]
		delete(e.opens, op.id)
		e.openHistory[0] = nil
		e.openHistory = e.openHistory[1:]
	}
}
func (e *Engine) maintain() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pruneLocked()
	if e.closing {
		return
	}
	now := e.opts.Now()
	for _, c := range e.connections {
		if c.client == nil {
			continue
		}
		if idle(c) && now.Sub(c.view.LastUsedAt) >= e.opts.Limits.IdleTimeout {
			e.dropLocked(c, "idle")
			continue
		}
		if !c.ping && now.Sub(c.lastPing) >= 30*time.Second {
			c.ping = true
			c.lastPing = now
			go e.keepalive(c, c.client)
		}
	}
}
func (e *Engine) keepalive(c *connection, client *ssh.Client) {
	done := make(chan error, 1)
	go func() { _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); done <- err }()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	var err error
	select {
	case err = <-done:
	case <-timer.C:
		err = context.DeadlineExceeded
	case <-e.ctx.Done():
		err = e.ctx.Err()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if c.client == client {
		c.ping = false
		if err != nil {
			e.dropLocked(c, "keepalive")
		}
	}
}
func (e *Engine) Shutdown(_ context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closing = true
	e.cancel()
	for _, c := range e.connections {
		e.closeLocked(c, "service_stop")
	}
	return nil
}
func asFault(err error, code, message string) *Fault {
	if err == nil {
		return nil
	}
	var f *Fault
	if errors.As(err, &f) {
		return f
	}
	var q *QueryError
	if errors.As(err, &q) {
		return &Fault{Code: q.Code, Message: q.Message, Retryable: q.Retryable}
	}
	return fault(code, message)
}
