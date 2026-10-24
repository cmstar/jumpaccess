//go:build !windows

package cli

import (
	"io"

	"golang.org/x/term"
)

func mcpTerminalANSI(destination io.Writer) (bool, func()) {
	file, ok := destination.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(int(file.Fd())), func() {}
}
