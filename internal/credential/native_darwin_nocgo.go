//go:build darwin && !cgo

package credential

import "fmt"

type unavailableBackend struct{}

func NewNativeBackend() Backend {
	return unavailableBackend{}
}

func NativeBackendAvailable() bool {
	return false
}

func (unavailableBackend) Get(string) ([]byte, error) {
	return nil, fmt.Errorf("macOS Keychain support requires a CGO-enabled build")
}

func (unavailableBackend) Set(string, []byte) error {
	return fmt.Errorf("macOS Keychain support requires a CGO-enabled build")
}

func (unavailableBackend) Delete(string) error {
	return fmt.Errorf("macOS Keychain support requires a CGO-enabled build")
}
