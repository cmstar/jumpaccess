//go:build darwin && cgo

package systemfont

/*
#cgo LDFLAGS: -framework CoreText -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <CoreText/CoreText.h>
#include <stdlib.h>

static char* JumpAccessCopyMonospacedFontFamilies(void) {
	CFArrayRef families = CTFontManagerCopyAvailableFontFamilyNames();
	if (families == NULL) {
		return NULL;
	}
	CFMutableStringRef result = CFStringCreateMutable(kCFAllocatorDefault, 0);
	if (result == NULL) {
		CFRelease(families);
		return NULL;
	}
	CFIndex count = CFArrayGetCount(families);
	for (CFIndex index = 0; index < count; index++) {
		CFStringRef family = (CFStringRef)CFArrayGetValueAtIndex(families, index);
		CTFontRef font = CTFontCreateWithName(family, 12.0, NULL);
		if (font == NULL) {
			continue;
		}
		if ((CTFontGetSymbolicTraits(font) & kCTFontTraitMonoSpace) != 0) {
			CFStringAppend(result, family);
			CFStringAppend(result, CFSTR("\n"));
		}
		CFRelease(font);
	}
	CFRelease(families);
	CFIndex capacity = CFStringGetMaximumSizeForEncoding(
		CFStringGetLength(result), kCFStringEncodingUTF8) + 1;
	char* buffer = (char*)malloc((size_t)capacity);
	if (buffer == NULL || !CFStringGetCString(result, buffer, capacity, kCFStringEncodingUTF8)) {
		free(buffer);
		buffer = NULL;
	}
	CFRelease(result);
	return buffer;
}
*/
import "C"

import (
	"fmt"
	"strings"
	"unsafe"
)

func platformMonospacedFamilies() ([]string, error) {
	value := C.JumpAccessCopyMonospacedFontFamilies()
	if value == nil {
		return nil, fmt.Errorf("enumerate macOS font families")
	}
	defer C.free(unsafe.Pointer(value))
	raw := strings.TrimSpace(C.GoString(value))
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}
