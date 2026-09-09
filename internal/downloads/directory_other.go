//go:build !windows && (!darwin || !cgo)

package downloads

import (
	"os"
	"path/filepath"
)

// 无系统目录 API 的构建使用用户 Downloads；不可用时由 CLI 提示另选目录。
func Directory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads"), nil
}
