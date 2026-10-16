package agentmcp

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
)

type Instance struct {
	ID        string     `json:"id"`
	Host      string     `json:"host"`
	URL       string     `json:"url"`
	StartedAt time.Time  `json:"startedAt"`
	Config    config.MCP `json:"config"`
}

type Status struct {
	State           string     `json:"state"`
	Instance        *Instance  `json:"instance"`
	Config          config.MCP `json:"config"`
	TokenConfigured bool       `json:"tokenConfigured"`
	RestartRequired bool       `json:"restartRequired"`
	Error           string     `json:"error"`
}

type HandlerFactory func(Instance, config.MCP, string) http.Handler

// Runtime 的生存期属于宿主进程，不属于启动请求或 MCP 协议会话。
type Runtime struct {
	settings  Settings
	host      string
	handler   HandlerFactory
	mu        sync.Mutex
	active    *runningServer
	lastError string
}

type runningServer struct {
	info     Instance
	server   *http.Server
	done     chan struct{}
	cancel   context.CancelFunc
	unlock   func() error
	stopping bool
}

func NewRuntime(settings Settings, host string, handler HandlerFactory) *Runtime {
	return &Runtime{settings: settings, host: host, handler: handler}
}

func (r *Runtime) Start(ctx context.Context) (Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	if r.active != nil {
		cfg, err := r.settings.Config()
		if err != nil {
			return Status{}, err
		}
		return r.localStatus(cfg, true), nil
	}
	unlock, err := r.settings.AcquireInstance(ctx)
	if err != nil {
		return Status{}, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = unlock()
		}
	}()
	cfg, err := r.settings.Config()
	if err != nil {
		return Status{}, err
	}
	token, err := r.settings.ShowToken()
	if errors.Is(err, credential.ErrNotFound) {
		return Status{}, fmt.Errorf("请先配置 MCP 访问 Token")
	}
	if err != nil {
		return Status{}, err
	}
	if r.handler == nil {
		return Status{}, fmt.Errorf("MCP 协议处理器不可用")
	}
	var tlsConfig *tls.Config
	if cfg.TLSCert != "" {
		certificate, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			return Status{}, fmt.Errorf("无法加载 MCP TLS 证书和私钥，请检查路径、权限与内容")
		}
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Status{}, err
	}
	info := Instance{ID: hex.EncodeToString(id[:]), Host: r.host, URL: cfg.URL(), StartedAt: time.Now().UTC(), Config: cfg}
	handler := r.handler(info, cfg, token)
	if handler == nil {
		return Status{}, fmt.Errorf("MCP 协议处理器不可用")
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.Address())
	if err != nil {
		r.lastError = "MCP 监听失败，请检查地址或端口占用"
		return Status{}, fmt.Errorf("%s: %w", r.lastError, err)
	}
	if tlsConfig != nil {
		listener = tls.NewListener(listener, tlsConfig)
	}
	base, cancel := context.WithCancel(context.Background())
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 65 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 16 << 10, BaseContext: func(net.Listener) context.Context { return base }}
	run := &runningServer{info: info, server: server, cancel: cancel, unlock: unlock, done: make(chan struct{})}
	if err := r.writeInstance(info); err != nil {
		cancel()
		listener.Close()
		return Status{}, err
	}
	r.active, r.lastError, keep = run, "", true
	go func() {
		err := server.Serve(listener)
		close(run.done)
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.active == run && !run.stopping {
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				r.lastError = "MCP HTTP 服务意外停止"
			}
			r.clearLocked(run)
		}
	}()
	return r.localStatus(cfg, true), nil
}

func (r *Runtime) Stop(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run := r.active
	if run == nil {
		return nil
	}
	run.stopping = true
	run.cancel()
	err := run.server.Shutdown(ctx)
	if err != nil {
		_ = run.server.Close()
	}
	<-run.done
	r.clearLocked(run)
	return err
}

func (r *Runtime) clearLocked(run *runningServer) {
	run.cancel()
	_ = run.server.Close()
	_ = os.Remove(r.instancePath())
	_ = run.unlock()
	r.active = nil
}

func (r *Runtime) Status(ctx context.Context) (Status, error) {
	cfg, err := r.settings.Config()
	if err != nil {
		return Status{}, err
	}
	hasToken, err := r.settings.TokenConfigured()
	if err != nil {
		return Status{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	status := r.localStatus(cfg, hasToken)
	if r.active != nil {
		return status, nil
	}
	unlock, err := r.settings.AcquireInstance(ctx)
	if err == nil {
		_ = unlock()
		return status, nil
	}
	if !errors.Is(err, ErrRunning) {
		return Status{}, err
	}
	status.State = "external"
	// 锁表明其他进程占用；文件仅作展示，不据此向任意地址发送凭据。
	file, err := os.Open(r.instancePath())
	if err == nil {
		defer file.Close()
		var info Instance
		if json.NewDecoder(io.LimitReader(file, 32<<10)).Decode(&info) == nil && info.ID != "" {
			status.Instance = &info
			status.RestartRequired = cfg != info.Config
		}
	}
	return status, nil
}

func (r *Runtime) localStatus(cfg config.MCP, hasToken bool) Status {
	status := Status{State: "stopped", Config: cfg, TokenConfigured: hasToken, Error: r.lastError}
	if r.lastError != "" {
		status.State = "failed"
	}
	if r.active != nil {
		info := r.active.info
		status.State = "running"
		status.Instance = &info
		status.RestartRequired = cfg != info.Config
	}
	return status
}

func (r *Runtime) instancePath() string {
	return filepath.Join(filepath.Dir(r.settings.Store.Path), "mcp-instance.json")
}

func (r *Runtime) writeInstance(info Instance) error {
	file, err := os.CreateTemp(filepath.Dir(r.instancePath()), ".mcp-instance-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if err := json.NewEncoder(file).Encode(info); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), r.instancePath())
}
