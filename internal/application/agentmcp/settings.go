package agentmcp

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/filelock"
)

var ErrRunning = errors.New("MCP 实例正在运行或管理操作忙碌，请先停止服务后重试")

const tokenKey = "mcp/access-token"

type Settings struct {
	Store config.Store
	// 仅兼容旧开发版的独立 Token 文件；显式保存后以 config.toml 为准。
	Credentials credential.Backend
	Locker      filelock.Locker
}

func (s Settings) Config() (config.MCP, error) {
	value, err := s.Store.Load()
	if err != nil {
		return config.MCP{}, err
	}
	cfg := value.MCPSettings()
	if cfg.Token == "" && s.Credentials != nil {
		data, err := s.Credentials.Get(tokenKey)
		if err != nil && !errors.Is(err, credential.ErrNotFound) {
			return config.MCP{}, err
		}
		defer clear(data)
		if err == nil {
			cfg.Token = string(data)
			if err := config.ValidateMCPToken(cfg.Token); err != nil {
				return config.MCP{}, err
			}
		}
	}
	return cfg, nil
}

// SaveConfig 一次原子写入完整配置；运行中的服务继续使用启动快照。
func (s Settings) SaveConfig(ctx context.Context, value config.MCP) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if err := config.ValidateMCPToken(value.Token); err != nil {
		return err
	}
	if value.TLSCert != "" {
		if _, err := tls.LoadX509KeyPair(value.TLSCert, value.TLSKey); err != nil {
			return fmt.Errorf("无法加载 MCP TLS 证书和私钥，请检查路径、权限与内容")
		}
	}
	return s.Store.Update(ctx, func(current *config.Config) error { current.MCP = &value; return nil })
}

// AcquireInstance 约束服务生存期，同一应用数据目录只允许一个宿主监听。
func (s Settings) AcquireInstance(ctx context.Context) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lockContext, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	unlock, err := s.Locker.Lock(lockContext, "mcp-instance")
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, ErrRunning
		}
		return nil, err
	}
	return unlock, nil
}

func (s Settings) TokenConfigured() (bool, error) {
	cfg, err := s.Config()
	return cfg.Token != "", err
}

// GenerateToken 只生成草稿，不写配置或独立凭据，也不要求服务停止。
func (s Settings) GenerateToken(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("生成 MCP Token 失败: %w", err)
	}
	defer clear(raw[:])
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
