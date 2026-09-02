//go:build windows

package main

import (
	"context"
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const monitorInfoPrimary = 1

type nativeRect struct {
	Left, Top, Right, Bottom int32
}

type monitorInfo struct {
	Size    uint32
	Monitor nativeRect
	Work    nativeRect
	Flags   uint32
	Device  [32]uint16
}

type displayCollector struct {
	displays []displayArea
	err      error
}

var (
	displayEnumerationMu = sync.Mutex{}
	activeCollector      *displayCollector
	user32Display        = windows.NewLazySystemDLL("user32.dll")
	shcoreDisplay        = windows.NewLazySystemDLL("shcore.dll")
	enumDisplayMonitors  = user32Display.NewProc("EnumDisplayMonitors")
	getMonitorInfo       = user32Display.NewProc("GetMonitorInfoW")
	getDPIForMonitor     = shcoreDisplay.NewProc("GetDpiForMonitor")
	displayCallback      = windows.NewCallback(collectDisplay)
)

func collectDisplay(monitor, _ uintptr, _ *nativeRect, _ uintptr) uintptr {
	collector := activeCollector
	if collector == nil {
		return 0
	}
	info := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	result, _, callErr := getMonitorInfo.Call(monitor, uintptr(unsafe.Pointer(&info)))
	if result == 0 {
		collector.err = fmt.Errorf("get monitor info: %w", callErr)
		return 0
	}
	id := windows.UTF16ToString(info.Device[:])
	if id == "" {
		id = fmt.Sprintf("windows:%d:%d:%d:%d", info.Monitor.Left, info.Monitor.Top, info.Monitor.Right, info.Monitor.Bottom)
	}
	collector.displays = append(collector.displays, displayArea{
		ID:      id,
		X:       int(info.Work.Left),
		Y:       int(info.Work.Top),
		Width:   int(info.Work.Right - info.Work.Left),
		Height:  int(info.Work.Bottom - info.Work.Top),
		DPI:     monitorDPI(monitor),
		Primary: info.Flags&monitorInfoPrimary != 0,
	})
	return 1
}

func monitorDPI(monitor uintptr) int {
	if err := getDPIForMonitor.Find(); err != nil {
		return 96
	}
	var horizontal, vertical uint32
	result, _, _ := getDPIForMonitor.Call(
		monitor,
		0,
		uintptr(unsafe.Pointer(&horizontal)),
		uintptr(unsafe.Pointer(&vertical)),
	)
	if result != 0 || horizontal == 0 {
		return 96
	}
	return int(horizontal)
}

func nativeDisplayAreas(context.Context) ([]displayArea, error) {
	displayEnumerationMu.Lock()
	defer displayEnumerationMu.Unlock()
	collector := &displayCollector{}
	activeCollector = collector
	defer func() { activeCollector = nil }()
	result, _, callErr := enumDisplayMonitors.Call(0, 0, displayCallback, 0)
	if result == 0 {
		if collector.err != nil {
			return nil, collector.err
		}
		return nil, fmt.Errorf("enumerate display monitors: %w", callErr)
	}
	return collector.displays, collector.err
}
