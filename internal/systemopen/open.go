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
	case "linux":
		return "xdg-open", []string{path}, nil
	default:
		return "", nil, fmt.Errorf("unsupported operating system %q", goos)
	}
}

// Open 启动系统关联程序。Linux 等待 xdg-open 的退出结果；它可能持续到目标应用关闭。
// 需要继续处理交互的调用方应并发等待结果，不能阻塞在本调用上。
func Open(path string) error {
	name, args, err := CommandFor(runtime.GOOS, path)
	if err != nil {
		return err
	}
	command := exec.Command(name, args...)
	if runtime.GOOS == "linux" {
		if err := command.Run(); err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		return nil
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	// Windows/macOS 保留异步打开行为，但仍需回收启动器进程资源。
	go func() { _ = command.Wait() }()
	return nil
}
