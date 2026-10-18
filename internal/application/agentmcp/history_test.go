package agentmcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestHistorySharesExecutionAndExpiresWithoutReplay(t *testing.T) {
	e, c := execFixture(t, func(ch ssh.Channel, _ string) {
		fmt.Fprint(ch, "private stdout")
		fmt.Fprint(ch.Stderr(), "private stderr")
		exitSSH(ch, 7)
	})
	in := executeInput(e, c, 2)
	v, err := e.Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	awaitExecution(t, e, v.ID)
	page := e.history.Read(0, 100)
	var command, completed bool
	for _, line := range page.Lines {
		if strings.Contains(line.Text, "private stdout") || strings.Contains(line.Text, "private stderr") {
			t.Fatal("输出进入操作日志")
		}
		command = command || strings.Contains(line.Text, in.Command)
		if strings.Contains(line.Text, "exit=7") {
			completed = line.Severity == "INFO"
		}
	}
	if !command || !completed {
		t.Fatalf("缺少命令输入或非零退出状态: %+v", page)
	}
	e.mu.Lock()
	job := e.executions[v.ID]
	shared := e.history.records[v.ID].job == job && job.input.Command == in.Command
	runtimeRetained := job.client != nil || job.channel != nil || job.connection != nil
	e.history.limit = 3
	e.mu.Unlock()
	if !shared {
		t.Fatal("执行输入未由同一操作记录保留")
	}
	if runtimeRetained {
		t.Fatal("终结记录仍持有 SSH 运行资源")
	}
	cursor := page.NextCursor
	replayed, err := e.Execute(context.Background(), in)
	if err != nil || replayed.ID != v.ID {
		t.Fatal("重复请求没有复用原任务", err)
	}
	replayLogs := e.history.Read(cursor, 100).Lines
	if len(replayLogs) != 1 || !strings.Contains(replayLogs[0].Text, "execute_command") || !strings.Contains(replayLogs[0].Text, "重复请求") {
		t.Fatalf("重复执行调用缺少日志: %+v", replayLogs)
	}
	e.mu.Lock()
	if e.history.records[v.ID].job != job || len(e.history.records[v.ID].events) != 3 {
		t.Error("重试复制了原任务或把无界调用事件附加到原任务")
	}
	e.mu.Unlock()
	for range 4 {
		e.RecordCall("list_connections", nil)
	}
	_, err = e.Execute(context.Background(), in)
	assertFault(t, err, "EXECUTION_EXPIRED")
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.history.records) > 3 || job.output.Len() != 0 || job.input.Command != "" {
		t.Fatal("回收未释放唯一数据载荷")
	}
}

func TestCallLogPreservesQueryErrorCode(t *testing.T) {
	e := NewEngine(EngineOptions{Instance: "test"})
	t.Cleanup(e.cancel)
	e.RecordCall("list_assets", &QueryError{Code: "AUTH_REQUIRED", Message: "不应显示的上游原文"})
	lines := e.history.Read(0, 10).Lines
	if len(lines) != 1 || lines[0].Detail != 1 || !strings.Contains(lines[0].Text, "code=AUTH_REQUIRED") || strings.Contains(lines[0].Text, "上游原文") {
		t.Fatalf("错误日志不正确: %+v", lines)
	}
}

func TestCallSummaryAndConditionsShareOneBoundedRecord(t *testing.T) {
	h := NewHistory(1)
	e := NewEngine(EngineOptions{Instance: "test", History: h})
	t.Cleanup(e.cancel)
	e.RecordCall("list_assets", nil, CallDetails{Summary: "count=2", Conditions: `profile="work"  search="test"`})
	page := h.Read(0, 100)
	if len(h.records) != 1 || len(page.Lines) != 2 || page.Lines[0].Detail != 2 || page.Lines[1].Detail != 3 {
		t.Fatalf("查询摘要和条件没有共享一个记录: %+v", page)
	}
	if len(h.records["log:1"].events) != 2 || h.outputBytes != 0 || h.inputBytes != 0 {
		t.Fatal("查询日志另存了任务载荷")
	}
	e.RecordCall("get_server_info", nil)
	if got := h.Read(0, 100); len(got.Lines) != 1 || strings.Contains(got.Lines[0].Text, "list_assets") {
		t.Fatalf("查询回收没有同时移除摘要和条件: %+v", got)
	}
	e.RecordCall("list_assets", nil, CallDetails{Conditions: strings.Repeat("条", 10000) + "\x1b[2J"})
	for _, line := range h.Read(0, 100).Lines {
		if len(line.Text) > 4200 || strings.Contains(line.Text, "\x1b") {
			t.Fatal("调用日志未限制大小或未隔离控制字符")
		}
	}
}

func TestHistoryCursorGapAndSafeANSI(t *testing.T) {
	h := NewHistory(2)
	h.Record("one", "service_started", "INFO", 1, "first")
	first := h.Read(0, 1)
	h.Record("one", "service_stopped", "INFO", 1, "second")
	h.Record("two", "service_started", "INFO", 1, "third\x1b[2J\rspoof")
	h.Record("two", "call", "INFO", 1, "fourth")
	page := h.Read(first.NextCursor, 1)
	if !page.Gap || !page.HasMore || len(page.Lines) != 1 {
		t.Fatalf("游标缺口未声明: %+v", page)
	}
	plain := FormatLogLine(page.Lines[0], false)
	colored := FormatLogLine(page.Lines[0], true)
	if strings.Contains(plain, "\x1b") || strings.Contains(plain, "\r") || !strings.Contains(colored, "\x1b[32m") {
		t.Fatal("终端控制字符未隔离或 ANSI 缺失")
	}
	last := h.Read(page.NextCursor, 100)
	if last.HasMore || len(last.Lines) != 1 {
		t.Fatalf("增量读取不正确: %+v", last)
	}
}

func TestHistoryProtectsActiveOperations(t *testing.T) {
	gate := make(chan struct{})
	e, c := execFixture(t, func(ch ssh.Channel, _ string) { <-gate; exitSSH(ch, 0) })
	defer close(gate)
	v, err := e.Execute(context.Background(), executeInput(e, c, 2))
	if err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	e.history.limit = 1
	e.mu.Unlock()
	for range 5 {
		e.RecordCall("list_connections", nil)
	}
	if _, err := e.GetExecution(context.Background(), GetExecutionInput{ExecutionID: v.ID}); err != nil {
		t.Fatalf("活动任务被日志挤出: %v", err)
	}
}

func TestHistorySharesOutputBudgetAcrossRetiredInstances(t *testing.T) {
	h := NewHistory(3)
	makeJob := func(instance string) (*Engine, *execution) {
		e := NewEngine(EngineOptions{Instance: instance, History: h, Limits: Limits{TotalOutputBytes: 6}})
		t.Cleanup(e.cancel)
		job := &execution{view: ExecutionView{ID: instance + ":x:1", Status: "running"}, input: ExecuteInput{Command: "echo example"}, changed: make(chan struct{})}
		h.mu.Lock()
		r := h.addLocked(job.view.ID, true, e)
		r.job = job
		e.executions[job.view.ID] = job
		h.mu.Unlock()
		return e, job
	}
	first, old := makeJob("old")
	first.appendOutput(old, "stdout", []byte("old"))
	h.mu.Lock()
	old.view.Status = "completed"
	h.records[old.view.ID].active = false
	h.mu.Unlock()
	first.Retire()
	second, current := makeJob("current")
	second.appendOutput(current, "stderr", []byte("next"))
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.records[old.view.ID].owner != nil {
		t.Fatal("重启后的历史仍持有完整旧引擎和去重索引")
	}
	if old.output.Len() != 0 || old.droppedBytes != 3 || current.outputBytes != 4 || h.outputBytes != 4 {
		t.Fatal("输出全局额度未跨实例共享")
	}
	if old.input.Command != "echo example" {
		t.Fatal("释放旧宿主引用不应删除历史输入")
	}
}

func TestRetiredEngineEvictionRemovesItsIndexes(t *testing.T) {
	e := NewEngine(EngineOptions{Instance: "retired"})
	t.Cleanup(e.cancel)
	job := &execution{view: ExecutionView{ID: "retired:x:1", Status: "completed"}}
	e.mu.Lock()
	r := e.history.addLocked(job.view.ID, false, e)
	r.job = job
	e.executions[job.view.ID] = job
	e.executionHistory = append(e.executionHistory, job)
	e.mu.Unlock()
	e.Retire()
	e.mu.Lock()
	e.evictExecution(job)
	stale := len(e.executionHistory) > 0 || len(e.executions) > 0
	e.mu.Unlock()
	if stale {
		t.Fatal("退休后在途清理仍须移除旧引擎索引，防止回收循环不前进")
	}
}
