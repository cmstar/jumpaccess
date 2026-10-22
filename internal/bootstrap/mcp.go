package bootstrap

import (
	"net/http"
	"path/filepath"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/mcpserver"
)

// NewMCP 只装配宿主使用的服务，调用 Start 前不监听、不读取访问 Token。
func (r Runtime) NewMCP(host string) *agentmcp.Runtime {
	var runtime *agentmcp.Runtime
	runtime = agentmcp.NewRuntime(r.MCPSettings, host, func(info agentmcp.Instance, cfg config.MCP, token string) http.Handler {
		queries := agentmcp.NewQueries(info.ID, r.Store, r.Auth, r.Resources)
		connector := agentmcp.Connector{Config: r.Store, Auth: r.AuthManager, Queries: queries, Connect: r.Connect, KnownHosts: filepath.Join(r.RootDir, "known_hosts"), Timeout: r.Configuration.Behavior.ConnectTimeout.Duration}
		engine := agentmcp.NewEngine(agentmcp.EngineOptions{Instance: info.ID, History: runtime.History(), Resolve: connector.Resolve, Validate: connector.Validate, Dial: connector.Dial, OpenSFTP: connector.OpenDownloadSFTP})
		return mcpserver.NewHandler(mcpserver.Options{Instance: info, Config: cfg, Token: token, Queries: queries, Engine: engine})
	})
	return runtime
}
