//go:build darwin && cgo

package credential

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
// 保持文件型 Keychain、原 service 和空 account，兼容旧版条目。
static CFMutableDictionaryRef credentialQuery(const char *target, CFIndex length) {
    CFStringRef service = CFStringCreateWithBytes(NULL, (const UInt8 *)target, length, kCFStringEncodingUTF8, false);
    if (service == NULL) return NULL;
    CFMutableDictionaryRef query = CFDictionaryCreateMutable(NULL, 0,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    if (query != NULL) {
        CFDictionarySetValue(query, kSecClass, kSecClassGenericPassword);
        CFDictionarySetValue(query, kSecAttrService, service);
        CFDictionarySetValue(query, kSecAttrAccount, CFSTR(""));
    }
    CFRelease(service);
    return query;
}

static OSStatus credentialRead(CFMutableDictionaryRef query, CFDataRef *data) {
    CFDictionarySetValue(query, kSecReturnData, kCFBooleanTrue);
    CFDictionarySetValue(query, kSecMatchLimit, kSecMatchLimitOne);
    return SecItemCopyMatching(query, (CFTypeRef *)data);
}

static OSStatus credentialWrite(CFMutableDictionaryRef query, const UInt8 *bytes, CFIndex length) {
    CFDataRef data = CFDataCreate(NULL, bytes, length);
    if (data == NULL) return errSecAllocate;
    const void *keys[] = { kSecValueData };
    const void *values[] = { data };
    CFDictionaryRef attributes = CFDictionaryCreate(NULL, keys, values, 1,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    if (attributes == NULL) {
        CFRelease(data);
        return errSecAllocate;
    }
    OSStatus status = SecItemUpdate(query, attributes);
    if (status == errSecItemNotFound) {
        CFDictionarySetValue(query, kSecValueData, data);
        status = SecItemAdd(query, NULL);
        CFDictionaryRemoveValue(query, kSecValueData);
        // 另一个进程可能在查询与新增之间创建了同一条目。
        if (status == errSecDuplicateItem) status = SecItemUpdate(query, attributes);
    }
    CFRelease(attributes);
    CFRelease(data);
    return status;
}
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

func nativeQuery(key string) (C.CFMutableDictionaryRef, error) {
	target := []byte(nativeTarget(key))
	query := C.credentialQuery((*C.char)(unsafe.Pointer(&target[0])), C.CFIndex(len(target)))
	if query == 0 {
		return 0, fmt.Errorf("create macOS Keychain query failed")
	}
	return query, nil
}

func (nativeBackend) Get(key string) ([]byte, error) {
	query, err := nativeQuery(key)
	if err != nil {
		return nil, err
	}
	defer C.CFRelease(C.CFTypeRef(query))
	var data C.CFDataRef
	status := C.credentialRead(query, &data)
	if status == C.errSecItemNotFound {
		return nil, ErrNotFound
	}
	if status != C.errSecSuccess {
		return nil, fmt.Errorf("read macOS Keychain credential: status %d", int(status))
	}
	defer C.CFRelease(C.CFTypeRef(data))
	return C.GoBytes(unsafe.Pointer(C.CFDataGetBytePtr(data)), C.int(C.CFDataGetLength(data))), nil
}

func (nativeBackend) Set(key string, value []byte) error {
	query, err := nativeQuery(key)
	if err != nil {
		return err
	}
	defer C.CFRelease(C.CFTypeRef(query))
	var valuePtr *C.UInt8
	if len(value) > 0 {
		valuePtr = (*C.UInt8)(unsafe.Pointer(&value[0]))
	}
	status := C.credentialWrite(query, valuePtr, C.CFIndex(len(value)))
	if status != C.errSecSuccess {
		return fmt.Errorf("write macOS Keychain credential: status %d", int(status))
	}
	return nil
}
func (nativeBackend) Delete(key string) error {
	query, err := nativeQuery(key)
	if err != nil {
		return err
	}
	defer C.CFRelease(C.CFTypeRef(query))
	status := C.SecItemDelete(C.CFDictionaryRef(query))
	if status == C.errSecItemNotFound {
		return ErrNotFound
	}
	if status != C.errSecSuccess {
		return fmt.Errorf("delete macOS Keychain credential: status %d", int(status))
	}
	return nil
}
