package agentmcp

import (
	"container/list"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// History 是宿主内共享的操作仓库。Engine 索引和输出链表只引用这里的对象；
// 所有实例共用同一把锁，回收时无需持有两个 Engine 锁或调用外部订阅者。
type History struct {
	mu                        sync.Mutex
	limit                     int
	records                   map[string]*operationRecord
	fifo, events, outputQueue list.List
	sequence, evictedThrough  uint64
	inputBytes, outputBytes   int
	downloadBytes             int
}

type operationRecord struct {
	id            string
	active        bool
	owner         *Engine
	job           *execution
	open          *openOperation
	download      *download
	node          *list.Element
	events        []*list.Element
	inputBytes    int
	downloadBytes int
}

type LogLine struct {
	Sequence     uint64    `json:"sequence"`
	Time         time.Time `json:"time"`
	Instance     string    `json:"instance"`
	Severity     string    `json:"severity"`
	Detail       int       `json:"detail"`
	ConnectionID string    `json:"connectionID,omitempty"`
	ExecutionID  string    `json:"executionID,omitempty"`
	Event        string    `json:"event"`
	Text         string    `json:"text"`
}

type logEvent struct {
	LogLine
	record  *operationRecord
	command bool
}
type LogPage struct {
	Lines      []LogLine `json:"lines"`
	NextCursor uint64    `json:"nextCursor"`
	HasMore    bool      `json:"hasMore"`
	Gap        bool      `json:"gap"`
}

func NewHistory(limit int) *History {
	if limit <= 0 {
		limit = 3000
	}
	return &History{limit: limit, records: make(map[string]*operationRecord)}
}

func (h *History) addLocked(id string, active bool, owner *Engine) *operationRecord {
	for len(h.records) >= h.limit {
		var victim *operationRecord
		for n := h.fifo.Front(); n != nil; n = n.Next() {
			if r := n.Value.(*operationRecord); !r.active {
				victim = r
				break
			}
		}
		if victim == nil {
			return nil
		}
		h.removeLocked(victim)
	}
	r := &operationRecord{id: id, active: active, owner: owner}
	r.node = h.fifo.PushBack(r)
	h.records[id] = r
	return r
}

func (h *History) removeLocked(r *operationRecord) {
	if h.records[r.id] != r || r.active {
		return
	}
	for _, n := range r.events {
		h.evictedThrough = max(h.evictedThrough, n.Value.(*logEvent).Sequence)
		h.events.Remove(n)
	}
	if r.job != nil {
		for r.job.output.Len() > 0 {
			h.removeOutputLocked(r.job.output.Front().Value.(*outputBlock), false)
		}
		r.job.input = ExecuteInput{}
		if r.owner != nil {
			delete(r.owner.executions, r.id)
			r.owner.executionHistory = slices.DeleteFunc(r.owner.executionHistory, func(job *execution) bool { return job == r.job })
		}
	}
	if r.open != nil && r.owner != nil {
		delete(r.owner.opens, r.id)
		r.owner.openHistory = slices.DeleteFunc(r.owner.openHistory, func(op *openOperation) bool { return op == r.open })
	}
	if r.download != nil && r.owner != nil {
		delete(r.owner.downloads, r.id)
	}
	h.inputBytes -= r.inputBytes
	h.downloadBytes -= r.downloadBytes
	h.fifo.Remove(r.node)
	delete(h.records, r.id)
	r.events = nil
}

// Retire 在宿主停止实例后解除仓库到旧 Engine 的引用。历史只保留单条操作，
// 不因一条日志而保留整个旧实例的索引、连接池和上下文；在途清理仍持有自己的 Engine。
func (e *Engine) Retire() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range e.history.records {
		if r.owner == e {
			r.owner = nil
		}
	}
}

func (h *History) eventLocked(r *operationRecord, line LogLine, command bool) {
	h.sequence++
	if r == nil {
		h.evictedThrough = h.sequence
		return
	}
	line.Sequence = h.sequence
	if line.Time.IsZero() {
		line.Time = time.Now().UTC()
	}
	line.Text = safeLogText(line.Text)
	n := h.events.PushBack(&logEvent{LogLine: line, record: r, command: command})
	r.events = append(r.events, n)
}

func (h *History) Record(instance, event, severity string, detail int, text string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recordLocked(instance, event, severity, detail, "", text)
}

func (h *History) recordLocked(instance, event, severity string, detail int, connection, text string) {
	id := fmt.Sprintf("log:%d", h.sequence+1)
	r := h.addLocked(id, false, nil)
	h.eventLocked(r, LogLine{Instance: instance, Event: event, Severity: severity, Detail: detail, ConnectionID: connection, Text: text}, false)
}

// Read 是 GUI 和未来 CLI 共用的增量接口；不返回 stdout、stderr 或环境变量。
// 游标跨服务重启连续，宿主进程结束后失效；掉队显式返回 Gap。
func (h *History) Read(cursor uint64, limit int) LogPage {
	h.mu.Lock()
	defer h.mu.Unlock()
	limit = min(max(limit, 1), 500)
	page := LogPage{Lines: []LogLine{}, NextCursor: cursor, Gap: cursor > 0 && (cursor < h.evictedThrough || cursor > h.sequence)}
	if cursor > h.sequence {
		cursor = 0
		page.NextCursor = 0
	}
	size := 0
	for n := h.events.Front(); n != nil; n = n.Next() {
		e := n.Value.(*logEvent)
		if e.Sequence <= cursor {
			continue
		}
		if len(page.Lines) >= limit || size >= 512<<10 {
			page.HasMore = true
			break
		}
		line := e.LogLine
		if e.command {
			if e.record.job.input.Command == "" {
				line.Text = "命令输入已回收"
			} else {
				line.Text = "command=" + strconv.Quote(e.record.job.input.Command) + "  cwd=" + strconv.Quote(e.record.job.input.Cwd)
			}
		}
		size += len(line.Text)
		page.Lines = append(page.Lines, line)
		page.NextCursor = line.Sequence
	}
	if !page.HasMore {
		page.NextCursor = h.sequence
	}
	return page
}

func safeLogText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
}

// FormatLogLine 的 ANSI 编号与 GUI 色板语义相同。外部终端决定实际颜色。
func FormatLogLine(line LogLine, ansi bool) string {
	level := safeLogText(line.Severity)
	if ansi {
		color := map[string]string{"DEBUG": "36", "INFO": "32", "WARN": "33", "ERROR": "31"}[line.Severity]
		if color != "" {
			level = "\x1b[" + color + "m" + level + "\x1b[0m"
		}
	}
	connection := ""
	if line.ConnectionID != "" {
		connection = " [" + safeLogText(line.ConnectionID) + "]"
	}
	return line.Time.Local().Format("15:04:05") + "  " + level + connection + "  " + safeLogText(line.Text)
}

// CallDetails 只接受调用摘要和经过白名单筛选的条件，不持有原始请求或响应。
type CallDetails struct {
	Summary, Conditions       string
	ConnectionID, ExecutionID string
	Duration                  time.Duration
}

func (e *Engine) RecordCall(name string, err error, details ...CallDetails) {
	e.mu.Lock()
	defer e.mu.Unlock()
	d := CallDetails{}
	if len(details) > 0 {
		d = details[0]
	}
	e.recordCallLocked(name, err, d)
}

func (e *Engine) recordCallLocked(name string, err error, d CallDetails) {
	name = boundedLogText(name, 128)
	severity, detail, message := "INFO", 2, name+"  调用完成"
	if err != nil {
		severity, detail = "ERROR", 1
		message = name + "  调用失败  code=" + asFault(err, "INTERNAL_ERROR", "操作未完成").Code
	} else if d.Summary != "" {
		message += "  " + boundedLogText(d.Summary, 1024)
	}
	if d.Duration > 0 {
		message += fmt.Sprintf("  duration=%dms", d.Duration.Milliseconds())
	}
	r := e.history.addLocked(fmt.Sprintf("log:%d", e.history.sequence+1), false, nil)
	line := LogLine{Instance: e.opts.Instance, Event: "tool_call", Severity: severity, Detail: detail, ConnectionID: boundedLogText(d.ConnectionID, 256), ExecutionID: boundedLogText(d.ExecutionID, 256), Text: message}
	e.history.eventLocked(r, line, false)
	if d.Conditions != "" {
		line.Event, line.Severity, line.Detail = "tool_conditions", "DEBUG", 3
		line.Text = name + "  查询条件  " + boundedLogText(d.Conditions, 4096)
		e.history.eventLocked(r, line, false)
	}
}

func boundedLogText(value string, limit int) string {
	if len(value) > limit {
		value = value[:limit]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
		value += "…"
	}
	return safeLogText(value)
}

func (e *Engine) eventLocked(event, severity string, detail int, connection, text string) {
	e.history.recordLocked(e.opts.Instance, event, severity, detail, connection, text)
}

func (e *Engine) jobEventLocked(job *execution, event, severity string, detail int, text string, command bool) {
	e.history.eventLocked(e.history.records[job.view.ID], LogLine{Time: e.opts.Now(), Instance: e.opts.Instance, Event: event, Severity: severity, Detail: detail, ConnectionID: job.view.ConnectionID, ExecutionID: job.view.ID, Text: text}, command)
}
