package mcpserver

import (
	"context"
	"errors"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerQueries(server *mcp.Server, instance string, queries *agentmcp.Queries, engine *agentmcp.Engine) {
	addQuery(server, instance, "list_profiles", "列出 Profile 和认证状态；不触发交互登录。", queries.ListProfiles, engine)
	addQuery(server, instance, "list_organizations", "查询指定 Profile 可访问的组织。", queries.ListOrganizations, engine)
	addQuery(server, instance, "list_assets", "在明确的 Profile 和 Organization ID 下分页查询资产。", queries.ListAssets, engine)
	addQuery(server, instance, "get_asset", "只按明确 Asset ID 查询详情和授权账号，不进行名称匹配。", queries.GetAsset, engine)
	addQuery(server, instance, "list_accounts", "查询指定资产的授权账号及操作能力，不返回密码。", queries.ListAccounts, engine)
	addQuery(server, instance, "list_aliases", "列出 Alias 绑定与解析状态，歧义名称不会自动选择目标。", queries.ListAliases, engine)
}

func addQuery[In, Out any](server *mcp.Server, instance, name, description string, query func(context.Context, In) (Out, error), engine *agentmcp.Engine) {
	mcp.AddTool(server, &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, req *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Result[Out], error) {
		markCallHandled(ctx)
		started := time.Now()
		data, err := query(ctx, input)
		if engine != nil {
			engine.RecordCall(name, err, callDetails(input, data, started))
		}
		result := Result[Out]{SchemaVersion: 1, ServerInstanceID: instance, OK: err == nil}
		if err == nil {
			result.Data = &data
			return nil, result, nil
		}
		problem := &ToolError{Code: "INTERNAL_ERROR", Message: "查询未能完成，请检查服务状态"}
		var known *agentmcp.QueryError
		if errors.As(err, &known) {
			problem.Code = known.Code
			problem.Message = known.Message
			problem.Retryable = known.Retryable
		}
		result.Error = problem
		return &mcp.CallToolResult{IsError: true}, result, nil
	})
}
