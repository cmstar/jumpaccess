package cli

import (
	"io"

	"golang.org/x/sys/windows"
)

func mcpTerminalANSI(destination io.Writer) (bool, func()) {
	file, ok := destination.(interface{ Fd() uintptr })
	if !ok {
		return false, func() {}
	}
	handle := windows.Handle(file.Fd())
	var previous uint32
	if windows.GetConsoleMode(handle, &previous) != nil {
		return false, func() {}
	}
	const virtualTerminalProcessing = 0x0004
	if previous&virtualTerminalProcessing != 0 {
		return true, func() {}
	}
	if windows.SetConsoleMode(handle, previous|virtualTerminalProcessing) != nil {
		return false, func() {}
	}
	return true, func() { _ = windows.SetConsoleMode(handle, previous) }
}
