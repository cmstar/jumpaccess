package sftpsession

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

type linkedInfo struct{ os.FileInfo }

func (i linkedInfo) Mode() os.FileMode { return i.FileInfo.Mode() | os.ModeSymlink }

type linkRemote struct {
	*localRemote
	link    string
	target  string
	removed bool
}

func (r *linkRemote) resolve(p string) string {
	if p == r.link || strings.HasPrefix(p, r.link+"/") {
		return r.target + strings.TrimPrefix(p, r.link)
	}
	return p
}
func (r *linkRemote) Lstat(p string) (os.FileInfo, error) {
	if p == r.link && !r.removed {
		info, e := r.localRemote.Lstat(r.target)
		return linkedInfo{info}, e
	}
	return r.localRemote.Lstat(r.resolve(p))
}
func (r *linkRemote) ReadDir(p string) ([]os.FileInfo, error) {
	return r.localRemote.ReadDir(r.resolve(p))
}
func (r *linkRemote) Open(p string) (io.ReadCloser, error) { return r.localRemote.Open(r.resolve(p)) }
func (r *linkRemote) Remove(p string) error {
	if p == r.link {
		r.removed = true
		return nil
	}
	return r.localRemote.Remove(r.resolve(p))
}
func (r *linkRemote) RemoveDirectory(p string) error {
	return r.localRemote.RemoveDirectory(r.resolve(p))
}

func fixtureLinkedDirectory(t *testing.T) *linkRemote {
	t.Helper()
	r := &linkRemote{localRemote: newLocalRemote(t), link: "/folder/link", target: "/outside"}
	_ = os.MkdirAll(r.local(r.link), 0700)
	_ = os.MkdirAll(r.local(r.target), 0700)
	_ = os.WriteFile(r.local(path.Join(r.target, "secret")), []byte("outside data"), 0600)
	return r
}

func TestDownloadDoesNotFollowDirectorySymlinks(t *testing.T) {
	r := fixtureLinkedDirectory(t)
	m := testManager(t, r)
	s := activeSession(t, m)
	dest := t.TempDir()
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "download", Sources: []string{"/folder"}, Destination: dest})
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "completed")
	if _, err := os.Stat(filepath.Join(dest, "folder", "link", "secret")); !os.IsNotExist(err) {
		t.Fatalf("download followed directory symlink: %v", err)
	}
}

func TestRemoveDirectorySymlinkPreservesTargetContents(t *testing.T) {
	r := fixtureLinkedDirectory(t)
	m := testManager(t, r)
	s := activeSession(t, m)
	if err := m.Remove(s.ID, []string{r.link}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(r.local("/outside/secret")); err != nil || string(b) != "outside data" {
		t.Fatalf("symlink target removed: %q, %v", b, err)
	}
	if !r.removed {
		t.Fatal("selected symlink was not removed")
	}
}
