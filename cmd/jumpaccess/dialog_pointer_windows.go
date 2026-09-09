package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	pointerUser32           = windows.NewLazySystemDLL("user32.dll")
	pointerForegroundWindow = pointerUser32.NewProc("GetForegroundWindow")
	pointerWindowProcess    = pointerUser32.NewProc("GetWindowThreadProcessId")
	pointerEnumChildren     = pointerUser32.NewProc("EnumChildWindows")
	pointerClassName        = pointerUser32.NewProc("GetClassNameW")
	pointerCursorInfo       = pointerUser32.NewProc("GetCursorInfo")
	pointerScreenToClient   = pointerUser32.NewProc("ScreenToClient")
	pointerSendMessage      = pointerUser32.NewProc("SendMessageTimeoutW")
	pointerKeyState         = pointerUser32.NewProc("GetAsyncKeyState")
	pointerChildCallback    = windows.NewCallback(func(hwnd uintptr, target *uintptr) uintptr {
		var name [64]uint16
		length, _, _ := pointerClassName.Call(hwnd, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
		if length != 0 && windows.UTF16ToString(name[:]) == "Chrome_RenderWidgetHostHWND" {
			*target = hwnd
			return 0
		}
		return 1
	})
)

type pointerPoint struct{ X, Y int32 }
type pointerInfo struct {
	Size, Flags uint32
	Cursor      uintptr
	Position    pointerPoint
}

// WebView2 152 可能把输入时隐藏的指针带入原生对话框。
// 用本窗口的原生移动通知让 Chromium 自己恢复可见性，避免直接修改
// ShowCursor 计数、系统鼠标设置或实际指针位置。Wails 会清空浏览器环境参数，
// 因而不能通过 WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS 禁用该功能。
// https://github.com/MicrosoftEdge/WebView2Feedback/issues/5687
func restoreDialogPointer() {
	hwnd, _, _ := pointerForegroundWindow.Call()
	restoreWindowPointer(hwnd, windows.GetCurrentProcessId())
}

func restoreWindowPointer(hwnd uintptr, processID uint32) {
	var owner uint32
	pointerWindowProcess.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
	if hwnd == 0 || owner != processID {
		return
	}
	info := pointerInfo{Size: uint32(unsafe.Sizeof(pointerInfo{}))}
	if ok, _, _ := pointerCursorInfo.Call(uintptr(unsafe.Pointer(&info))); ok == 0 || info.Flags != 0 {
		return
	}
	// 不干预正在进行的拖动或触摸操作。
	for _, key := range []uintptr{1, 2, 4, 5, 6} {
		if state, _, _ := pointerKeyState.Call(key); state&0x8000 != 0 {
			return
		}
	}
	var target uintptr
	pointerEnumChildren.Call(hwnd, pointerChildCallback, uintptr(unsafe.Pointer(&target)))
	if target == 0 {
		return
	}
	position := info.Position
	if ok, _, _ := pointerScreenToClient.Call(target, uintptr(unsafe.Pointer(&position))); ok == 0 {
		return
	}
	refreshPointerAt(target, position)
}

func refreshPointerAt(hwnd uintptr, position pointerPoint) {
	// 先偏移一个像素再恢复通知坐标，避免同坐标通知被合并；不移动系统鼠标。
	for _, x := range []int32{position.X ^ 1, position.X} {
		coordinates := uintptr(uint32(uint16(x)) | uint32(uint16(position.Y))<<16)
		var result uintptr
		// 仅通知本应用内的 WebView；每条通知最多等待 200 ms。
		if ok, _, _ := pointerSendMessage.Call(hwnd, 0x0200, 0, coordinates, 0x0003, 200, uintptr(unsafe.Pointer(&result))); ok == 0 {
			return
		}
	}
}
