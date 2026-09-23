package main

import (
	"context"
	"fmt"
	"time"

	"github.com/cmstar/jumpaccess/internal/guiconfig"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (wailsDesktopWindow) IsFullscreen(ctx context.Context) bool {
	return runtime.WindowIsFullscreen(ctx)
}
func (wailsDesktopWindow) Fullscreen(ctx context.Context)   { runtime.WindowFullscreen(ctx) }
func (wailsDesktopWindow) Unfullscreen(ctx context.Context) { runtime.WindowUnfullscreen(ctx) }
func (wailsDesktopWindow) Maximize(ctx context.Context)     { runtime.WindowMaximise(ctx) }
func (wailsDesktopWindow) Unmaximize(ctx context.Context)   { runtime.WindowUnmaximise(ctx) }

// SetWindowFullscreen 串行切换原生窗口；全屏前的边界只在内存中保存，不将全屏作为启动状态。
func (a *desktopApp) SetWindowFullscreen(enabled bool) error {
	a.fullscreenMu.Lock()
	defer a.fullscreenMu.Unlock()
	ctx := a.context()
	if enabled && a.beforeFullscreen == nil {
		saved := a.lastNormalPlacement()
		if !saved.HasBounds {
			saved = a.initialPreferences.Window
		}
		if a.window.IsNormal(ctx) {
			saved = a.captureNormalWindowPlacement(ctx)
			a.rememberLastNormal(saved)
		} else if display, ok := a.currentDisplay(ctx); ok {
			saved.Display = display.ID
		}
		saved.Maximized = a.window.IsMaximized(ctx)
		a.beforeFullscreen = &saved
	}
	if enabled != a.window.IsFullscreen(ctx) {
		if enabled {
			a.window.Fullscreen(ctx)
		} else {
			a.window.Unfullscreen(ctx)
		}
	}
	// Wails 将窗口操作投递到 UI 线程，macOS 还包含原生切换动画。
	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for a.window.IsFullscreen(ctx) != enabled {
		select {
		case <-waitCtx.Done():
			if enabled && !a.window.IsFullscreen(ctx) {
				a.beforeFullscreen = nil
			}
			return fmt.Errorf("切换全屏失败，请重试: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
	if a.goos == "darwin" {
		// styleMask 早于动画完成更新，保留切换锁直到窗口边界稳定。
		if err := a.waitForFullscreenAnimation(waitCtx); err != nil {
			return err
		}
	}
	if !enabled {
		// 原生接口恢复最大化及普通边界；再处理显示器拔出或工作区缩小。
		if saved := a.beforeFullscreen; saved != nil {
			if err := a.restoreAfterFullscreen(waitCtx, *saved); err != nil {
				return err
			}
		}
		a.beforeFullscreen = nil
	}
	return nil
}

func (a *desktopApp) restoreAfterFullscreen(ctx context.Context, saved guiconfig.WindowPlacement) error {
	if saved.Maximized && a.window.IsMaximized(ctx) {
		displays, err := a.listDisplayAreas(ctx)
		_, found := displayByID(displays, saved.Display)
		if err != nil || len(displays) == 0 || found {
			return nil
		}
		// 原显示器已拔出，先恢复可见的普通边界再在目标屏最大化。
		a.window.Unmaximize(ctx)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for !a.window.IsNormal(ctx) {
			select {
			case <-ctx.Done():
				return fmt.Errorf("恢复全屏前窗口失败: %w", ctx.Err())
			case <-ticker.C:
			}
		}
	}
	normal := saved
	normal.Maximized = false
	a.windowPlacementMu.Lock()
	a.restoreAfterMinimize = &normal
	a.windowPlacementMu.Unlock()
	a.ensureWindowVisible()
	if saved.Maximized {
		a.window.Maximize(ctx)
	}
	return nil
}

func (a *desktopApp) waitForFullscreenAnimation(ctx context.Context) error {
	started, stable := time.Now(), time.Now()
	previous := a.currentWindowBounds(ctx)
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	for time.Since(started) < 500*time.Millisecond || time.Since(stable) < 200*time.Millisecond {
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待全屏动画结束失败: %w", ctx.Err())
		case <-ticker.C:
			bounds := a.currentWindowBounds(ctx)
			if bounds != previous {
				previous = bounds
				stable = time.Now()
			}
		}
	}
	return nil
}
