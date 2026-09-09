package main

import (
	"reflect"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestRefreshPointerUsesWindowMessagesWithoutMovingMouse(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var messages []pointerPoint
	defaultProc := pointerUser32.NewProc("DefWindowProcW")
	callback := windows.NewCallback(func(hwnd, message, wparam, lparam uintptr) uintptr {
		if message == 0x0200 {
			messages = append(messages, pointerPoint{int32(int16(lparam & 0xffff)), int32(int16((lparam >> 16) & 0xffff))})
			return 0
		}
		result, _, _ := defaultProc.Call(hwnd, message, wparam, lparam)
		return result
	})
	className, _ := windows.UTF16PtrFromString("JumpAccessPointerTest")
	type windowClass struct {
		Size, Style                        uint32
		Procedure                          uintptr
		ClassExtra, WindowExtra            int32
		Instance, Icon, Cursor, Background uintptr
		MenuName, ClassName                *uint16
		SmallIcon                          uintptr
	}
	class := windowClass{Procedure: callback, ClassName: className}
	class.Size = uint32(unsafe.Sizeof(class))
	atom, _, err := pointerUser32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		t.Fatal(err)
	}
	defer pointerUser32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(className)), 0)
	hwnd, _, err := pointerUser32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(className)), 0, 0x80000000, 0, 0, 10, 10, 0, 0, 0, 0)
	if hwnd == 0 {
		t.Fatal(err)
	}
	defer pointerUser32.NewProc("DestroyWindow").Call(hwnd)
	// 验证真实 EnumChildWindows 回调能找到 WebView 窗口并返回 HWND。
	childName, _ := windows.UTF16PtrFromString("Chrome_RenderWidgetHostHWND")
	childClass := windowClass{Size: class.Size, Procedure: callback, ClassName: childName}
	if atom, _, err := pointerUser32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&childClass))); atom == 0 {
		t.Fatal(err)
	}
	defer pointerUser32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(childName)), 0)
	child, _, err := pointerUser32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(childName)), 0, 0x40000000, 0, 0, 10, 10, hwnd, 0, 0, 0)
	if child == 0 {
		t.Fatal(err)
	}
	defer pointerUser32.NewProc("DestroyWindow").Call(child)
	var target uintptr
	pointerEnumChildren.Call(hwnd, pointerChildCallback, uintptr(unsafe.Pointer(&target)))
	if target != child {
		t.Fatalf("WebView target = %v, want %v", target, child)
	}
	var before, after pointerPoint
	getPosition := pointerUser32.NewProc("GetCursorPos")
	if ok, _, err := getPosition.Call(uintptr(unsafe.Pointer(&before))); ok == 0 {
		t.Fatal(err)
	}
	// 包括负坐标（鼠标可能在终端范围之外），验证原生 LPARAM 没有符号损失。
	refreshPointerAt(hwnd, pointerPoint{-12, -8})
	if want := []pointerPoint{{-11, -8}, {-12, -8}}; !reflect.DeepEqual(messages, want) {
		t.Fatalf("mouse notifications = %v, want %v", messages, want)
	}
	if ok, _, err := getPosition.Call(uintptr(unsafe.Pointer(&after))); ok == 0 {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("system pointer moved: %v -> %v", before, after)
	}
	messages = nil
	// 即使有有效的 HWND，也不允许向其他进程的窗口发送通知。
	restoreWindowPointer(hwnd, windows.GetCurrentProcessId()+1)
	if len(messages) != 0 {
		t.Fatalf("foreign window received notifications: %v", messages)
	}
}
