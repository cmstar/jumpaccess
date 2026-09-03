//go:build windows && !bindings

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	messageBoxOK            = 0x00000000
	messageBoxIconError     = 0x00000010
	messageBoxSetForeground = 0x00010000
)

var messageBoxW = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")

func showStartupError(title, message string) {
	titleUTF16, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	messageUTF16, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return
	}
	_, _, _ = messageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(messageUTF16)),
		uintptr(unsafe.Pointer(titleUTF16)),
		messageBoxOK|messageBoxIconError|messageBoxSetForeground,
	)
}
