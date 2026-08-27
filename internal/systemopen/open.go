package systemopen

import (
	"fmt"
	"os/exec"
	"runtime"
)

func CommandFor(goos, path string) (string, []string, error) {
	switch goos {
	case "windows":
		return "rundll32.exe", []string{"url.dll,FileProtocolHandler", path}, nil
	case "darwin":
		return "open", []string{path}, nil
	default:
		return "", nil, fmt.Errorf("unsupported operating system %q", goos)
	}
}

func Open(path string) error {
	name, args, err := CommandFor(runtime.GOOS, path)
	if err != nil {
		return err
	}
	if err := exec.Command(name, args...).Start(); err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	return nil
}
