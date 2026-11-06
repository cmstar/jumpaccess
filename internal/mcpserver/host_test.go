package mcpserver

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cmstar/jumpaccess/internal/config"
)

// 全网卡配置的请求保护用内存请求验证，日常回归不需要绑定真实网卡。
func TestLANWildcardHostAndOriginProtection(t *testing.T) {
	for _, tc := range []struct {
		name, listen, local, host, origin, token string
		want                                     int
	}{
		{"IPv4 local address", "0.0.0.0", "192.0.2.10", "192.0.2.10:39287", "", "test-token", http.StatusNoContent},
		{"IPv6 local address", "::", "2001:db8::10", "[2001:db8::10]:39287", "", "test-token", http.StatusNoContent},
		{"loopback", "0.0.0.0", "127.0.0.1", "127.0.0.1:39287", "", "test-token", http.StatusNoContent},
		{"same origin", "0.0.0.0", "192.0.2.10", "192.0.2.10:39287", "http://192.0.2.10:39287", "test-token", http.StatusNoContent},
		{"different address", "0.0.0.0", "192.0.2.10", "192.0.2.11:39287", "", "test-token", http.StatusForbidden},
		{"wrong port", "0.0.0.0", "192.0.2.10", "192.0.2.10:39288", "", "test-token", http.StatusForbidden},
		{"unspecified IPv4", "0.0.0.0", "192.0.2.10", "0.0.0.0:39287", "", "test-token", http.StatusForbidden},
		{"unspecified IPv6", "::", "2001:db8::10", "[::]:39287", "", "test-token", http.StatusForbidden},
		{"missing local address", "0.0.0.0", "", "192.0.2.10:39287", "", "test-token", http.StatusForbidden},
		{"foreign origin", "0.0.0.0", "192.0.2.10", "192.0.2.10:39287", "http://foreign.example.test", "test-token", http.StatusForbidden},
		{"missing token", "0.0.0.0", "192.0.2.10", "192.0.2.10:39287", "", "", http.StatusUnauthorized},
		{"wrong token", "0.0.0.0", "192.0.2.10", "192.0.2.10:39287", "", "wrong-token", http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultMCP()
			cfg.ListenHost, cfg.AllowLAN = tc.listen, true
			handler := protect(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}), Options{Config: cfg, Token: "test-token"})
			request := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/mcp", nil)
			if tc.local != "" {
				address := &net.TCPAddr{IP: net.ParseIP(tc.local), Port: cfg.Port}
				request = request.WithContext(context.WithValue(request.Context(), http.LocalAddrContextKey, address))
			}
			if tc.token != "" {
				request.Header.Set("Authorization", "Bearer "+tc.token)
			}
			if tc.origin != "" {
				request.Header.Set("Origin", tc.origin)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("HTTP 状态 = %d, want %d", response.Code, tc.want)
			}
		})
	}
}
