//go:build !windows && (!darwin || !cgo)

package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func nativeDisplayAreas(ctx context.Context) ([]displayArea, error) {
	screens, err := runtime.ScreenGetAll(ctx)
	if err != nil {
		return nil, err
	}
	for _, screen := range screens {
		if screen.IsCurrent || (len(screens) == 1 && screen.IsPrimary) {
			return []displayArea{{
				ID:      "current",
				Width:   screen.Size.Width,
				Height:  screen.Size.Height,
				Primary: screen.IsPrimary,
				Current: true,
			}}, nil
		}
	}
	return nil, nil
}
