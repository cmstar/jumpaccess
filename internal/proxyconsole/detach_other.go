//go:build !windows

package proxyconsole

import "os"

// DetachPrivateProxy 在非 Windows 平台上不执行任何操作。
func DetachPrivateProxy(_, _ *os.File) {}
