package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// 自动启动只使用进程启动时读取的 GUI 偏好，不依赖前端 Tab 或后续偏好保存。
func (a *desktopApp) startMCPOnLaunch(ctx context.Context) {
	a.mcpStartupMu.Lock()
	defer a.mcpStartupMu.Unlock()
	if a.mcpStartupHandled {
		return
	}
	a.mcpStartupHandled = true
	if !a.initialPreferences.MCP.AutoStart || a.mcp == nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	a.stopMCPStartup = cancel
	go func() {
		defer cancel()
		if _, err := a.mcp.Start(ctx); err != nil && ctx.Err() == nil {
			message := "MCP 自动启动失败，请在 MCP 模块检查配置和监听端口后手动启动。"
			if errors.Is(err, agentmcp.ErrRunning) {
				message = "MCP 自动启动失败：其他进程正在提供服务或执行管理操作，请在 MCP 模块查看状态。"
			}
			// 保留到共享日志中；前端即使稍后就绪也能读取并提示，不记录凭据或原始错误内容。
			a.mcp.History().Record("", "auto_start_failed", "ERROR", 1, message)
		}
	}()
}

func (a *desktopApp) cancelMCPAutoStart() {
	a.mcpStartupMu.Lock()
	defer a.mcpStartupMu.Unlock()
	// 停止先于启动回调时，也不能再安排后台启动。
	a.mcpStartupHandled = true
	if a.stopMCPStartup != nil {
		a.stopMCPStartup()
	}
}

func (a *desktopApp) requireMCP() error {
	if a.mcp == nil {
		return fmt.Errorf("MCP 服务尚未初始化")
	}
	return nil
}

func (a *desktopApp) GetMCPStatus() (agentmcp.Status, error) {
	if err := a.requireMCP(); err != nil {
		return agentmcp.Status{}, err
	}
	return a.mcp.Status(a.context())
}

func (a *desktopApp) ReadMCPLogs(cursor uint64, limit int) (agentmcp.LogPage, error) {
	if err := a.requireMCP(); err != nil {
		return agentmcp.LogPage{}, err
	}
	return a.mcp.History().Read(cursor, limit), nil
}

func (a *desktopApp) ListMCPConnections() ([]agentmcp.ConnectionView, error) {
	if err := a.requireMCP(); err != nil {
		return nil, err
	}
	return a.mcp.Connections(a.context())
}

func (a *desktopApp) CloseMCPConnection(id, mode string) (agentmcp.ConnectionView, error) {
	if err := a.requireMCP(); err != nil {
		return agentmcp.ConnectionView{}, err
	}
	ctx, cancel := context.WithTimeout(a.context(), 6*time.Second)
	defer cancel()
	return a.mcp.CloseConnection(ctx, agentmcp.CloseInput{ConnectionID: id, Mode: mode})
}

func (a *desktopApp) SaveMCPConfig(value config.MCP) (agentmcp.Status, error) {
	if err := a.requireMCP(); err != nil {
		return agentmcp.Status{}, err
	}
	if err := a.core.MCPSettings.SaveConfig(a.context(), value); err != nil {
		return agentmcp.Status{}, err
	}
	return a.GetMCPStatus()
}

func (a *desktopApp) StartMCP() (agentmcp.Status, error) {
	a.cancelMCPAutoStart()
	if err := a.requireMCP(); err != nil {
		return agentmcp.Status{}, err
	}
	return a.mcp.Start(a.context())
}

func (a *desktopApp) StopMCP() (agentmcp.Status, error) {
	if err := a.requireMCP(); err != nil {
		return agentmcp.Status{}, err
	}
	if err := a.stopMCP(); err != nil {
		return agentmcp.Status{}, err
	}
	return a.GetMCPStatus()
}

func (a *desktopApp) GenerateMCPToken() (string, error) {
	if err := a.requireMCP(); err != nil {
		return "", err
	}
	return a.core.MCPSettings.GenerateToken(a.context())
}

func (a *desktopApp) RestartMCP() (agentmcp.Status, error) {
	a.cancelMCPAutoStart()
	if err := a.requireMCP(); err != nil {
		return agentmcp.Status{}, err
	}
	ctx, cancel := context.WithTimeout(a.context(), 10*time.Second)
	defer cancel()
	return a.mcp.Restart(ctx)
}

func (a *desktopApp) ChooseMCPTLSFile(kind, currentPath string) (string, error) {
	options, err := mcpTLSFileOptions(kind, currentPath)
	if err != nil {
		return "", err
	}
	return runtime.OpenFileDialog(a.context(), options)
}

func mcpTLSFileOptions(kind, currentPath string) (runtime.OpenDialogOptions, error) {
	options := runtime.OpenDialogOptions{}
	switch kind {
	case "certificate":
		options.Title = "选择 TLS 证书文件"
	case "key":
		options.Title = "选择 TLS 私钥文件"
	default:
		return options, fmt.Errorf("未知 MCP TLS 文件类型")
	}
	options.Filters = []runtime.FileFilter{{DisplayName: "证书和密钥 (*.pem;*.crt;*.cer;*.key)", Pattern: "*.pem;*.crt;*.cer;*.key"}, {DisplayName: "所有文件", Pattern: "*"}}
	if directory := filepath.Dir(currentPath); filepath.IsAbs(directory) {
		if info, err := os.Stat(directory); err == nil && info.IsDir() {
			options.DefaultDirectory = directory
		}
	}
	return options, nil
}

func (a *desktopApp) stopMCP() error {
	a.cancelMCPAutoStart()
	if a.mcp == nil {
		return nil
	}
	// GUI 的退出回调可能已被取消，服务清理使用独立且有界的宿主截止时间。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.mcp.Stop(ctx)
}
