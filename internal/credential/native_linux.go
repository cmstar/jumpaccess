//go:build linux

package credential

import (
	"fmt"
	"path/filepath"
	"strings"
)

const linuxProxyHostKeyCredential = "ssh/proxy-host-key"

// NewNativeBackendForRoot 在应用根目录内保存 Linux ProxyCommand 主机密钥，
// 供 WSL 与无桌面环境使用，不依赖桌面 Keyring。
func NewNativeBackendForRoot(rootDir string) (Backend, error) {
	if strings.TrimSpace(rootDir) == "" {
		return nil, fmt.Errorf("application root directory is empty")
	}
	return newFixedFileBackend(
		filepath.Join(rootDir, "ssh", "proxy_host_ed25519_key"),
		linuxProxyHostKeyCredential,
	), nil
}
