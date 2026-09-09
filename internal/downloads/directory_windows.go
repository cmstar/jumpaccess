package downloads

import "golang.org/x/sys/windows"

// Directory 使用 Known Folder，遵循用户对下载目录的重定向。
func Directory() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_Downloads, windows.KF_FLAG_DEFAULT)
}
