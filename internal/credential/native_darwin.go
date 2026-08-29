//go:build darwin && cgo

package credential

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

type nativeBackend struct{}

func NewNativeBackend() Backend {
	return nativeBackend{}
}

func (nativeBackend) Get(key string) ([]byte, error) {
	target := []byte(nativeTarget(key))
	var length C.UInt32
	var data unsafe.Pointer
	status := C.SecKeychainFindGenericPassword(
		nil,
		C.UInt32(len(target)), unsafe.Pointer(&target[0]),
		0, nil,
		&length, &data,
		nil,
	)
	if status == C.errSecItemNotFound {
		return nil, ErrNotFound
	}
	if status != C.errSecSuccess {
		return nil, fmt.Errorf("read macOS Keychain credential: status %d", int(status))
	}
	defer C.SecKeychainItemFreeContent(nil, data)
	return C.GoBytes(data, C.int(length)), nil
}

func (nativeBackend) Set(key string, value []byte) error {
	target := []byte(nativeTarget(key))
	var item C.SecKeychainItemRef
	status := C.SecKeychainFindGenericPassword(
		nil,
		C.UInt32(len(target)), unsafe.Pointer(&target[0]),
		0, nil,
		nil, nil,
		&item,
	)
	var valuePtr unsafe.Pointer
	if len(value) > 0 {
		valuePtr = unsafe.Pointer(&value[0])
	}
	if status == C.errSecSuccess {
		defer C.CFRelease(C.CFTypeRef(item))
		status = C.SecKeychainItemModifyAttributesAndData(item, nil, C.UInt32(len(value)), valuePtr)
	} else if status == C.errSecItemNotFound {
		status = C.SecKeychainAddGenericPassword(
			nil,
			C.UInt32(len(target)), unsafe.Pointer(&target[0]),
			0, nil,
			C.UInt32(len(value)), valuePtr,
			nil,
		)
	}
	if status != C.errSecSuccess {
		return fmt.Errorf("write macOS Keychain credential: status %d", int(status))
	}
	return nil
}
func (nativeBackend) Delete(key string) error {
	target := []byte(nativeTarget(key))
	var item C.SecKeychainItemRef
	status := C.SecKeychainFindGenericPassword(
		nil,
		C.UInt32(len(target)), unsafe.Pointer(&target[0]),
		0, nil,
		nil, nil,
		&item,
	)
	if status == C.errSecItemNotFound {
		return ErrNotFound
	}
	if status != C.errSecSuccess {
		return fmt.Errorf("find macOS Keychain credential: status %d", int(status))
	}
	defer C.CFRelease(C.CFTypeRef(item))
	if status = C.SecKeychainItemDelete(item); status != C.errSecSuccess {
		return fmt.Errorf("delete macOS Keychain credential: status %d", int(status))
	}
	return nil
}
