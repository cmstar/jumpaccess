//go:build bindings

package main

import "github.com/cmstar/jumpaccess/internal/guiconfig"

// Wails 在生成 bindings 时会编译并执行带 bindings tag 的临时程序。
// 该过程只需要反射绑定方法，不能读取构建机上的用户配置或初始化运行时依赖。
func newDesktopAppForRun() (*desktopApp, error) {
	return &desktopApp{initialPreferences: guiconfig.Default()}, nil
}
