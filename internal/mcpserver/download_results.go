package mcpserver

import (
	"context"
	"errors"
	"slices"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/application/downloadcache"
)

func decorateDownloadCall[In any](cache *downloadcache.Store, call func(context.Context, In) (agentmcp.DownloadView, error)) func(context.Context, In) (agentmcp.DownloadView, error) {
	return func(ctx context.Context, in In) (agentmcp.DownloadView, error) {
		view, err := call(ctx, in)
		if err == nil {
			view = downloadForRequest(ctx, cache, view)
		}
		return view, err
	}
}

func decorateDownloadPageCall(cache *downloadcache.Store, call func(context.Context, agentmcp.DownloadsPageInput) (agentmcp.Page[agentmcp.DownloadView], error)) func(context.Context, agentmcp.DownloadsPageInput) (agentmcp.Page[agentmcp.DownloadView], error) {
	return func(ctx context.Context, in agentmcp.DownloadsPageInput) (agentmcp.Page[agentmcp.DownloadView], error) {
		page, err := call(ctx, in)
		if err == nil {
			page.Items = slices.Clone(page.Items)
			for i := range page.Items {
				page.Items[i] = downloadForRequest(ctx, cache, page.Items[i])
			}
		}
		return page, err
	}
}

// URL 只在响应时产生，绑定当前已校验的访问地址，不写回共享任务或操作日志。
func downloadForRequest(ctx context.Context, cache *downloadcache.Store, view agentmcp.DownloadView) agentmcp.DownloadView {
	if view.Link == nil {
		return view
	}
	link := *view.Link
	view.Link = &link
	link.URL, link.Available, link.UnavailableReason = "", false, ""
	unavailable := func(reason string) agentmcp.DownloadView {
		link.UnavailableReason = reason
		view.Instructions = reason + "。任务完成仅表示先前的服务端准备已完成；本次不能据此声称文件已保存到调用方。不要自动重新下载远端源文件或追加哈希、rsync 核验。"
		return view
	}
	if cache == nil {
		return unavailable("领取服务不可用")
	}
	if downloadBaseURL(ctx) == "" {
		return unavailable("当前访问地址无法生成领取链接")
	}
	address, err := cache.Link(link.Artifact, downloadBaseURL(ctx))
	if err == nil {
		link.URL, link.Available = address, true
		return view
	}
	switch {
	case errors.Is(err, downloadcache.ErrExpired):
		return unavailable("领取链接已过期")
	case errors.Is(err, downloadcache.ErrNotFound):
		return unavailable("领取文件已不可用")
	case errors.Is(err, downloadcache.ErrClosed):
		return unavailable("领取服务已停止")
	case errors.Is(err, downloadcache.ErrInvalidName):
		return unavailable("领取文件状态已改变，不能继续交付")
	default:
		return unavailable("领取资源当前不可用，请检查服务状态")
	}
}
