package agentmcp

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

func TestUpdateMCPConfigSavesTokenAndFieldsTogether(t *testing.T) {
	s := testSettings(t)
	ctx := context.Background()
	original := config.Default()
	original.CurrentProfile = "work"
	original.Profiles["work"] = config.Profile{URL: "https://example.test", Organization: "org", Aliases: map[string]config.Alias{"server": {Asset: "asset", Account: "account"}}}
	if err := s.Store.Save(original); err != nil {
		t.Fatal(err)
	}
	token, err := s.GenerateToken(ctx)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.UpdateConfig(ctx, func(value *config.MCP) error {
		value.ListenHost = "0.0.0.0"
		value.AllowLAN = true
		value.Port = 39288
		value.Token = token
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.Store.Load()
	if err != nil || current.MCPSettings() != saved || saved.Token != token || saved.Port != 39288 || !saved.AllowLAN || saved.ListenHost != "0.0.0.0" {
		t.Fatal("首次补丁应同时保存 Token 与完整网络设置", err)
	}
	if current.CurrentProfile != original.CurrentProfile || current.Behavior != original.Behavior || !reflect.DeepEqual(current.Profiles, original.Profiles) {
		t.Fatal("MCP 补丁改变了其他配置")
	}
	updated, err := s.UpdateConfig(ctx, func(value *config.MCP) error { value.Port++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	saved.Port++
	if updated != saved {
		t.Fatal("单项修改应保留未指定字段")
	}
}

func TestUpdateMCPConfigSerializesReadModifyWrite(t *testing.T) {
	s := testSettings(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	initial := config.DefaultMCP()
	initial.Token, _ = s.GenerateToken(ctx)
	if err := s.SaveConfig(ctx, initial); err != nil {
		t.Fatal(err)
	}
	const count = 12
	start := make(chan struct{})
	results := make(chan error, count)
	for range count {
		go func() {
			<-start
			_, err := s.UpdateConfig(ctx, func(value *config.MCP) error { value.Port++; return nil })
			results <- err
		}()
	}
	close(start)
	for range count {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	current, err := s.Config()
	if err != nil || current.Port != initial.Port+count || current.Token != initial.Token {
		t.Fatal("并发补丁未基于锁内最新配置提交", err)
	}
}

func TestUpdateMCPConfigCallbackRunsUnderConfigurationLock(t *testing.T) {
	s := testSettings(t)
	ctx := context.Background()
	_, err := s.UpdateConfig(ctx, func(value *config.MCP) error {
		waiting, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
		defer cancel()
		unlock, err := s.Locker.Lock(waiting, "config")
		if err == nil {
			_ = unlock()
			return errors.New("字段合并回调未持有配置锁")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		value.Token, err = s.GenerateToken(ctx)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUpdateMCPConfigFailuresPreserveSavedConfiguration(t *testing.T) {
	s := testSettings(t)
	ctx := context.Background()
	initial := config.DefaultMCP()
	initial.Token, _ = s.GenerateToken(ctx)
	if err := s.SaveConfig(ctx, initial); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.Store.Path)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*config.MCP) error{
		"回调失败":    func(value *config.MCP) error { value.Port++; return errors.New("修改未完成") },
		"无效端口":    func(value *config.MCP) error { value.Port = 0; return nil },
		"缺少Token": func(value *config.MCP) error { value.Token = ""; return nil },
		"缺少TLS私钥": func(value *config.MCP) error { value.TLSCert = "missing.crt"; return nil },
		"证书不可加载":  func(value *config.MCP) error { value.TLSCert, value.TLSKey = "missing.crt", "missing.key"; return nil },
		"生成Token后网络无效": func(value *config.MCP) error {
			value.Token, _ = s.GenerateToken(ctx)
			value.ListenHost = "0.0.0.0"
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := s.UpdateConfig(ctx, change)
			if err == nil || got != (config.MCP{}) {
				t.Fatal("失败补丁不应返回未保存的配置")
			}
			after, err := os.ReadFile(s.Store.Path)
			if err != nil || string(before) != string(after) {
				t.Fatal("失败补丁改变了原配置", err)
			}
		})
	}
}

func TestUpdateMCPConfigMigratesLegacyTokenOnlyOnSuccess(t *testing.T) {
	s := testSettings(t)
	ctx := context.Background()
	legacy, _ := s.GenerateToken(ctx)
	if err := s.Credentials.Set(tokenKey, []byte(legacy)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateConfig(ctx, func(value *config.MCP) error { value.Port = 0; return nil }); err == nil {
		t.Fatal("无效补丁应失败")
	}
	if _, err := os.Stat(s.Store.Path); !os.IsNotExist(err) {
		t.Fatal("失败补丁不应迁移旧 Token 或创建配置")
	}
	saved, err := s.UpdateConfig(ctx, func(value *config.MCP) error { value.Port++; return nil })
	if err != nil || saved.Token != legacy {
		t.Fatal("显式修改应保留并迁移旧 Token", err)
	}
	current, err := s.Store.Load()
	if err != nil || current.MCPSettings() != saved {
		t.Fatal("兼容 Token 未随配置保存", err)
	}
	replacement, _ := s.GenerateToken(ctx)
	if err := s.Credentials.Set(tokenKey, []byte(replacement)); err != nil {
		t.Fatal(err)
	}
	saved, err = s.UpdateConfig(ctx, func(value *config.MCP) error { value.Port++; return nil })
	if err != nil || saved.Token != legacy {
		t.Fatal("已有 TOML Token 时补丁不应再读取旧凭据", err)
	}
}

func TestUpdateMCPConfigCanceledBeforeSaveDoesNotWrite(t *testing.T) {
	s := testSettings(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := s.UpdateConfig(ctx, func(value *config.MCP) error {
		value.Token, _ = s.GenerateToken(ctx)
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("保存前取消应返回 context.Canceled", err)
	}
	if _, err := os.Stat(s.Store.Path); !os.IsNotExist(err) {
		t.Fatal("取消的补丁不应写配置")
	}
}
