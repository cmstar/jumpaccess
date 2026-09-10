package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const maxBackgroundBytes = 20 * 1024 * 1024

func (a *desktopApp) ChooseTerminalBackground(currentPath string) (string, error) {
	options := runtime.OpenDialogOptions{Title: "选择终端背景图", Filters: []runtime.FileFilter{{DisplayName: "图片 (*.png;*.jpg;*.jpeg;*.webp;*.gif)", Pattern: "*.png;*.jpg;*.jpeg;*.webp;*.gif"}}}
	if directory := filepath.Dir(currentPath); filepath.IsAbs(directory) {
		if info, err := os.Stat(directory); err == nil && info.IsDir() {
			options.DefaultDirectory = directory
		}
	}
	return runtime.OpenFileDialog(a.context(), options)
}

func (a *desktopApp) ReadTerminalBackground(path string) (string, error) {
	return readTerminalBackground(path)
}

// 只向 WebView 返回有大小上限的本地图片，不暴露任意文件内容。
func readTerminalBackground(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("无法读取背景图，请检查文件是否存在及访问权限")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("背景图必须是普通图片文件")
	}
	if info.Size() > maxBackgroundBytes {
		return "", fmt.Errorf("背景图不能超过 20 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBackgroundBytes+1))
	if err != nil {
		return "", fmt.Errorf("读取背景图失败")
	}
	if len(data) > maxBackgroundBytes {
		return "", fmt.Errorf("背景图不能超过 20 MiB")
	}
	mime := http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
	default:
		return "", fmt.Errorf("背景图只支持 PNG、JPG、WEBP、GIF")
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
