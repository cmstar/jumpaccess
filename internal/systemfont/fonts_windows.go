//go:build windows

package systemfont

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	defaultCharset = 1
	tmpfFixedPitch = 0x01 // The Windows flag is set for variable-pitch fonts.
)

type logFontW struct {
	Height         int32
	Width          int32
	Escapement     int32
	Orientation    int32
	Weight         int32
	Italic         byte
	Underline      byte
	StrikeOut      byte
	CharSet        byte
	OutPrecision   byte
	ClipPrecision  byte
	Quality        byte
	PitchAndFamily byte
	FaceName       [32]uint16
}

type textMetricW struct {
	Height           int32
	Ascent           int32
	Descent          int32
	InternalLeading  int32
	ExternalLeading  int32
	AverageCharWidth int32
	MaximumCharWidth int32
	Weight           int32
	Overhang         int32
	DigitizedAspectX int32
	DigitizedAspectY int32
	FirstChar        uint16
	LastChar         uint16
	DefaultChar      uint16
	BreakChar        uint16
	Italic           byte
	Underlined       byte
	StruckOut        byte
	PitchAndFamily   byte
	CharSet          byte
}

var (
	gdi32                   = windows.NewLazySystemDLL("gdi32.dll")
	procCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC            = gdi32.NewProc("DeleteDC")
	procEnumFontFamiliesExW = gdi32.NewProc("EnumFontFamiliesExW")
	fontEnumerationID       atomic.Uintptr
	fontEnumerations        sync.Map
	fontEnumerationCallback = windows.NewCallback(collectMonospacedFamily)
)

func collectMonospacedFamily(logFont *logFontW, textMetric *textMetricW, _ uint32, context uintptr) uintptr {
	if logFont == nil || textMetric == nil || textMetric.PitchAndFamily&tmpfFixedPitch != 0 {
		return 1
	}
	value, ok := fontEnumerations.Load(context)
	if !ok {
		return 0
	}
	name := strings.TrimSpace(windows.UTF16ToString(logFont.FaceName[:]))
	if name != "" {
		families := value.(*[]string)
		*families = append(*families, name)
	}
	return 1
}

func platformMonospacedFamilies() ([]string, error) {
	dc, _, callErr := procCreateCompatibleDC.Call(0)
	if dc == 0 {
		return nil, fmt.Errorf("create font enumeration device context: %w", callErr)
	}
	defer procDeleteDC.Call(dc)

	families := make([]string, 0, 32)
	context := fontEnumerationID.Add(1)
	if context == 0 {
		context = fontEnumerationID.Add(1)
	}
	fontEnumerations.Store(context, &families)
	defer fontEnumerations.Delete(context)

	var filter logFontW
	filter.CharSet = defaultCharset
	result, _, callErr := procEnumFontFamiliesExW.Call(
		dc,
		uintptr(unsafe.Pointer(&filter)),
		fontEnumerationCallback,
		context,
		0,
	)
	if result == 0 && len(families) == 0 && callErr != windows.ERROR_SUCCESS {
		return nil, fmt.Errorf("enumerate Windows font families: %w", callErr)
	}
	return families, nil
}
