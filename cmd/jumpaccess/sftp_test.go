package main

import (
	"context"
	"testing"

	sftpsessionapp "github.com/cmstar/jumpaccess/internal/application/sftpsession"
	sshsessionapp "github.com/cmstar/jumpaccess/internal/application/sshsession"
)

func TestQuitWithTransfersRequestsConfirmationUntilUserAccepts(t *testing.T) {
	requests, quits := 0, 0
	app := &desktopApp{
		emitEvent: func(_ context.Context, event string, _ ...interface{}) {
			if event == "app:quit-requested" {
				requests++
			}
		},
		quit: func(context.Context) { quits++ },
	}
	if !app.requestQuit(true) || requests != 1 || quits != 0 {
		t.Fatal("active transfers must prevent exit and ask the UI")
	}
	if app.requestQuit(false) {
		t.Fatal("idle exit should not prompt")
	}
	app.ConfirmQuit()
	if quits != 1 || app.requestQuit(true) {
		t.Fatal("confirmed exit should proceed")
	}
}

func TestSFTPFromSSHUsesOriginalConnectionIdentity(t *testing.T) {
	request := sftpsessionapp.StartRequest{SourceSSHSessionID: "ssh-1", Profile: "changed", Organization: "changed", Target: "reassigned-alias", Account: "root", Directory: "/srv/app"}
	got, err := sftpRequestFromSSH(request, []sshsessionapp.StateEvent{{ID: "ssh-1", Status: "active", Profile: "original", Organization: "org-a", AssetID: "asset-a", Account: "account-a"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Profile != "original" || got.Organization != "org-a" || got.Target != "asset-a" || got.Account != "account-a" || got.Directory != "/srv/app" {
		t.Fatalf("SFTP did not preserve the SSH connection identity and directory snapshot: %#v", got)
	}
}

func TestSFTPFromMissingSSHDoesNotResolveChangedAlias(t *testing.T) {
	_, err := sftpRequestFromSSH(sftpsessionapp.StartRequest{SourceSSHSessionID: "gone", Target: "changed-alias"}, nil)
	if err == nil {
		t.Fatal("missing SSH source must not resolve a potentially reassigned alias")
	}
}

func TestSFTPWithoutSSHUsesSelectedTarget(t *testing.T) {
	want := sftpsessionapp.StartRequest{Profile: "profile", Target: "alias", Account: "account"}
	got, err := sftpRequestFromSSH(want, nil)
	if err != nil || got != want {
		t.Fatalf("got %#v, %v", got, err)
	}
}

func TestDesktopAcceptsNativeFileDrops(t *testing.T) {
	options := newWailsOptions(&desktopApp{})
	if options.DragAndDrop == nil || !options.DragAndDrop.EnableFileDrop || !options.DragAndDrop.DisableWebViewDrop {
		t.Fatal("SFTP uploads need native file paths without WebView navigation on drop")
	}
}
