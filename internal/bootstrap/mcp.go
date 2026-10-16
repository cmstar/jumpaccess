package bootstrap

import (
	"net/http"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/mcpserver"
)

// NewMCP 只装配宿主使用的服务，调用 Start 前不监听、不读取访问 Token。
func (r Runtime) NewMCP(host string) *agentmcp.Runtime {
	return agentmcp.NewRuntime(r.MCPSettings, host, func(info agentmcp.Instance, cfg config.MCP, token string) http.Handler {
		return mcpserver.NewHandler(mcpserver.Options{Instance: info, Config: cfg, Token: token})
	})
}
