package agentmcp

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestMCPTokenIsSeparatePrivateCredential(t *testing.T) {
	s := testSettings(t)
	ctx := context.Background()
	configured, err := s.TokenConfigured()
	if err != nil || configured {
		t.Fatal("初始状态应无 Token", err)
	}
	token, err := s.GenerateToken(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 {
		t.Fatal("生成值应为 32 字节随机 Token")
	}
	if _, err := s.GenerateToken(ctx, false); !errors.Is(err, ErrTokenExists) {
		t.Fatal("生成不应覆盖已有凭据", err)
	}
	if err := s.SaveConfig(ctx, config.DefaultMCP()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.Store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), token) {
		t.Fatal("Token 泄露到普通配置")
	}
	if _, err := (credential.Repository{Backend: s.Credentials}).Load("mcp"); !errors.Is(err, credential.ErrNotFound) {
		t.Fatal("MCP 不应写入 OAuth 命名空间")
	}
	before, err := s.ShowToken()
	if err != nil || before != token {
		t.Fatal("无法显式读取 Token", err)
	}
	after, err := s.GenerateToken(ctx, true)
	if err != nil || after == before {
		t.Fatal("轮换应保存新值", err)
	}
	if shown, err := s.ShowToken(); err != nil || shown != after {
		t.Fatal("轮换结果未保存", err)
	}
	for _, invalid := range []string{"", "password", strings.Repeat("A", 42), after + "="} {
		if err := s.SetToken(ctx, invalid); err == nil || strings.Contains(err.Error(), invalid) && invalid != "" {
			t.Fatal("应拒绝非法值且不在错误中泄露输入")
		}
	}
	if shown, _ := s.ShowToken(); shown != after {
		t.Fatal("非法输入改变了凭据")
	}
}

func TestMCPTokenChangesRequireStoppedInstance(t *testing.T) {
	s := testSettings(t)
	ctx := context.Background()
	unlock, err := s.AcquireInstance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := s.GenerateToken(ctx, false); !errors.Is(err, ErrRunning) {
		t.Fatal("运行中不应更改 Token", err)
	}
	// 网络配置可以保存为待重启配置，不影响当前实例的监听快照。
	if err := s.SaveConfig(ctx, config.DefaultMCP()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcquireInstance(ctx); !errors.Is(err, ErrRunning) {
		t.Fatal("同一数据目录只允许一个实例", err)
	}
}

func TestMCPConcurrentGenerateOnlyCreatesOneToken(t *testing.T) {
	s := testSettings(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.GenerateToken(context.Background(), false)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrTokenExists) && !errors.Is(err, ErrRunning) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatal("并发生成覆盖了凭据")
	}
}
