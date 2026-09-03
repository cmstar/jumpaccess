//go:build darwin && cgo && !bindings

package main

/*
#cgo LDFLAGS: -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <CoreFoundation/CFUserNotification.h>
#include <stdlib.h>

static void JumpAccessShowStartupError(const char* titleValue, const char* messageValue) {
	CFStringRef title = CFStringCreateWithCString(
		kCFAllocatorDefault, titleValue, kCFStringEncodingUTF8);
	CFStringRef message = CFStringCreateWithCString(
		kCFAllocatorDefault, messageValue, kCFStringEncodingUTF8);
	if (title == NULL || message == NULL) {
		if (title != NULL) CFRelease(title);
		if (message != NULL) CFRelease(message);
		return;
	}
	CFOptionFlags responseFlags = 0;
	CFUserNotificationDisplayAlert(
		0,
		kCFUserNotificationStopAlertLevel,
		NULL,
		NULL,
		NULL,
		title,
		message,
		CFSTR("OK"),
		NULL,
		NULL,
		&responseFlags);
	CFRelease(title);
	CFRelease(message);
}
*/
import "C"

import "unsafe"

func showStartupError(title, message string) {
	titleValue := C.CString(title)
	defer C.free(unsafe.Pointer(titleValue))
	messageValue := C.CString(message)
	defer C.free(unsafe.Pointer(messageValue))
	C.JumpAccessShowStartupError(titleValue, messageValue)
}
