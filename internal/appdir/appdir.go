package appdir

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

const directoryName = "JumpAccess"

// Root returns the single directory used for non-sensitive JumpAccess data.
func Root() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}

	return RootFor(runtime.GOOS, os.Getenv("LOCALAPPDATA"), configDir)
}

// RootFor exposes the platform decision separately from OS discovery so that
// both supported layouts can be verified on either development platform.
func RootFor(goos, localAppData, userConfigDir string) (string, error) {
	switch goos {
	case "windows":
		if strings.TrimSpace(localAppData) == "" {
			return "", fmt.Errorf("LOCALAPPDATA is not set")
		}
		return filepath.Join(localAppData, directoryName), nil
	case "darwin":
		if strings.TrimSpace(userConfigDir) == "" {
			return "", fmt.Errorf("user config directory is empty")
		}
		return path.Join(userConfigDir, directoryName), nil
	default:
		return "", fmt.Errorf("unsupported operating system %q", goos)
	}
}
