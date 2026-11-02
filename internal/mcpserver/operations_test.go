package mcpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callOperation[T any](t *testing.T, session *mcp.ClientSession, name string, input any) mcpserver.Result[T] {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: input})
	if err != nil {
		t.Fatal(err)
	}
	var out mcpserver.Result[T]
	data, _ := json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOperationDiscoveryEpochErrorsAndRuntimeOwnership(t *testing.T) {
	_, settings, token := runtimeFixture(t)
	var engine *agentmcp.Engine
	r := agentmcp.NewRuntime(settings, "gui", func(info agentmcp.Instance, cfg config.MCP, token string) http.Handler {
		engine = agentmcp.NewEngine(agentmcp.EngineOptions{Instance: info.ID, Resolve: func(context.Context, agentmcp.Target) (agentmcp.ResolvedTarget, error) {
			return agentmcp.ResolvedTarget{}, &agentmcp.Fault{Code: "TARGET_INCOMPLETE", Message: "synthetic"}
		}})
		return mcpserver.NewHandler(mcpserver.Options{Instance: info, Config: cfg, Token: token, Engine: engine})
	})
	status, err := r.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop(context.Background())
	session, err := mcp.NewClient(&mcp.Implementation{Name: "control-test", Version: "1"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: status.Instance.URL, HTTPClient: &http.Client{Transport: bearerTransport{token}}, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 13 {
		t.Fatalf("tools=%+v %v", tools, err)
	}
	for _, tool := range tools.Tools {
		if tool.InputSchema == nil || tool.OutputSchema == nil {
			t.Fatal("missing schema", tool.Name)
		}
		if tool.Name == "execute_command" && tool.Annotations.ReadOnlyHint {
			t.Fatal("exec declared readonly")
		}
	}
	info := callOperation[mcpserver.ServerInfo](t, session, "get_server_info", map[string]any{})
	if !info.OK || info.Data.RequestEpoch != 1 || info.Data.Counts == nil {
		t.Fatalf("info %+v", info)
	}
	opened := callOperation[agentmcp.OpenResult](t, session, "open_connection", map[string]any{"target": map[string]any{"profile": "p"}, "request_key": status.Instance.ID + ":1:00000000-0000-4000-8000-000000000001"})
	if !opened.OK || opened.Data.Error.Code != "TARGET_INCOMPLETE" {
		t.Fatalf("open %+v", opened)
	}
	invalid := callOperation[agentmcp.ExecutionView](t, session, "execute_command", map[string]any{"connection_id": "old:c:1", "request_key": status.Instance.ID + ":1:00000000-0000-4000-8000-000000000002", "command": "true", "cwd": "/"})
	if invalid.OK || invalid.Error.Code != "INSTANCE_CHANGED" || invalid.Error.Details["accepted"] != false {
		t.Fatalf("invalid %+v", invalid)
	}
	if err := r.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !engine.Stats().Draining {
		t.Fatal("HTTP Runtime stop left business engine alive")
	}
}
