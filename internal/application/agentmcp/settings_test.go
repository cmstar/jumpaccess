package agentmcp

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/filelock"
)

func testSettings(t *testing.T) Settings {
	t.Helper()
	root := t.TempDir()
	return Settings{Store: config.Store{Path: filepath.Join(root, "config.toml")}, Credentials: credential.NewFileBackend(filepath.Join(root, "credentials")), Locker: filelock.Locker{Dir: filepath.Join(root, "locks")}}
}

func TestGenerateMCPTokenOnlyReturnsDraft(t *testing.T) {
	s := testSettings(t)
	unlock, err := s.AcquireInstance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	first, err := s.GenerateToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.GenerateToken(context.Background())
	if err != nil || first == second {
		t.Fatal("运行中应能生成不同的草稿", err)
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(first)
	if err != nil || len(decoded) != 32 {
		t.Fatal("Token 随机值格式错误")
	}
	configured, err := s.TokenConfigured()
	if err != nil || configured {
		t.Fatal("生成不应改变已保存配置", err)
	}
	if _, err := os.Stat(s.Store.Path); !os.IsNotExist(err) {
		t.Fatal("生成不应写配置文件")
	}
	if _, err := s.Credentials.Get(tokenKey); !errors.Is(err, credential.ErrNotFound) {
		t.Fatal("生成不应写独立凭据", err)
	}
}

func TestMCPTokenSavedWithConfiguration(t *testing.T) {
	s := testSettings(t)
	token, err := s.GenerateToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	value, err := config.Decode([]byte("version = 1\n[mcp]\nport = 39288\ntoken = '" + token + "'\n"))
	if err != nil {
		t.Fatal("MCP 配置应接受访问 Token", err)
	}
	unlock, err := s.AcquireInstance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := s.SaveConfig(context.Background(), value.MCPSettings()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.Store.Path)
	if err != nil || !strings.Contains(string(data), token) {
		t.Fatal("Token 应随设置保存到 config.toml", err)
	}
	got, err := s.Config()
	if err != nil || got != value.MCPSettings() {
		t.Fatal("完整配置未原样保存", err)
	}
	if _, err := s.AcquireInstance(context.Background()); !errors.Is(err, ErrRunning) {
		t.Fatal("保存配置不应释放服务实例锁", err)
	}
	if _, err := s.Credentials.Get(tokenKey); !errors.Is(err, credential.ErrNotFound) {
		t.Fatal("保存不应写独立凭据", err)
	}
}

func TestLegacyMCPTokenReadDoesNotWriteAndExplicitSaveUsesTOML(t *testing.T) {
	s := testSettings(t)
	ctx := context.Background()
	legacy, _ := s.GenerateToken(ctx)
	if err := s.Credentials.Set(tokenKey, []byte(legacy)); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Config()
	if err != nil || cfg.Token != legacy {
		t.Fatal("应兼容已有独立 Token", err)
	}
	if _, err := os.Stat(s.Store.Path); !os.IsNotExist(err) {
		t.Fatal("读取不应迁移文件")
	}
	if err := s.SaveConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	replacement, _ := s.GenerateToken(ctx)
	if err := s.Credentials.Set(tokenKey, []byte(replacement)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Config()
	if err != nil || got.Token != legacy {
		t.Fatal("已有 TOML Token 时不应再读取旧凭据", err)
	}
}

func TestInvalidMCPSettingsDoNotReplaceSavedConfiguration(t *testing.T) {
	s := testSettings(t)
	ctx := context.Background()
	cfg := config.DefaultMCP()
	cfg.Token, _ = s.GenerateToken(ctx)
	if err := s.SaveConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.Store.Path)
	for name, change := range map[string]func(*config.MCP){
		"端口过大": func(c *config.MCP) { c.Port = 100001 }, "零端口": func(c *config.MCP) { c.Port = 0 }, "负端口": func(c *config.MCP) { c.Port = -1 },
		"无效地址": func(c *config.MCP) { c.ListenHost = "999.1.2.3" }, "未允许LAN": func(c *config.MCP) { c.ListenHost = "0.0.0.0" },
		"空Token": func(c *config.MCP) { c.Token = "" }, "错误Token": func(c *config.MCP) { c.Token = "invalid-secret" },
		"缺少私钥": func(c *config.MCP) { c.TLSCert = "missing.crt" }, "无效证书": func(c *config.MCP) { c.TLSCert = "missing.crt"; c.TLSKey = "missing.key" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := cfg
			change(&invalid)
			if err := s.SaveConfig(ctx, invalid); err == nil || strings.Contains(err.Error(), "invalid-secret") {
				t.Fatal("应拒绝无效配置且不回显 Token")
			}
			after, _ := os.ReadFile(s.Store.Path)
			if string(before) != string(after) {
				t.Fatal("失败保存改变了文件")
			}
		})
	}
}
