package agentmcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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
)

type Instance struct {
	ID                string     `json:"id"`
	Host              string     `json:"host"`
	URL               string     `json:"url"`
	StartedAt         time.Time  `json:"startedAt"`
	Config            config.MCP `json:"config"`
	ConfigFingerprint string     `json:"configFingerprint"`
}

type Status struct {
	State           string       `json:"state"`
	Instance        *Instance    `json:"instance"`
	Config          config.MCP   `json:"config"`
	TokenConfigured bool         `json:"tokenConfigured"`
	RestartRequired bool         `json:"restartRequired"`
	Error           string       `json:"error"`
	Counts          *EngineStats `json:"counts,omitempty"`
	StopWarning     string       `json:"stopWarning,omitempty"`
}

type HandlerFactory func(Instance, config.MCP, string) http.Handler

// Runtime 的生存期属于宿主进程，不属于启动请求或 MCP 协议会话。
type Runtime struct {
	settings    Settings
	host        string
	handler     HandlerFactory
	mu          sync.Mutex
	active      *runningServer
	lastError   string
	stopWarning string
	history     *History
}

type runningServer struct {
	info     Instance
	server   *http.Server
	done     chan struct{}
	cancel   context.CancelFunc
	unlock   func() error
	stopping bool
	engine   managedEngine
}

type managedEngine interface {
	Shutdown(context.Context) error
	Stats() EngineStats
}

func NewRuntime(settings Settings, host string, handler HandlerFactory) *Runtime {
	return &Runtime{settings: settings, host: host, handler: handler, history: NewHistory(3000)}
}

func (r *Runtime) History() *History { return r.history }

type connectionManager interface {
	ListConnections(context.Context, struct{}) ([]ConnectionView, error)
	CloseConnection(context.Context, CloseInput) (ConnectionView, error)
}

func (r *Runtime) Connections(ctx context.Context) ([]ConnectionView, error) {
	r.mu.Lock()
	var manager connectionManager
	if r.active != nil {
		manager, _ = r.active.engine.(connectionManager)
	}
	r.mu.Unlock()
	if manager == nil {
		return []ConnectionView{}, nil
	}
	return manager.ListConnections(ctx, struct{}{})
}

func (r *Runtime) CloseConnection(ctx context.Context, input CloseInput) (ConnectionView, error) {
	r.mu.Lock()
	var manager connectionManager
	if r.active != nil {
		manager, _ = r.active.engine.(connectionManager)
	}
	r.mu.Unlock()
	if manager == nil {
		return ConnectionView{}, fault("SERVICE_STOPPED", "MCP 服务未在当前宿主运行")
	}
	return manager.CloseConnection(ctx, input)
}

func (r *Runtime) Start(ctx context.Context) (Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.startLocked(ctx)
}

func (r *Runtime) startLocked(ctx context.Context) (Status, error) {
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
	if err := config.ValidateMCPToken(cfg.Token); err != nil {
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
	info := Instance{ID: hex.EncodeToString(id[:]), Host: r.host, URL: cfg.URL(), StartedAt: time.Now().UTC(), Config: cfg.WithoutToken(), ConfigFingerprint: configFingerprint(cfg)}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.Address())
	if err != nil {
		r.lastError = "MCP 监听失败，请检查地址或端口占用"
		r.history.Record(info.ID, "service_failed", "ERROR", 1, r.lastError)
		return Status{}, fmt.Errorf("%s: %w", r.lastError, err)
	}
	handler := r.handler(info, cfg.WithoutToken(), cfg.Token)
	if handler == nil {
		listener.Close()
		return Status{}, fmt.Errorf("MCP 协议处理器不可用")
	}
	if tlsConfig != nil {
		listener = tls.NewListener(listener, tlsConfig)
	}
	base, cancel := context.WithCancel(context.Background())
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 65 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 16 << 10, BaseContext: func(net.Listener) context.Context { return base }}
	run := &runningServer{info: info, server: server, cancel: cancel, unlock: unlock, done: make(chan struct{})}
	run.engine, _ = handler.(managedEngine)
	if err := r.writeInstance(info); err != nil {
		cancel()
		listener.Close()
		if run.engine != nil {
			_ = run.engine.Shutdown(ctx)
		}
		return Status{}, err
	}
	r.active, r.lastError, keep = run, "", true
	r.history.Record(info.ID, "service_started", "INFO", 1, "MCP 服务已启动  "+info.URL+"  host="+r.host)
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
	return r.stopLocked(ctx)
}

// Restart 将本进程的停止与启动串行化；不会关闭或接管外部宿主。
func (r *Runtime) Restart(ctx context.Context) (Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil {
		return Status{}, fmt.Errorf("MCP 服务未在当前进程运行")
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	if err := r.stopLocked(ctx); err != nil {
		return Status{}, err
	}
	return r.startLocked(ctx)
}

func (r *Runtime) stopLocked(ctx context.Context) error {
	run := r.active
	if run == nil {
		return nil
	}
	run.stopping = true
	r.stopWarning = ""
	if run.engine != nil {
		cleanup, finish := context.WithTimeout(ctx, 4*time.Second)
		cleanupError := run.engine.Shutdown(cleanup)
		finish()
		stats := run.engine.Stats()
		if cleanupError != nil || stats.Executions > 0 || stats.UnknownExecutions > 0 || stats.Downloads > 0 || stats.UnknownDownloads > 0 {
			r.stopWarning = "服务已停止跟踪部分任务，无法确认其远端结果；请核对服务器状态，勿直接重跑原命令。"
		}
	}
	run.cancel()
	err := run.server.Shutdown(ctx)
	if err != nil {
		_ = run.server.Close()
	}
	<-run.done
	r.clearLocked(run)
	r.history.Record(run.info.ID, "service_stopped", "INFO", 1, "MCP 服务已停止")
	if r.stopWarning != "" {
		r.history.Record(run.info.ID, "execution_unknown", "WARN", 1, r.stopWarning)
	}
	return err
}

func (r *Runtime) clearLocked(run *runningServer) {
	run.cancel()
	_ = run.server.Close()
	if !run.stopping && run.engine != nil {
		r.history.Record(run.info.ID, "service_failed", "ERROR", 1, "MCP HTTP 服务意外停止")
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = run.engine.Shutdown(ctx)
		cancel()
	}
	_ = os.Remove(r.instancePath())
	if retired, ok := run.engine.(interface{ Retire() }); ok {
		retired.Retire()
	}
	_ = run.unlock()
	r.active = nil
}

func (r *Runtime) Status(ctx context.Context) (Status, error) {
	cfg, err := r.settings.Config()
	if err != nil {
		return Status{}, err
	}
	hasToken := cfg.Token != ""
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
			status.RestartRequired = configFingerprint(cfg) != info.ConfigFingerprint
		}
	}
	return status, nil
}

func (r *Runtime) localStatus(cfg config.MCP, hasToken bool) Status {
	status := Status{State: "stopped", Config: cfg, TokenConfigured: hasToken, Error: r.lastError, StopWarning: r.stopWarning}
	if r.lastError != "" {
		status.State = "failed"
	}
	if r.active != nil {
		info := r.active.info
		status.State = "running"
		status.Instance = &info
		status.RestartRequired = configFingerprint(cfg) != info.ConfigFingerprint
		if r.active.engine != nil {
			counts := r.active.engine.Stats()
			status.Counts = &counts
		}
	}
	return status
}

// 实例描述只保存整体配置指纹，Token 明文仅存在配置文件和鉴权内存中。
func configFingerprint(cfg config.MCP) string {
	data, _ := json.Marshal(cfg)
	defer clear(data)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
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
