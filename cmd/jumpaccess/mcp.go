package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

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
	if a.mcp == nil {
		return nil
	}
	// GUI 的退出回调可能已被取消，服务清理使用独立且有界的宿主截止时间。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.mcp.Stop(ctx)
}
