//go:build windows

// Package proxyconsole 负责让标准传输流已重定向的 Windows ProxyCommand 进程
// 在连接成功后脱离私有控制台，同时避免影响共享或交互控制台。
package proxyconsole

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	procFreeConsole           = kernel32.NewProc("FreeConsole")
)

type consoleAPI interface {
	IsConsole(windows.Handle) (bool, error)
	ProcessCount() (uint32, error)
	Free() error
}

type nativeConsoleAPI struct{}

// DetachPrivateProxy 是一项 best-effort 优化。只有 stdin、stdout 均已重定向，
// 并且当前控制台只附着 jumpctl 一个进程时才执行脱离。
func DetachPrivateProxy(stdin, stdout *os.File) {
	if stdin == nil || stdout == nil {
		return
	}
	detachPrivateProxy(nativeConsoleAPI{}, [2]windows.Handle{
		windows.Handle(stdin.Fd()),
		windows.Handle(stdout.Fd()),
	})
}

func detachPrivateProxy(api consoleAPI, handles [2]windows.Handle) bool {
	for _, handle := range handles {
		isConsole, err := api.IsConsole(handle)
		if err != nil || isConsole {
			return false
		}
	}
	count, err := api.ProcessCount()
	if err != nil || count != 1 {
		return false
	}
	return api.Free() == nil
}

func (nativeConsoleAPI) IsConsole(handle windows.Handle) (bool, error) {
	if handle == 0 || handle == windows.InvalidHandle {
		return false, nil
	}
	var mode uint32
	err := windows.GetConsoleMode(handle, &mode)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, windows.ERROR_INVALID_HANDLE) {
		return false, nil
	}
	return false, fmt.Errorf("inspect standard handle: %w", err)
}

func (nativeConsoleAPI) ProcessCount() (uint32, error) {
	var processIDs [2]uint32
	count, _, callErr := procGetConsoleProcessList.Call(
		uintptr(unsafe.Pointer(&processIDs[0])),
		uintptr(len(processIDs)),
	)
	if count == 0 {
		return 0, windowsCallError("GetConsoleProcessList", callErr)
	}
	return uint32(count), nil
}

func (nativeConsoleAPI) Free() error {
	ok, _, callErr := procFreeConsole.Call()
	if ok == 0 {
		return windowsCallError("FreeConsole", callErr)
	}
	return nil
}

func windowsCallError(name string, err error) error {
	if err == nil || errors.Is(err, windows.ERROR_SUCCESS) {
		return fmt.Errorf("%s failed", name)
	}
	return fmt.Errorf("%s: %w", name, err)
}
