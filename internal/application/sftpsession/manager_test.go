package sftpsession

import (
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"
	"time"

	connectapp "github.com/cmstar/jumpaccess/internal/application/connect"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/cmstar/jumpaccess/internal/target"
	"golang.org/x/crypto/ssh"
)

type prepareFunc func(context.Context, connectapp.Options) (connectapp.Prepared, error)

func (f prepareFunc) Prepare(ctx context.Context, o connectapp.Options) (connectapp.Prepared, error) {
	return f(ctx, o)
}

// 本地目录仅替换外部 SFTP 服务，文件操作和传输仍使用真实文件。
type localRemote struct {
	root   string
	closed chan struct{}
	once   sync.Once
}

func newLocalRemote(t *testing.T) *localRemote {
	t.Helper()
	return &localRemote{root: t.TempDir(), closed: make(chan struct{})}
}
func (r *localRemote) local(p string) string {
	return filepath.Join(r.root, filepath.FromSlash(path.Clean("/"+p)))
}
func (r *localRemote) Getwd() (string, error)            { return "/", nil }
func (r *localRemote) RealPath(p string) (string, error) { return path.Clean("/" + p), nil }
func (r *localRemote) InitialDirectory(p string) (string, error) {
	if info, err := os.Stat(r.local(p)); p != "" && err == nil && info.IsDir() {
		return path.Clean("/" + p), nil
	}
	return "/", nil
}
func (r *localRemote) ReadDir(p string) ([]os.FileInfo, error) {
	dirs, err := os.ReadDir(r.local(p))
	if err != nil {
		return nil, err
	}
	infos := make([]os.FileInfo, 0, len(dirs))
	for _, d := range dirs {
		info, e := d.Info()
		if e != nil {
			return nil, e
		}
		infos = append(infos, info)
	}
	return infos, nil
}
func (r *localRemote) Lstat(p string) (os.FileInfo, error)  { return os.Lstat(r.local(p)) }
func (r *localRemote) Open(p string) (io.ReadCloser, error) { return os.Open(r.local(p)) }
func (r *localRemote) OpenFile(p string, flags int) (io.WriteCloser, error) {
	return os.OpenFile(r.local(p), flags, 0600)
}
func (r *localRemote) Mkdir(p string) error           { return os.Mkdir(r.local(p), 0700) }
func (r *localRemote) Rename(a, b string) error       { return os.Rename(r.local(a), r.local(b)) }
func (r *localRemote) Remove(p string) error          { return os.Remove(r.local(p)) }
func (r *localRemote) RemoveDirectory(p string) error { return os.Remove(r.local(p)) }
func (r *localRemote) Wait() error                    { <-r.closed; return nil }
func (r *localRemote) Close() error                   { r.once.Do(func() { close(r.closed) }); return nil }

func testManager(t *testing.T, remote RemoteClient) *Manager {
	t.Helper()
	m := &Manager{
		Prepare: prepareFunc(func(_ context.Context, o connectapp.Options) (connectapp.Prepared, error) {
			return connectapp.Prepared{Selection: target.Selection{Profile: "canonical-profile", Organization: "org-1", Alias: "work"}, Asset: jumpserver.AssetDetail{Asset: jumpserver.Asset{ID: "asset-1", Name: "server"}}, Account: jumpserver.Account{ID: "account-1", Username: "user"}}, nil
		}),
		HostKeyCallback: func(context.Context) (ssh.HostKeyCallback, error) { return ssh.InsecureIgnoreHostKey(), nil },
		Open:            func(context.Context, OpenOptions) (RemoteClient, error) { return remote, nil },
	}
	t.Cleanup(func() { _ = m.CloseAll() })
	return m
}
func activeSession(t *testing.T, m *Manager) StateEvent {
	t.Helper()
	state, err := m.Start(context.Background(), StartRequest{Target: "work", Directory: "/work"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, s := range m.List() {
			if s.ID == state.ID && s.Status == StatusActive {
				return s
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("session did not become active")
	return StateEvent{}
}

func TestStartRetainsCanonicalConnectionIdentityAndInitialDirectory(t *testing.T) {
	r := newLocalRemote(t)
	if err := r.Mkdir("/work"); err != nil {
		t.Fatal(err)
	}
	m := testManager(t, r)
	s := activeSession(t, m)
	if s.Profile != "canonical-profile" || s.Organization != "org-1" || s.AssetID != "asset-1" || s.Account != "account-1" || s.Title != "work" || s.Directory != "/work" {
		t.Fatalf("unexpected active session: %+v", s)
	}
}

func TestReadDirectoryReturnsMetadataAndUpdatesOnlySFTPDirectory(t *testing.T) {
	r := newLocalRemote(t)
	if err := r.Mkdir("/files"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.local("/files/report.txt"), []byte("report"), 0600); err != nil {
		t.Fatal(err)
	}
	m := testManager(t, r)
	s := activeSession(t, m)
	d, err := m.ReadDirectory(s.ID, "/files")
	if err != nil {
		t.Fatal(err)
	}
	if d.Path != "/files" || len(d.Entries) != 1 || d.Entries[0].Name != "report.txt" || d.Entries[0].Path != "/files/report.txt" || d.Entries[0].Size != 6 || d.Entries[0].Type != "file" || d.Entries[0].ModifiedAt == "" {
		t.Fatalf("directory: %+v", d)
	}
	if m.List()[0].Directory != "/files" {
		t.Fatal("SFTP directory was not updated")
	}
}

func TestMakeDirectoryCreatesInsideCurrentDirectory(t *testing.T) {
	r := newLocalRemote(t)
	m := testManager(t, r)
	s := activeSession(t, m)
	if err := m.MakeDirectory(s.ID, "new-folder"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(r.local("/new-folder"))
	if err != nil || !info.IsDir() {
		t.Fatalf("folder missing: %v", err)
	}
}

func TestRenameMovesEntryWithoutOverwritingAnother(t *testing.T) {
	r := newLocalRemote(t)
	_ = os.WriteFile(r.local("/old.txt"), []byte("old"), 0600)
	_ = os.WriteFile(r.local("/existing.txt"), []byte("keep"), 0600)
	m := testManager(t, r)
	s := activeSession(t, m)
	if err := m.Rename(s.ID, "/old.txt", "existing.txt"); err == nil {
		t.Fatal("overwriting an existing name must be rejected")
	}
	if err := m.Rename(s.ID, "/old.txt", "new.txt"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(r.local("/new.txt")); err != nil || string(b) != "old" {
		t.Fatalf("renamed content = %q, %v", b, err)
	}
}

func TestRemoveRecursivelyDeletesSelectedDirectory(t *testing.T) {
	r := newLocalRemote(t)
	_ = os.MkdirAll(r.local("/old/nested"), 0700)
	_ = os.WriteFile(r.local("/old/nested/file"), []byte("data"), 0600)
	_ = os.WriteFile(r.local("/keep"), []byte("keep"), 0600)
	m := testManager(t, r)
	s := activeSession(t, m)
	if err := m.Remove(s.ID, []string{"/old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.local("/old")); !os.IsNotExist(err) {
		t.Fatalf("selected folder still exists: %v", err)
	}
	if _, err := os.Stat(r.local("/keep")); err != nil {
		t.Fatal(err)
	}
}

func TestHomeDirectoryUsesServerInitialFallback(t *testing.T) {
	r := newLocalRemote(t)
	m := testManager(t, r)
	s := activeSession(t, m)
	home, err := m.HomeDirectory(s.ID)
	if err != nil || home != "/" {
		t.Fatalf("home = %q, %v", home, err)
	}
}

func TestStartRequestsSFTPConnectionToken(t *testing.T) {
	r := newLocalRemote(t)
	m := testManager(t, r)
	base := m.Prepare
	protocol := make(chan string, 1)
	m.Prepare = prepareFunc(func(ctx context.Context, o connectapp.Options) (connectapp.Prepared, error) {
		protocol <- o.Protocol
		return base.Prepare(ctx, o)
	})
	activeSession(t, m)
	if got := <-protocol; got != "sftp" {
		t.Fatalf("requested protocol=%q, want sftp", got)
	}
}

func TestKnownAccountActionsPreventUnauthorizedMutation(t *testing.T) {
	r := newLocalRemote(t)
	m := testManager(t, r)
	base := m.Prepare
	m.Prepare = prepareFunc(func(ctx context.Context, o connectapp.Options) (connectapp.Prepared, error) {
		p, e := base.Prepare(ctx, o)
		p.Account.Actions = []jumpserver.LabelValue{{Value: "download"}}
		return p, e
	})
	s := activeSession(t, m)
	if err := m.MakeDirectory(s.ID, "/forbidden"); err == nil {
		t.Fatal("known denied upload action allowed mkdir")
	}
	if s.Permissions == nil || s.Permissions.Upload == nil || *s.Permissions.Upload || s.Permissions.Download == nil || !*s.Permissions.Download {
		t.Fatalf("permissions not exposed: %+v", s.Permissions)
	}
}
