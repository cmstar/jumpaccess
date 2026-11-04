package mcpserver

import (
	"context"
	"net/url"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/application/uploadcache"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerUploadSources(server *mcp.Server, instance string, e *agentmcp.Engine, cache *uploadcache.Store) {
	addOperation(server, instance, "prepare_upload", "为 HTTP 上传申请接收资格。用于异机来源或 MCP 服务无法读取本机源文件；调用方须能读取并发送内容。明确 target 或 connection_id、remote_directory、准备包实际 size、format 和 entries，并提供新请求键。单普通文件用 gzip，目录或多来源用 tar.gz，已有压缩文件用 raw；只解开本次新增外层。entries 按包内顺序列出 name、原始 size、directory，目录大小为 0，父目录在前，路径相对且无链接、重复或越界。包最多 1 GiB、清单最多 1000 项。返回 source_id 和限时 PUT url；仅发送准备包原始字节，正文不进入 MCP 参数。收齐不等于远端完成，随后 commit_upload。重复请求保留原键，不续期。", false, decorateUploadSource(cache, e.PrepareUpload), e)
	addOperation(server, instance, "get_upload_source", "查询接收对象，preparing 等待目标准备，waiting 可通过限时 PUT url 发送，receiving 等待接收结束，ready 才可 commit_upload，submitted 查看 upload_id。查询不续期；HTTP 响应丢失时先查询，不直接重新写入。ready 有效期自收齐起 30 分钟；当前状态和 URL 不证明远端已发布。", true, decorateUploadSource(cache, e.GetUploadSource), e)
	addOperation(server, instance, "cancel_upload_source", "取消尚未提交的 HTTP 接收对象，正在接收时请求停止并清理半成品。提交后须按 upload_id 使用 cancel_upload，不能通过取消接收对象删除已发布文件。", false, e.CancelUploadSource, e)
	addOperation(server, instance, "commit_upload", "将 ready 接收对象提交为远端上传任务，提供 source_id 和新请求键，可指定 timeout_ms 与 wait_ms。同一对象只能被一项任务接管，相同请求键重试返回原任务；失败、未知或记录过期不得换键重放。提交重新校验目标身份和授权，复用既有 SFTP/rz、还原与排他发布流程。通过 get_upload 等待，只有 files_ready=true 表示全部最终文件已发布。", false, e.CommitUpload, e)
}

func decorateUploadSource[In any](cache *uploadcache.Store, call func(context.Context, In) (agentmcp.UploadSourceView, error)) func(context.Context, In) (agentmcp.UploadSourceView, error) {
	return func(ctx context.Context, in In) (agentmcp.UploadSourceView, error) {
		v, err := call(ctx, in)
		if err != nil {
			return v, err
		}
		if v.State == "waiting" && downloadBaseURL(ctx) != "" {
			signature, err := cache.Signature(v.ID)
			if err != nil {
				return agentmcp.UploadSourceView{}, &agentmcp.Fault{Code: "UPLOAD_SOURCE_EXPIRED", Message: "上传接收资格已失效，请查询原对象"}
			}
			v.URL = downloadBaseURL(ctx) + uploadcache.RoutePrefix + url.PathEscape(v.ID) + "?signature=" + url.QueryEscape(signature)
			v.Method = "PUT"
		}
		return v, nil
	}
}
