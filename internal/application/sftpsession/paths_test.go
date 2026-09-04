package sftpsession

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestDownloadNamesRejectTraversalAndWindowsDeviceAliases(t *testing.T) {
	for _, name := range []string{"..", "../outside", "..\\outside", "NUL.txt", "CON", "COM1.log", "LPT9", "report:stream", "trailing.", "trailing ", "control\x01"} {
		if validLocalName(name) {
			t.Errorf("unsafe download name accepted: %q", name)
		}
	}
	for _, name := range []string{"report.txt", ".env", "资料.zip", "notes (1).md"} {
		if !validLocalName(name) {
			t.Errorf("valid download name rejected: %q", name)
		}
	}
}

type maliciousNameInfo struct{ os.FileInfo }

func (maliciousNameInfo) Name() string { return "..\\escaped.txt" }

type maliciousNameRemote struct{ *localRemote }

func (r *maliciousNameRemote) ReadDir(p string) ([]os.FileInfo, error) {
	if p == "/folder" {
		i, e := r.localRemote.Lstat("/data")
		return []os.FileInfo{maliciousNameInfo{i}}, e
	}
	return r.localRemote.ReadDir(p)
}
func (r *maliciousNameRemote) Lstat(p string) (os.FileInfo, error) {
	if strings.Contains(p, "escaped") {
		return r.localRemote.Lstat("/data")
	}
	return r.localRemote.Lstat(p)
}
func (r *maliciousNameRemote) Open(p string) (io.ReadCloser, error) {
	if strings.Contains(p, "escaped") {
		return r.localRemote.Open("/data")
	}
	return r.localRemote.Open(p)
}
func TestDirectoryDownloadRejectsServerNamesThatBecomeLocalTraversal(t *testing.T) {
	r := &maliciousNameRemote{newLocalRemote(t)}
	_ = r.Mkdir("/folder")
	_ = os.WriteFile(r.local("/data"), []byte("malicious filename"), 0600)
	m := testManager(t, r)
	s := activeSession(t, m)
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "download", Sources: []string{"/folder"}, Destination: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitTransfer(t, m, events[0].ID, "failed")
	if failed.Transferred != 0 {
		t.Fatalf("unsafe filename copied data: %+v", failed)
	}
}
