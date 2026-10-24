package cli

import (
	"io"
	"os"
)

func prepareMCPColor(destination io.Writer, mode string) (bool, func()) {
	if mode == "never" || (mode == "auto" && (os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb")) {
		return false, func() {}
	}
	supported, restore := mcpTerminalANSI(destination)
	return mode == "always" || supported, restore
}
