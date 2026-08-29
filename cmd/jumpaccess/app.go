package main

import "context"

// desktopApp 是 Wails 表现层入口。共享应用服务会在后续步骤注入此处。
type desktopApp struct {
	ctx context.Context
}

func newDesktopApp() *desktopApp {
	return &desktopApp{}
}

func (a *desktopApp) startup(ctx context.Context) {
	a.ctx = ctx
}
