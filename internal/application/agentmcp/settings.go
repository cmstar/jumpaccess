package agentmcp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/filelock"
)

var (
	ErrRunning     = errors.New("MCP 实例正在运行或管理操作忙碌，请先停止服务后重试")
	ErrTokenExists = errors.New("MCP Token 已存在，请使用重新生成或替换操作")
)

const tokenKey = "mcp/access-token"

type Settings struct {
	Store       config.Store
	Credentials credential.Backend
	Locker      filelock.Locker
}

func (s Settings) Config() (config.MCP, error) {
	value, err := s.Store.Load()
	if err != nil {
		return config.MCP{}, err
	}
	return value.MCPSettings(), nil
}

func (s Settings) SaveConfig(ctx context.Context, value config.MCP) error {
	if err := value.Validate(); err != nil {
		return err
	}
	return s.Store.Update(ctx, func(current *config.Config) error { current.MCP = &value; return nil })
}

// AcquireInstance 同时约束服务生存期与 Token 修改，避免启动读取凭据时发生轮换。
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
	_, err := s.ShowToken()
	if errors.Is(err, credential.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (s Settings) ShowToken() (string, error) {
	data, err := s.Credentials.Get(tokenKey)
	if err != nil {
		return "", err
	}
	defer clear(data)
	token := string(data)
	if err := validateToken(token); err != nil {
		return "", err
	}
	return token, nil
}

func (s Settings) SetToken(ctx context.Context, value string) error {
	if err := validateToken(value); err != nil {
		return err
	}
	unlock, err := s.AcquireInstance(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	return s.saveToken(value)
}

func (s Settings) GenerateToken(ctx context.Context, replace bool) (string, error) {
	unlock, err := s.AcquireInstance(ctx)
	if err != nil {
		return "", err
	}
	defer unlock()
	if !replace {
		configured, err := s.TokenConfigured()
		if err != nil {
			return "", err
		}
		if configured {
			return "", ErrTokenExists
		}
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("生成 MCP Token 失败: %w", err)
	}
	defer clear(raw[:])
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	if err := s.saveToken(token); err != nil {
		return "", err
	}
	return token, nil
}

func (s Settings) saveToken(value string) error {
	data := []byte(value)
	defer clear(data)
	return s.Credentials.Set(tokenKey, data)
}

func validateToken(value string) error {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	defer clear(decoded)
	if err != nil || len(value) != 43 || len(decoded) != 32 {
		return fmt.Errorf("MCP Token 必须为 32 字节随机值的无填充 Base64url 编码")
	}
	return nil
}
