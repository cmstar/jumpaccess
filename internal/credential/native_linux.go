//go:build linux

package credential

import (
	"fmt"
	"path/filepath"
	"strings"
)

const linuxProxyHostKeyCredential = "ssh/proxy-host-key"

// NewNativeBackendForRoot stores Linux-native credentials in a dedicated
// private directory under the application root. This works without a desktop
// keyring in WSL and headless environments.
func NewNativeBackendForRoot(rootDir string) (Backend, error) {
	if strings.TrimSpace(rootDir) == "" {
		return nil, fmt.Errorf("application root directory is empty")
	}
	return newFixedFileBackend(
		filepath.Join(rootDir, "ssh", "proxy_host_ed25519_key"),
		linuxProxyHostKeyCredential,
	), nil
}
