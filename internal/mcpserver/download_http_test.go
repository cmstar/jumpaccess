package mcpserver

import (
	"context"
	"crypto/tls"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/downloadcache"
	"github.com/cmstar/jumpaccess/internal/config"
)

func cacheArtifact(t *testing.T, size int64) (*downloadcache.Store, downloadcache.Artifact) {
	t.Helper()
	store, err := downloadcache.New(downloadcache.Options{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	pending, err := store.Begin(size, ".gz")
	if err != nil {
		t.Fatal(err)
	}
	if err := pending.File.Truncate(size); err != nil {
		t.Fatal(err)
	}
	artifact, err := pending.Commit("日志.gz")
	if err != nil {
		t.Fatal(err)
	}
	return store, artifact
}

func fileServer(t *testing.T, store *downloadcache.Store) (*httptest.Server, *config.MCP) {
	t.Helper()
	cfg := config.DefaultMCP()
	server := httptest.NewUnstartedServer(nil)
	cfg.Port = server.Listener.Addr().(*net.TCPAddr).Port
	server.Config.Handler = NewHandler(Options{Config: cfg, Token: "test-secret", Cache: store})
	server.Start()
	t.Cleanup(server.Close)
	return server, &cfg
}

func TestDownloadHTTPScopedLinkHeadersAndHostProtection(t *testing.T) {
	store, artifact := cacheArtifact(t, 4096)
	server, _ := fileServer(t, store)
	link, err := store.Link(artifact, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, method, host, origin string
		tamper, bearer             bool
		want                       int
	}{
		{name: "GET without MCP token", method: "GET", want: 200},
		{name: "HEAD", method: "HEAD", want: 200},
		{name: "wrong host", method: "GET", host: "evil.invalid:80", want: 403},
		{name: "wrong origin", method: "GET", origin: "https://evil.invalid", want: 403},
		{name: "POST", method: "POST", want: 405},
		{name: "MCP token cannot replace link signature", method: "GET", tamper: true, bearer: true, want: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, _ := url.Parse(link)
			if tc.tamper {
				q := target.Query()
				q.Set("signature", "invalid")
				target.RawQuery = q.Encode()
			}
			req, _ := http.NewRequest(tc.method, target.String(), nil)
			if tc.host != "" {
				req.Host = tc.host
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.bearer {
				req.Header.Set("Authorization", "Bearer test-secret")
			}
			// 首版不承诺断点续传，Range 也只能拿到同一完整内容。
			req.Header.Set("Range", "bytes=100-200")
			response, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != tc.want {
				t.Fatalf("status=%d want=%d: %s", response.StatusCode, tc.want, body)
			}
			if tc.want != 200 {
				return
			}
			kind, params, err := mime.ParseMediaType(response.Header.Get("Content-Disposition"))
			if err != nil || kind != "attachment" || params["filename"] != "日志.gz" {
				t.Fatalf("disposition=%q err=%v", response.Header.Get("Content-Disposition"), err)
			}
			if response.ContentLength != 4096 || response.Header.Get("Content-Encoding") != "" || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("headers=%v", response.Header)
			}
			if tc.method == "HEAD" && len(body) != 0 || tc.method == "GET" && len(body) != 4096 {
				t.Fatalf("body length=%d", len(body))
			}
		})
	}
}

func TestDownloadHTTPBusyDoesNotConsumeHEADOrDeleteArtifact(t *testing.T) {
	store, artifact := cacheArtifact(t, 16)
	server, _ := fileServer(t, store)
	link, _ := store.Link(artifact, server.URL)
	u, _ := url.Parse(link)
	open := func() *downloadcache.Lease {
		lease, err := store.Open(artifact.Name, u.Query().Get("filename"), u.Query().Get("signature"), false)
		if err != nil {
			t.Fatal(err)
		}
		return lease
	}
	first, second := open(), open()
	defer first.Close()
	defer second.Close()
	response, err := server.Client().Get(link)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 429 || response.Header.Get("Retry-After") != "5" {
		t.Fatalf("busy=%d %v", response.StatusCode, response.Header)
	}
	response, err = server.Client().Head(link)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("HEAD=%d", response.StatusCode)
	}
	first.Close()
	response, err = server.Client().Get(link)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if data, err := io.ReadAll(response.Body); response.StatusCode != 200 || err != nil || len(data) != 16 {
		t.Fatalf("retry status=%d bytes=%d err=%v", response.StatusCode, len(data), err)
	}
}

func TestDownloadStreamOverridesServerDeadlineAndReleasesTimedOutLease(t *testing.T) {
	for _, tc := range []struct {
		name               string
		idle, total, pause time.Duration
		wantComplete       bool
	}{
		{"override server write timeout", time.Second, 2 * time.Second, 100 * time.Millisecond, true},
		{"idle timeout", 40 * time.Millisecond, time.Second, 150 * time.Millisecond, false},
		{"total timeout", time.Second, 60 * time.Millisecond, 150 * time.Millisecond, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, artifact := cacheArtifact(t, 8<<20)
			server := httptest.NewUnstartedServer(downloadHandler(store, downloadHTTPTimeouts{Idle: tc.idle, Total: tc.total}))
			server.Config.WriteTimeout = 20 * time.Millisecond
			server.Config.ConnContext = func(ctx context.Context, c net.Conn) context.Context {
				_ = c.(*net.TCPConn).SetWriteBuffer(1024)
				return ctx
			}
			server.Start()
			defer server.Close()
			link, _ := store.Link(artifact, server.URL)
			response, err := server.Client().Get(link)
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(tc.pause)
			n, copyErr := io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if tc.wantComplete && (copyErr != nil || n != artifact.Size) {
				t.Fatalf("inherited server timeout truncated: %d / %d, %v", n, artifact.Size, copyErr)
			}
			if !tc.wantComplete && (copyErr == nil || n >= artifact.Size) {
				t.Fatalf("timeout did not truncate: %d / %d, %v", n, artifact.Size, copyErr)
			}
			u, _ := url.Parse(link)
			for range 2 {
				lease, err := store.Open(artifact.Name, u.Query().Get("filename"), u.Query().Get("signature"), false)
				if err != nil {
					t.Fatalf("request slot was not released: %v", err)
				}
				defer lease.Close()
			}
		})
	}
}

func TestDownloadRequestBaseUsesVerifiedHostAndTLS(t *testing.T) {
	cfg := config.DefaultMCP()
	cfg.Port = 12345
	handler := protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, downloadBaseURL(r.Context())) }), Options{Config: cfg, Token: "secret"})
	for _, host := range []string{"127.0.0.1", "localhost"} {
		req := httptest.NewRequest("POST", "http://"+net.JoinHostPort(host, strconv.Itoa(cfg.Port))+"/mcp", strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("X-Forwarded-Host", "evil.invalid")
		req.Header.Set("X-Forwarded-Proto", "https")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != 200 || response.Body.String() != "http://"+req.Host {
			t.Fatalf("base=%d %q", response.Code, response.Body.String())
		}
	}
	req := httptest.NewRequest("POST", "https://127.0.0.1:12345/mcp", nil)
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != 200 || response.Body.String() != "https://127.0.0.1:12345" {
		t.Fatalf("TLS base=%d %q", response.Code, response.Body.String())
	}
}

func TestDownloadStoreShutdownInterruptsBlockedHTTPBody(t *testing.T) {
	store, artifact := cacheArtifact(t, 8<<20)
	server := httptest.NewUnstartedServer(downloadHandler(store, downloadHTTPTimeouts{Idle: time.Minute, Total: 15 * time.Minute}))
	server.Config.ConnContext = func(ctx context.Context, c net.Conn) context.Context {
		_ = c.(*net.TCPConn).SetWriteBuffer(1024)
		return ctx
	}
	server.Start()
	defer server.Close()
	link, _ := store.Link(artifact, server.URL)
	response, err := server.Client().Get(link)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, response.Body); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("停止后仍报告完整领取")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("缓存停止未取消 HTTP 正文传输")
	}
}

func TestDownloadHTTPRejectsRequestBodyBeforeClearingReadDeadline(t *testing.T) {
	store, artifact := cacheArtifact(t, 16)
	server, _ := fileServer(t, store)
	link, _ := store.Link(artifact, server.URL)
	req, _ := http.NewRequest(http.MethodGet, link, strings.NewReader("unexpected"))
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || !response.Close {
		t.Fatalf("request body was accepted: status=%d close=%v", response.StatusCode, response.Close)
	}
}

type delayedDownloadReader struct {
	entered, release chan struct{}
}

func (r *delayedDownloadReader) Read(p []byte) (int, error) {
	close(r.entered)
	<-r.release
	return copy(p, "prepared bytes"), io.EOF
}

type cancelDeadlineWriter struct {
	*httptest.ResponseRecorder
	mu        sync.Mutex
	cancelled chan struct{}
	once      sync.Once
	deadline  time.Time
}

func (w *cancelDeadlineWriter) SetReadDeadline(time.Time) error { return nil }
func (w *cancelDeadlineWriter) SetWriteDeadline(value time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deadline = value
	if !value.After(time.Now()) {
		w.once.Do(func() { close(w.cancelled) })
	}
	return nil
}

func TestDownloadCancellationDuringReadCannotRestoreFutureWriteDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &delayedDownloadReader{entered: make(chan struct{}), release: make(chan struct{})}
	writer := &cancelDeadlineWriter{ResponseRecorder: httptest.NewRecorder(), cancelled: make(chan struct{})}
	request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		streamDownload(writer, request, reader, make(chan struct{}), downloadHTTPTimeouts{Idle: time.Minute, Total: 15 * time.Minute})
	}()
	<-reader.entered
	cancel()
	select {
	case <-writer.cancelled:
	case <-time.After(time.Second):
		t.Fatal("取消没有设置已到期写入截止时间")
	}
	close(reader.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("取消后读取完成仍未结束领取")
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.deadline.After(time.Now()) || writer.Body.Len() != 0 {
		t.Fatalf("取消被后续读取覆盖：deadline=%v bytes=%d", writer.deadline, writer.Body.Len())
	}
}
