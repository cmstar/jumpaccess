package mcpserver_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/crypto/ssh"
)

func TestEveryOperationCallHasSafeLogsIncludingReplayAndNoop(t *testing.T) {
	_, settings, token := runtimeFixture(t)
	history := agentmcp.NewHistory(3000)
	dial := sharedOperationSSH(t, func(ch ssh.Channel, requests <-chan *ssh.Request) {
		for request := range requests {
			if request.Type != "exec" {
				_ = request.Reply(false, nil)
				continue
			}
			_ = request.Reply(true, nil)
			_, _ = fmt.Fprint(ch, "output-must-not-be-logged")
			_, _ = fmt.Fprint(ch.Stderr(), "stderr-must-not-be-logged")
			_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{7}))
			return
		}
	})
	r := agentmcp.NewRuntime(settings, "gui", func(info agentmcp.Instance, cfg config.MCP, token string) http.Handler {
		engine := agentmcp.NewEngine(agentmcp.EngineOptions{Instance: info.ID, History: history, Dial: dial, Validate: func(context.Context, agentmcp.ResolvedTarget) error { return nil }, Resolve: func(context.Context, agentmcp.Target) (agentmcp.ResolvedTarget, error) {
			return agentmcp.ResolvedTarget{Target: agentmcp.Target{Profile: "work", Organization: "org", AssetID: "asset", AccountID: "account"}}, nil
		}})
		return mcpserver.NewHandler(mcpserver.Options{Instance: info, Config: cfg, Token: token, Engine: engine})
	})
	status, err := r.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Stop(context.Background()) })
	session := sharedOperationClient(t, status.Instance.URL, token, "logging-test")
	var cursor uint64
	check := func(name string, input any, wantError bool) mcpserver.Result[map[string]any] {
		t.Helper()
		out := callOperation[map[string]any](t, session, name, input)
		if out.OK == wantError {
			t.Fatalf("%s 返回状态不符合预期", name)
		}
		page := history.Read(cursor, 100)
		cursor = page.NextCursor
		if len(page.Lines) == 0 {
			t.Fatalf("%s 未产生任何日志", name)
		}
		standard := false
		for _, line := range page.Lines {
			standard = standard || line.Detail <= 2
			if strings.Contains(line.Text, "must-not-be-logged") || strings.Contains(line.Text, token) {
				t.Fatalf("%s 日志包含载荷或凭据", name)
			}
		}
		if !standard {
			t.Fatalf("%s 没有标准级别摘要", name)
		}
		return out
	}
	check("get_server_info", map[string]any{}, false)
	open := agentmcp.OpenInput{Target: agentmcp.Target{Profile: "work"}, RequestKey: status.Instance.ID + ":1:00000000-0000-4000-8000-000000000001"}
	opened := check("open_connection", open, false)
	connection := (*opened.Data)["connection"].(map[string]any)["connection_id"].(string)
	check("open_connection", open, false)
	check("list_connections", map[string]any{}, false)
	check("get_connection", agentmcp.ConnectionInput{ConnectionID: connection}, false)
	wait := 5000
	execute := agentmcp.ExecuteInput{ConnectionID: connection, RequestKey: status.Instance.ID + ":1:00000000-0000-4000-8000-000000000002", Command: "fixture-command", Cwd: "/", TimeoutStrategy: "local", WaitMS: &wait, Env: map[string]string{"SECRET": "env-must-not-be-logged"}}
	result := check("execute_command", execute, false)
	if (*result.Data)["status"] != "completed" || (*result.Data)["exit_code"] != float64(7) {
		t.Fatal("合成执行应正常完成并返回非零退出码")
	}
	commandLogs := history.Read(0, 500).Lines
	accepted, inputLines, finished := 0, 0, 0
	for _, line := range commandLogs {
		switch line.Event {
		case "execution_accepted":
			accepted++
		case "command_input":
			inputLines++
		case "execution_finished":
			finished++
			if line.Severity != "INFO" {
				t.Fatal("非零退出码被误记为调用失败")
			}
		}
	}
	if accepted != 1 || inputLines != 1 || finished != 1 {
		t.Fatal("首次执行日志重复或缺失")
	}
	execution := (*result.Data)["execution_id"].(string)
	check("execute_command", execute, false)
	check("get_execution", agentmcp.GetExecutionInput{ExecutionID: execution}, false)
	check("list_executions", map[string]any{}, false)
	check("cancel_execution", agentmcp.CancelInput{ExecutionID: execution}, false)
	check("cancel_execution", agentmcp.CancelInput{ExecutionID: execution}, false)
	check("close_connection", agentmcp.CloseInput{ConnectionID: connection}, false)
	check("close_connection", agentmcp.CloseInput{ConnectionID: connection}, false)
	check("get_execution", agentmcp.GetExecutionInput{ExecutionID: "old:x:1"}, true)
	for _, tc := range []struct {
		name  string
		input any
	}{
		{"execute_command", map[string]any{"command": "invalid-must-not-be-logged"}},
		{"get_execution", map[string]any{"execution_id": 123}},
		{"missing_tool", map[string]any{"secret": "unknown-must-not-be-logged"}},
	} {
		response, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.name, Arguments: tc.input})
		if err == nil && !response.IsError {
			t.Fatalf("无效调用 %s 未拒绝", tc.name)
		}
		page := history.Read(cursor, 100)
		cursor = page.NextCursor
		if len(page.Lines) != 1 || page.Lines[0].Severity != "ERROR" || !strings.Contains(page.Lines[0].Text, "code=MCP_REJECTED") || strings.Contains(page.Lines[0].Text, "must-not-be-logged") {
			t.Fatalf("协议层拒绝日志不正确: %+v", page)
		}
	}
}
