//go:build windows || darwin

package credential

// NewNativeBackendForRoot 沿用操作系统凭据后端，无需使用应用根目录。
func NewNativeBackendForRoot(string) (Backend, error) {
	return NewNativeBackend(), nil
}
