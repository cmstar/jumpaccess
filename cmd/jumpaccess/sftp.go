package main

import (
	"fmt"
	sftpsessionapp "github.com/cmstar/jumpaccess/internal/application/sftpsession"
	sshsessionapp "github.com/cmstar/jumpaccess/internal/application/sshsession"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func sftpRequestFromSSH(request sftpsessionapp.StartRequest, sessions []sshsessionapp.StateEvent) (sftpsessionapp.StartRequest, error) {
	if request.SourceSSHSessionID == "" {
		return request, nil
	}
	for _, session := range sessions {
		if session.ID != request.SourceSSHSessionID {
			continue
		}
		if session.AssetID == "" || session.Account == "" {
			break
		}
		request.Profile = session.Profile
		request.Organization = session.Organization
		request.Target = session.AssetID
		request.Account = session.Account
		return request, nil
	}
	return sftpsessionapp.StartRequest{}, fmt.Errorf("原 SSH 连接已不可用，请从资产重新连接 SFTP")
}

func (a *desktopApp) StartSFTPSession(request sftpsessionapp.StartRequest) (sftpsessionapp.StateEvent, error) {
	var sessions []sshsessionapp.StateEvent
	if a.sessions != nil {
		sessions = a.sessions.List()
	}
	resolved, err := sftpRequestFromSSH(request, sessions)
	if err != nil {
		return sftpsessionapp.StateEvent{}, err
	}
	return a.sftp.Start(a.context(), resolved)
}

func (a *desktopApp) ListSFTPSessions() []sftpsessionapp.StateEvent { return a.sftp.List() }
func (a *desktopApp) CloseSFTPSession(id string) error              { return a.sftp.Close(id) }
func (a *desktopApp) ReadSFTPDirectory(id, directory string) (sftpsessionapp.Directory, error) {
	return a.sftp.ReadDirectory(id, directory)
}
func (a *desktopApp) HomeSFTPDirectory(id string) (sftpsessionapp.Directory, error) {
	directory, err := a.sftp.HomeDirectory(id)
	if err != nil {
		return sftpsessionapp.Directory{}, err
	}
	return a.sftp.ReadDirectory(id, directory)
}
func (a *desktopApp) MakeSFTPDirectory(id, directory string) error {
	return a.sftp.MakeDirectory(id, directory)
}
func (a *desktopApp) RenameSFTPEntry(id, source, newName string) error {
	return a.sftp.Rename(id, source, newName)
}
func (a *desktopApp) RemoveSFTPEntries(id string, paths []string) error {
	return a.sftp.Remove(id, paths)
}
func (a *desktopApp) ChooseSFTPUploadFiles() ([]string, error) {
	files, err := runtime.OpenMultipleFilesDialog(a.context(), runtime.OpenDialogOptions{Title: "选择上传文件"})
	if files == nil {
		files = []string{}
	}
	return files, err
}
func (a *desktopApp) ChooseSFTPUploadDirectory() (string, error) {
	return runtime.OpenDirectoryDialog(a.context(), runtime.OpenDialogOptions{Title: "选择上传文件夹"})
}
func (a *desktopApp) ChooseSFTPDownloadDirectory() (string, error) {
	return runtime.OpenDirectoryDialog(a.context(), runtime.OpenDialogOptions{Title: "选择下载位置", CanCreateDirectories: true})
}
func (a *desktopApp) StartSFTPTransfer(request sftpsessionapp.TransferRequest) ([]sftpsessionapp.TransferEvent, error) {
	return a.sftp.StartTransfer(request)
}
func (a *desktopApp) ListSFTPTransfers(sessionID string) []sftpsessionapp.TransferEvent {
	return a.sftp.ListTransfers(sessionID)
}
func (a *desktopApp) CancelSFTPTransfer(id string) error { return a.sftp.CancelTransfer(id) }
func (a *desktopApp) RetrySFTPTransfer(id string) (sftpsessionapp.TransferEvent, error) {
	return a.sftp.RetryTransfer(id)
}
func (a *desktopApp) ResolveSFTPConflict(id, choice string, applyToBatch bool) error {
	return a.sftp.ResolveConflict(id, choice, applyToBatch)
}
func (a *desktopApp) ClearCompletedSFTPTransfers(sessionID string) { a.sftp.ClearCompleted(sessionID) }

func (a *desktopApp) requestQuit(activeTransfers bool) bool {
	if !activeTransfers || a.quitConfirmed.Load() {
		return false
	}
	a.emitEvent(a.context(), "app:quit-requested")
	return true
}
func (a *desktopApp) ConfirmQuit() {
	a.quitConfirmed.Store(true)
	a.quit(a.context())
}
