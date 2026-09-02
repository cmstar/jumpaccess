//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework AppKit
#include <stdint.h>

int JumpAccessDisplayCount(void);
int JumpAccessDisplayAt(int index, uint32_t *displayID, int *x, int *y, int *width, int *height, int *primary, int *current);
*/
import "C"

import (
	"context"
	"fmt"
)

func nativeDisplayAreas(context.Context) ([]displayArea, error) {
	count := int(C.JumpAccessDisplayCount())
	displays := make([]displayArea, 0, count)
	for index := 0; index < count; index++ {
		var displayID C.uint32_t
		var x, y, width, height, primary, current C.int
		if C.JumpAccessDisplayAt(C.int(index), &displayID, &x, &y, &width, &height, &primary, &current) == 0 {
			return nil, fmt.Errorf("display topology changed while enumerating screens")
		}
		displays = append(displays, displayArea{
			ID:      fmt.Sprintf("mac:%d", uint32(displayID)),
			X:       int(x),
			Y:       int(y),
			Width:   int(width),
			Height:  int(height),
			Primary: primary != 0,
			Current: current != 0,
		})
	}
	return displays, nil
}
