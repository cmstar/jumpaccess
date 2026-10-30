//go:build windows || darwin

package credential

// NewNativeBackendForRoot returns the operating system credential backend.
// Desktop backends do not need the application root directory.
func NewNativeBackendForRoot(string) (Backend, error) {
	return NewNativeBackend(), nil
}
