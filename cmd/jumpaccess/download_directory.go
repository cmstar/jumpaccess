package main

import (
	"context"
	"fmt"
	"os"

	"github.com/cmstar/jumpaccess/internal/guiconfig"
)

// chooseDownloadDirectory 将原生对话框与目录策略分开，授权仍由活动会话检查负责。
func chooseDownloadDirectory(ctx context.Context, store guiconfig.Store, resolve func() (string, error), choose func(string) (string, error), grant func(string) (string, error)) (string, error) {
	settings, err := store.Load()
	if err != nil {
		return "", err
	}
	directory, resolveErr := resolve()
	if resolveErr != nil || !writableDownloadDirectory(directory) {
		directory = ""
	}
	if settings.Downloads.Mode == "automatic" && directory != "" {
		return grant(directory)
	}
	if settings.Downloads.Mode == "custom" && writableDownloadDirectory(settings.Downloads.Directory) {
		return grant(settings.Downloads.Directory)
	}
	if settings.Downloads.Mode == "remember" && writableDownloadDirectory(settings.Downloads.LastDirectory) {
		directory = settings.Downloads.LastDirectory
	}
	path, err := choose(directory)
	if err != nil || path == "" {
		return "", err
	}
	id, err := grant(path)
	if err != nil {
		return "", err
	}
	if err := store.Update(ctx, func(current *guiconfig.Config) error {
		current.Downloads.LastDirectory = path
		return nil
	}); err != nil {
		return "", fmt.Errorf("保存上次下载目录失败: %w", err)
	}
	return id, nil
}

func writableDownloadDirectory(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	probe, err := os.CreateTemp(path, ".jumpaccess-write-check-*")
	if err != nil {
		return false
	}
	closeErr := probe.Close()
	removeErr := os.Remove(probe.Name())
	return closeErr == nil && removeErr == nil
}
