package bootstrap

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/application/downloadcache"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/mcpserver"
)

// NewMCP 只装配宿主使用的服务，调用 Start 前不监听、不读取访问 Token。
func (r Runtime) NewMCP(host string) *agentmcp.Runtime {
	var runtime *agentmcp.Runtime
	runtime = agentmcp.NewRuntime(r.MCPSettings, host, func(info agentmcp.Instance, cfg config.MCP, token string) http.Handler {
		cache, err := downloadcache.New(downloadcache.Options{Directory: filepath.Join(r.RootDir, "cache", "mcp-downloads"), OnCleanupError: func(error) {
			runtime.History().Record(info.ID, "download_cache_cleanup", "WARN", 1, "领取缓存清理失败，文件仍占用额度，将继续尝试清理")
		}})
		if err != nil {
			return mcpStartupFailure{err: fmt.Errorf("无法初始化 MCP 下载领取缓存: %w", err)}
		}
		queries := agentmcp.NewQueries(info.ID, r.Store, r.Auth, r.Resources)
		connector := agentmcp.Connector{Config: r.Store, Auth: r.AuthManager, Queries: queries, Connect: r.Connect, KnownHosts: filepath.Join(r.RootDir, "known_hosts"), Timeout: r.Configuration.Behavior.ConnectTimeout.Duration}
		engine := agentmcp.NewEngine(agentmcp.EngineOptions{Instance: info.ID, History: runtime.History(), Resolve: connector.Resolve, Validate: connector.Validate, Dial: connector.Dial, OpenSFTP: connector.OpenDownloadSFTP, DeliveryMode: agentmcp.DownloadDeliveryMode(cfg), DownloadCache: cache})
		return mcpserver.NewHandler(mcpserver.Options{Instance: info, Config: cfg, Token: token, Queries: queries, Engine: engine, Cache: cache})
	})
	return runtime
}

type mcpStartupFailure struct{ err error }

func (h mcpStartupFailure) StartupError() error { return h.err }
func (h mcpStartupFailure) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "MCP 服务不可用", http.StatusServiceUnavailable)
}
