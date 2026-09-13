package main

import (
	"encoding/base64"
	"fmt"

	sshsessionapp "github.com/cmstar/jumpaccess/internal/application/sshsession"
	"github.com/cmstar/jumpaccess/internal/application/zmodemfiles"
	"github.com/cmstar/jumpaccess/internal/downloads"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *desktopApp) ProbeSSHTransferCommands(id string) sshsessionapp.TransferCapabilities {
	return a.sessions.ProbeTransferCommands(a.context(), id)
}

func (a *desktopApp) ChooseDownloadFolder(directory string) (string, error) {
	if !writableDownloadDirectory(directory) {
		directory, _ = downloads.Directory()
		if !writableDownloadDirectory(directory) {
			directory = ""
		}
	}
	restoreDialogPointer()
	defer restoreDialogPointer()
	return runtime.OpenDirectoryDialog(a.context(), runtime.OpenDialogOptions{Title: "选择自动下载目录", DefaultDirectory: directory, CanCreateDirectories: true})
}
func (a *desktopApp) WriteSSHBinary(id, data string) error { return a.sessions.WriteBinary(id, data) }
func (a *desktopApp) AcknowledgeSSHOutput(id string, sequence uint64) {
	a.sessions.AcknowledgeOutput(id, sequence)
}

func (a *desktopApp) requireActiveSSH(id string) error {
	if a.sessions != nil {
		for _, state := range a.sessions.List() {
			if state.ID == id && state.Status == sshsessionapp.StatusActive {
				return nil
			}
		}
	}
	return fmt.Errorf("SSH 连接已关闭")
}
func (a *desktopApp) ChooseZmodemUploadFiles(session string) ([]zmodemfiles.File, error) {
	if err := a.requireActiveSSH(session); err != nil {
		return nil, err
	}
	if err := a.sessions.SetTransferActive(session, true); err != nil {
		return nil, err
	}
	restoreDialogPointer()
	paths, err := runtime.OpenMultipleFilesDialog(a.context(), runtime.OpenDialogOptions{Title: "ZMODEM 上传：选择文件"})
	restoreDialogPointer()
	if err != nil {
		return nil, err
	}
	if err := a.requireActiveSSH(session); err != nil {
		return nil, err
	}
	return a.zmodemFiles.OpenUploads(session, paths)
}
func (a *desktopApp) ChooseZmodemDownloadDirectory(session string) (string, error) {
	if err := a.requireActiveSSH(session); err != nil {
		return "", err
	}
	if err := a.sessions.SetTransferActive(session, true); err != nil {
		return "", err
	}
	return chooseDownloadDirectory(a.context(), a.preferences, downloads.Directory, func(directory string) (string, error) {
		restoreDialogPointer()
		defer restoreDialogPointer()
		return runtime.OpenDirectoryDialog(a.context(), runtime.OpenDialogOptions{Title: "ZMODEM 下载：选择保存位置", DefaultDirectory: directory, CanCreateDirectories: true})
	}, func(path string) (string, error) {
		if err := a.requireActiveSSH(session); err != nil {
			return "", err
		}
		return a.zmodemFiles.GrantDirectory(session, path)
	})
}
func (a *desktopApp) CreateZmodemDownload(session, grant, name string, size int64) (zmodemfiles.File, error) {
	if err := a.requireActiveSSH(session); err != nil {
		return zmodemfiles.File{}, err
	}
	return a.zmodemFiles.CreateDownload(session, grant, name, size)
}
func (a *desktopApp) ReadZmodemFile(id string) (string, error) {
	data, err := a.zmodemFiles.Read(id)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}
func (a *desktopApp) WriteZmodemFile(id, data string) error {
	if len(data) > base64.StdEncoding.EncodedLen(zmodemfiles.ChunkSize) {
		return fmt.Errorf("文件数据块过大")
	}
	bytes, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("文件数据编码无效")
	}
	return a.zmodemFiles.Write(id, bytes)
}
func (a *desktopApp) CloseZmodemFile(id string, complete bool) error {
	return a.zmodemFiles.Close(id, complete)
}
func (a *desktopApp) EndZmodemTransfer(session string) {
	a.zmodemFiles.CloseSession(session)
	if a.sessions != nil {
		_ = a.sessions.SetTransferActive(session, false)
	}
}
