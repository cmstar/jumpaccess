package main

import "github.com/cmstar/jumpaccess/internal/guiconfig"

type displayArea struct {
	ID               string
	X, Y             int
	Width, Height    int
	DPI              int
	Primary, Current bool
}

type windowBounds struct {
	X, Y          int
	Width, Height int
}

type windowRestorePlan struct {
	SetX, SetY int
	Normal     guiconfig.WindowPlacement
}

func resolveWindowRestore(goos string, saved guiconfig.WindowPlacement, current windowBounds, displays []displayArea) (windowRestorePlan, bool) {
	if !saved.HasBounds || len(displays) == 0 {
		return windowRestorePlan{}, false
	}
	currentDisplay, ok := currentDisplayForWindow(goos, current, displays)
	if !ok {
		return windowRestorePlan{}, false
	}

	target, targetFound := displayByID(displays, saved.Display)
	relativeX, relativeY := saved.X, saved.Y
	if saved.Display == "" {
		if goos == "windows" {
			legacyBounds := windowBounds{X: saved.X, Y: saved.Y, Width: saved.Width, Height: saved.Height}
			target, targetFound = displayContainingWindow(goos, legacyBounds, displays)
			if targetFound {
				relativeX = saved.X - target.X
				relativeY = saved.Y - target.Y
			}
		} else {
			target, targetFound = currentDisplay, true
		}
	}
	centerOnTarget := !targetFound
	if centerOnTarget {
		target, ok = primaryDisplay(displays)
		if !ok {
			target = currentDisplay
		}
	}

	normal := saved
	normal.Maximized = false
	normal.Display = target.ID
	maximumWidth, maximumHeight := displayLogicalSize(goos, target)
	normal.Width = minInt(saved.Width, maximumWidth)
	normal.Height = minInt(saved.Height, maximumHeight)
	physicalWidth, physicalHeight := windowPhysicalSize(goos, normal.Width, normal.Height, target)
	if centerOnTarget {
		relativeX, relativeY = centeredPosition(physicalWidth, physicalHeight, target)
	}
	relativeX, relativeY = clampRelativePosition(relativeX, relativeY, physicalWidth, physicalHeight, target)
	normal.X = relativeX
	normal.Y = relativeY
	absoluteX := target.X + relativeX
	absoluteY := target.Y + relativeY
	return windowRestorePlan{
		SetX:   absoluteX - currentDisplay.X,
		SetY:   absoluteY - currentDisplay.Y,
		Normal: normal,
	}, true
}

func normalizeCurrentWindowPlacement(goos string, current windowBounds, displays []displayArea) (guiconfig.WindowPlacement, bool) {
	display, ok := currentDisplayForWindow(goos, current, displays)
	if !ok {
		return guiconfig.WindowPlacement{}, false
	}
	x, y := current.X, current.Y
	if goos == "windows" {
		x -= display.X
		y -= display.Y
	}
	return guiconfig.WindowPlacement{
		HasBounds: true,
		Display:   display.ID,
		X:         x,
		Y:         y,
		Width:     current.Width,
		Height:    current.Height,
	}, true
}

func currentDisplayForWindow(goos string, bounds windowBounds, displays []displayArea) (displayArea, bool) {
	if goos != "windows" {
		for _, display := range displays {
			if display.Current {
				return display, true
			}
		}
		if display, ok := primaryDisplay(displays); ok {
			return display, true
		}
	}
	if display, ok := displayContainingWindow(goos, bounds, displays); ok {
		return display, true
	}
	return nearestDisplay(bounds, displays)
}

func displayContainingWindow(goos string, bounds windowBounds, displays []displayArea) (displayArea, bool) {
	bestArea := 0
	var best displayArea
	for _, display := range displays {
		physicalWidth, physicalHeight := windowPhysicalSize(goos, bounds.Width, bounds.Height, display)
		left := maxInt(bounds.X, display.X)
		top := maxInt(bounds.Y, display.Y)
		right := minInt(bounds.X+physicalWidth, display.X+display.Width)
		bottom := minInt(bounds.Y+physicalHeight, display.Y+display.Height)
		area := maxInt(0, right-left) * maxInt(0, bottom-top)
		if area > bestArea {
			bestArea = area
			best = display
		}
	}
	return best, bestArea > 0
}

func displayLogicalSize(goos string, display displayArea) (int, int) {
	if goos != "windows" {
		return display.Width, display.Height
	}
	dpi := displayDPI(display)
	return display.Width * 96 / dpi, display.Height * 96 / dpi
}

func windowPhysicalSize(goos string, width, height int, display displayArea) (int, int) {
	if goos != "windows" {
		return width, height
	}
	dpi := displayDPI(display)
	return width * dpi / 96, height * dpi / 96
}

func displayDPI(display displayArea) int {
	if display.DPI > 0 {
		return display.DPI
	}
	return 96
}

func nearestDisplay(bounds windowBounds, displays []displayArea) (displayArea, bool) {
	if len(displays) == 0 {
		return displayArea{}, false
	}
	cx := bounds.X + bounds.Width/2
	cy := bounds.Y + bounds.Height/2
	best := displays[0]
	bestDistance := int64(^uint64(0) >> 1)
	for _, display := range displays {
		nearestX := clampInt(cx, display.X, display.X+display.Width)
		nearestY := clampInt(cy, display.Y, display.Y+display.Height)
		dx := int64(cx - nearestX)
		dy := int64(cy - nearestY)
		distance := dx*dx + dy*dy
		if distance < bestDistance {
			bestDistance = distance
			best = display
		}
	}
	return best, true
}

func displayByID(displays []displayArea, id string) (displayArea, bool) {
	if id == "" {
		return displayArea{}, false
	}
	for _, display := range displays {
		if display.ID == id {
			return display, true
		}
	}
	return displayArea{}, false
}

func primaryDisplay(displays []displayArea) (displayArea, bool) {
	for _, display := range displays {
		if display.Primary {
			return display, true
		}
	}
	if len(displays) == 0 {
		return displayArea{}, false
	}
	return displays[0], true
}

func centeredPosition(width, height int, display displayArea) (int, int) {
	return maxInt(0, (display.Width-width)/2), maxInt(0, (display.Height-height)/2)
}

func clampRelativePosition(x, y, width, height int, display displayArea) (int, int) {
	return clampInt(x, 0, maxInt(0, display.Width-width)), clampInt(y, 0, maxInt(0, display.Height-height))
}

func clampInt(value, minimum, maximum int) int {
	return minInt(maxInt(value, minimum), maximum)
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
