package mcpserver

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/uploadcache"
	"github.com/cmstar/jumpaccess/internal/config"
)

func TestHTTPUploadLengthCredentialAndFreeze(t *testing.T) {
	cache, err := uploadcache.New(uploadcache.Options{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	source, _ := cache.Reserve(4, nil)
	sig, _ := cache.Signature(source.ID)
	server := httptest.NewServer(uploadHandler(cache, downloadHTTPTimeouts{Idle: time.Second, Total: time.Second * 3}))
	defer server.Close()
	url := server.URL + uploadcache.RoutePrefix + source.ID + "?signature=" + sig
	send := func(address, body string, chunked bool) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPut, address, strings.NewReader(body))
		if chunked {
			req.ContentLength = -1
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		return res.StatusCode
	}
	if status := send(url+"&signature=bad", "data", false); status != 403 {
		t.Fatal(status)
	}
	if status := send(url, "short", false); status != 400 {
		t.Fatal(status)
	}
	if status := send(url, "ab", true); status != 400 {
		t.Fatal(status)
	}
	if status := send(url, "extra", true); status != 400 {
		t.Fatal(status)
	}
	if status := send(url, "data", true); status != 200 {
		t.Fatal(status)
	}
	if status := send(url, "new!", false); status != 409 {
		t.Fatal(status)
	}
	lease, err := cache.Claim(source.ID, "job")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	data, _ := io.ReadAll(lease.File)
	if !bytes.Equal(data, []byte("data")) {
		t.Fatal("content changed")
	}
}

func TestHTTPUploadScopedCredentialAndHostProtection(t *testing.T) {
	cache, _ := uploadcache.New(uploadcache.Options{Directory: t.TempDir(), MaxTransfers: 1})
	defer cache.Close()
	cfg := config.DefaultMCP()
	server := httptest.NewUnstartedServer(nil)
	cfg.Port = server.Listener.Addr().(*net.TCPAddr).Port
	server.Config.Handler = NewHandler(Options{Config: cfg, Token: "mcp-secret", UploadCache: cache})
	server.Start()
	defer server.Close()
	for _, tc := range []struct {
		name, method, host, origin string
		tamper                     bool
		status                     int
	}{{"put", "PUT", "", "", false, 200}, {"wrong host", "PUT", "evil.test:80", "", false, 403}, {"wrong origin", "PUT", "", "http://evil.test", false, 403}, {"get", "GET", "", "", false, 405}, {"MCP token cannot replace signature", "PUT", "", "", true, 403}} {
		t.Run(tc.name, func(t *testing.T) {
			source, _ := cache.Reserve(0, nil)
			sig, _ := cache.Signature(source.ID)
			if tc.tamper {
				sig = "bad"
			}
			req, _ := http.NewRequest(tc.method, server.URL+uploadcache.RoutePrefix+source.ID+"?signature="+sig, nil)
			if tc.host != "" {
				req.Host = tc.host
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.tamper {
				req.Header.Set("Authorization", "Bearer mcp-secret")
			}
			res, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.status {
				t.Fatal(res.StatusCode)
			}
			if res.Header.Get("Cache-Control") != "no-store" {
				t.Fatal("credential response cacheable")
			}
		})
	}
	source, _ := cache.Reserve(0, nil)
	sig, _ := cache.Signature(source.ID)
	req, _ := http.NewRequest("POST", server.URL+"/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+sig)
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("scoped token authorized MCP", res.StatusCode)
	}
	receiver, _ := cache.BeginReceive(source.ID, sig)
	defer receiver.Abort()
	second, _ := cache.Reserve(0, nil)
	sig2, _ := cache.Signature(second.ID)
	req, _ = http.NewRequest("PUT", server.URL+uploadcache.RoutePrefix+second.ID+"?signature="+sig2, nil)
	res, err = server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 429 || res.Header.Get("Retry-After") != "5" {
		t.Fatal("missing receive backpressure", res.StatusCode)
	}
}

func TestHTTPUploadIdleTimeoutAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "cancel"}[cancelled], func(t *testing.T) {
			cache, _ := uploadcache.New(uploadcache.Options{Directory: t.TempDir()})
			defer cache.Close()
			source, _ := cache.Reserve(4, nil)
			sig, _ := cache.Signature(source.ID)
			server := httptest.NewServer(uploadHandler(cache, downloadHTTPTimeouts{Idle: 100 * time.Millisecond, Total: time.Second}))
			defer server.Close()
			reader, writer := io.Pipe()
			defer writer.Close()
			defer reader.Close()
			req, _ := http.NewRequest(http.MethodPut, server.URL+uploadcache.RoutePrefix+source.ID+"?signature="+sig, reader)
			req.ContentLength = 4
			done := make(chan int, 1)
			go func() {
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					done <- 0
					return
				}
				defer res.Body.Close()
				done <- res.StatusCode
			}()
			writer.Write([]byte("a"))
			if cancelled {
				deadline := time.Now().Add(time.Second)
				for {
					v, err := cache.Get(source.ID)
					if err != nil {
						t.Fatal(err)
					}
					if v.State == "receiving" {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("receive did not start")
					}
					time.Sleep(time.Millisecond)
				}
				cache.Cancel(source.ID)
			}
			select {
			case status := <-done:
				if status != 408 {
					t.Fatal("wrong interruption status", status)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("HTTP upload did not stop")
			}
			if !cancelled {
				view, err := cache.Get(source.ID)
				if err != nil || view.State != "waiting" {
					t.Fatalf("partial content was retained: %+v %v", view, err)
				}
			}
		})
	}
}
