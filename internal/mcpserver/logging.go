package mcpserver

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type callHandledKey struct{}

func markCallHandled(ctx context.Context) {
	if handled, ok := ctx.Value(callHandledKey{}).(*bool); ok {
		*handled = true
	}
}

// SDK 在类型化处理器之前拒绝的调用也要留下摘要；不复制原始参数或错误正文。
func logRejectedCalls(engine *agentmcp.Engine) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			handled := false
			started := time.Now()
			result, err := next(context.WithValue(ctx, callHandledKey{}, &handled), method, req)
			if !handled {
				name := "tools/call"
				if call, ok := req.(*mcp.CallToolRequest); ok && call.Params != nil {
					name = call.Params.Name
				}
				engine.RecordCall(name, &agentmcp.Fault{Code: "MCP_REJECTED"}, agentmcp.CallDetails{Duration: time.Since(started)})
			}
			return result, err
		}
	}
}

// 条件采用显式白名单；不能把整个输入 Marshal，否则执行命令和环境变量会重复留存。
func queryConditions(input any) string {
	page := func(p agentmcp.PageInput) string {
		limit := p.Limit
		if limit == 0 {
			limit = 100
		}
		return fmt.Sprintf("limit=%d  has_cursor=%t", limit, p.Cursor != "")
	}
	profile := func(p, org string) string { return "profile=" + logQuote(p) + "  organization=" + logQuote(org) }
	switch in := input.(type) {
	case agentmcp.PageInput:
		return page(in)
	case agentmcp.ProfilePageInput:
		return "profile=" + logQuote(in.Profile) + "  " + page(in.PageInput)
	case agentmcp.AssetsInput:
		return profile(in.Profile, in.Organization) + "  search=" + logQuote(in.Search) + "  " + page(in.PageInput)
	case agentmcp.AssetInput:
		return profile(in.Profile, in.Organization) + "  asset_id=" + logQuote(in.AssetID)
	case agentmcp.AccountsInput:
		return profile(in.Profile, in.Organization) + "  asset_id=" + logQuote(in.AssetID) + "  " + page(in.PageInput)
	case agentmcp.AliasesInput:
		return profile(in.Profile, in.Organization) + "  " + page(in.PageInput)
	case agentmcp.ConnectionInput:
		return "connection_id=" + logQuote(in.ConnectionID)
	case agentmcp.GetExecutionInput:
		return fmt.Sprintf("execution_id=%s  cursor=%d  max_bytes=%d  wait_ms=%d", logQuote(in.ExecutionID), in.Cursor, in.MaxBytes, in.WaitMS)
	case agentmcp.ExecutionsPageInput:
		return "connection_id=" + logQuote(in.ConnectionID) + "  status=" + logQuote(in.Status) + "  " + page(in.PageInput)
	case agentmcp.GetDownloadInput:
		return "download_id=" + logQuote(in.DownloadID)
	case agentmcp.DownloadIDInput:
		return "download_id=" + logQuote(in.DownloadID)
	case agentmcp.DownloadsPageInput:
		return "status=" + logQuote(in.Status) + "  " + page(in.PageInput)
	}
	return ""
}

func logQuote(value string) string {
	if len(value) > 512 {
		value = value[:512]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
		value += "…"
	}
	return strconv.Quote(value)
}

func pageSummary[T any](p agentmcp.Page[T]) string {
	return fmt.Sprintf("count=%d  total=%d  has_more=%t", len(p.Items), p.Total, p.NextCursor != "")
}

// 只提取计数、标识与状态，不序列化结果正文，尤其不能把 Output 带入日志。
func callDetails(input, output any, started time.Time) agentmcp.CallDetails {
	d := agentmcp.CallDetails{Conditions: queryConditions(input), Duration: time.Since(started)}
	switch in := input.(type) {
	case agentmcp.ConnectionInput:
		d.ConnectionID = in.ConnectionID
	case agentmcp.CloseInput:
		d.ConnectionID = in.ConnectionID
	case agentmcp.ExecuteInput:
		d.ConnectionID = in.ConnectionID
	case agentmcp.GetExecutionInput:
		d.ExecutionID = in.ExecutionID
	case agentmcp.CancelInput:
		d.ExecutionID = in.ExecutionID
	}
	switch out := output.(type) {
	case agentmcp.Page[agentmcp.ProfileView]:
		d.Summary = pageSummary(out)
	case agentmcp.Page[jumpserver.Organization]:
		d.Summary = pageSummary(out)
	case agentmcp.Page[agentmcp.AssetView]:
		d.Summary = pageSummary(out)
	case agentmcp.Page[agentmcp.AccountView]:
		d.Summary = pageSummary(out)
	case agentmcp.Page[agentmcp.AliasView]:
		d.Summary = pageSummary(out)
	case agentmcp.Page[agentmcp.ConnectionView]:
		d.Summary = pageSummary(out)
	case agentmcp.Page[agentmcp.ExecutionView]:
		d.Summary = pageSummary(out)
	case agentmcp.Page[agentmcp.DownloadView]:
		d.Summary = pageSummary(out)
	case agentmcp.DownloadView:
		d.Summary = "download_id=" + logQuote(out.ID) + "  status=" + logQuote(out.Status) + "  phase=" + logQuote(out.Phase)
	case agentmcp.AssetResult:
		d.Summary = fmt.Sprintf("count=1  accounts=%d  protocols=%d", len(out.Accounts), len(out.Protocols))
	case agentmcp.ConnectionView:
		d.ConnectionID = firstNonEmpty(out.ID, d.ConnectionID)
		d.Summary = "status=" + logQuote(out.Status)
	case agentmcp.ExecutionView:
		d.ConnectionID = firstNonEmpty(out.ConnectionID, d.ConnectionID)
		d.ExecutionID = firstNonEmpty(out.ID, d.ExecutionID)
		d.Summary = "execution_id=" + logQuote(d.ExecutionID) + "  status=" + logQuote(out.Status)
	case ServerInfo:
		d.Summary = "state=" + logQuote(out.State)
	}
	if in, ok := input.(agentmcp.CloseInput); ok {
		d.Summary += "  mode=" + logQuote(firstNonEmpty(in.Mode, "if_idle"))
	}
	if in, ok := input.(agentmcp.CancelInput); ok {
		d.Summary += "  mode=" + logQuote(firstNonEmpty(in.Mode, "request_stop"))
	}
	return d
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
