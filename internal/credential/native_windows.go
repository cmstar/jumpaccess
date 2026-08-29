//go:build windows

package credential

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	credentialTypeGeneric     = 1
	credentialPersistLocal    = 2
	credentialNotFoundErrCode = windows.Errno(1168)
)

var (
	advapi32        = windows.NewLazySystemDLL("advapi32.dll")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

type windowsCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type nativeBackend struct{}

func NewNativeBackend() Backend {
	return nativeBackend{}
}

func NativeBackendAvailable() bool {
	return true
}

func (nativeBackend) Get(key string) ([]byte, error) {
	target, err := windows.UTF16PtrFromString(nativeTarget(key))
	if err != nil {
		return nil, fmt.Errorf("encode credential target: %w", err)
	}
	var found *windowsCredential
	result, _, callErr := procCredReadW.Call(
		uintptr(unsafe.Pointer(target)),
		credentialTypeGeneric,
		0,
		uintptr(unsafe.Pointer(&found)),
	)
	if result == 0 {
		if callErr == credentialNotFoundErrCode {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read Windows credential: %w", callErr)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(found)))
	if found.CredentialBlobSize == 0 {
		return []byte{}, nil
	}
	return append([]byte(nil), unsafe.Slice(found.CredentialBlob, int(found.CredentialBlobSize))...), nil
}

func (nativeBackend) Set(key string, value []byte) error {
	target, err := windows.UTF16PtrFromString(nativeTarget(key))
	if err != nil {
		return fmt.Errorf("encode credential target: %w", err)
	}
	username, err := windows.UTF16PtrFromString(key)
	if err != nil {
		return fmt.Errorf("encode credential username: %w", err)
	}
	var blob *byte
	if len(value) > 0 {
		blob = &value[0]
	}
	item := windowsCredential{
		Type:               credentialTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(value)),
		CredentialBlob:     blob,
		Persist:            credentialPersistLocal,
		UserName:           username,
	}
	result, _, callErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&item)), 0)
	if result == 0 {
		return fmt.Errorf("write Windows credential: %w", callErr)
	}
	return nil
}

func (nativeBackend) Delete(key string) error {
	target, err := windows.UTF16PtrFromString(nativeTarget(key))
	if err != nil {
		return fmt.Errorf("encode credential target: %w", err)
	}
	result, _, callErr := procCredDeleteW.Call(uintptr(unsafe.Pointer(target)), credentialTypeGeneric, 0)
	if result == 0 {
		if callErr == credentialNotFoundErrCode {
			return ErrNotFound
		}
		return fmt.Errorf("delete Windows credential: %w", callErr)
	}
	return nil
}
