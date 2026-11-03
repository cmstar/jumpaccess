package agentmcp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

type uploadTestSFTP struct {
	root    string
	ctx     context.Context
	payload func(io.WriteCloser) io.WriteCloser
	closed  chan struct{}
	once    sync.Once
}

func (s *uploadTestSFTP) OpenFile(name string, flags int) (io.WriteCloser, error) {
	if s.ctx != nil && s.ctx.Err() != nil {
		return nil, s.ctx.Err()
	}
	f, err := os.OpenFile(filepath.Join(s.root, filepath.FromSlash(strings.TrimPrefix(name, "/"))), flags, 0600)
	if err != nil {
		return nil, err
	}
	if filepath.Base(name) == "payload" && s.payload != nil {
		return s.payload(f), nil
	}
	return f, nil
}
func (s *uploadTestSFTP) Close() error {
	s.once.Do(func() {
		if s.closed != nil {
			close(s.closed)
		}
	})
	return nil
}

type uploadFailWriter struct {
	io.WriteCloser
	fail func()
}

func (w uploadFailWriter) Write(p []byte) (int, error) {
	n, _ := w.WriteCloser.Write(p[:len(p)/2])
	w.fail()
	return n, errors.New("synthetic SFTP disconnect")
}

type uploadBlockingWriter struct {
	io.WriteCloser
	started, closed chan struct{}
	once            sync.Once
}

func (w *uploadBlockingWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.closed
	return 0, errors.New("closed")
}

func TestUploadThroughSSHAndSFTPFallback(t *testing.T) {
	for _, mode := range []string{"gzip", "raw", "empty", "empty_archive", "directory", "fallback"} {
		t.Run(mode, func(t *testing.T) {
			opts, directory, stage := uploadLinuxFixture(t)
			local := t.TempDir()
			original := bytes.Repeat([]byte("upload config line\n"), 4000)
			if strings.HasPrefix(mode, "empty") {
				original = nil
			}
			source := filepath.Join(local, "config.txt")
			if mode == "raw" || mode == "empty_archive" {
				source = filepath.Join(local, "user.zip")
			}
			if err := os.WriteFile(source, original, 0600); err != nil {
				t.Fatal(err)
			}
			paths := []string{source}
			if mode == "directory" {
				source = filepath.Join(local, "tree")
				os.MkdirAll(filepath.Join(source, "empty"), 0700)
				os.WriteFile(filepath.Join(source, "nested.txt"), original, 0600)
				archive := filepath.Join(local, "user.zip")
				os.WriteFile(archive, []byte("user archive bytes"), 0600)
				paths = []string{source, archive}
			}
			destination := filepath.Join(directory, "destination")
			os.Mkdir(destination, 0700)
			root := "${HOME}"
			opts.OpenUploadSFTP = func(ctx context.Context, _ ResolvedTarget) (UploadSFTP, error) {
				s := &uploadTestSFTP{root: stage, ctx: ctx}
				if mode == "fallback" {
					s.payload = func(f io.WriteCloser) io.WriteCloser {
						return uploadFailWriter{f, func() { os.WriteFile(source, []byte("changed after packaging"), 0600) }}
					}
				}
				return UploadSFTP{Client: s, Root: &root}, nil
			}
			e := NewEngine(opts)
			defer e.Shutdown(context.Background())
			job, err := e.UploadFiles(context.Background(), UploadInput{Target: Target{Profile: "p", Alias: "test"}, Source: "local", LocalPaths: paths, RemoteDirectory: linuxDownloadPath(t, destination), RequestKey: requestKey(1, 720), TimeoutMS: 25000})
			if err != nil {
				t.Fatal(err)
			}
			wait := 25000
			job, err = e.GetUpload(context.Background(), GetUploadInput{UploadID: job.ID, WaitMS: &wait})
			if err != nil || job.Status != "completed" || !job.FilesReady || job.RemoteMayContinue {
				t.Fatalf("upload %+v %v", job, err)
			}
			expected := "sftp"
			if mode == "fallback" {
				expected = "zmodem"
			}
			if job.Protocol != expected || job.TransferredBytes != job.TransferBytes {
				t.Fatalf("progress %+v", job.UploadProgress)
			}
			restored := filepath.Join(destination, filepath.Base(source))
			if mode == "directory" {
				restored = filepath.Join(restored, "nested.txt")
				if info, err := os.Stat(filepath.Join(destination, "tree", "empty")); err != nil || !info.IsDir() {
					t.Fatal("empty directory lost", err)
				}
				if data, _ := os.ReadFile(filepath.Join(destination, "user.zip")); string(data) != "user archive bytes" {
					t.Fatal("user archive changed")
				}
			}
			if data, err := os.ReadFile(restored); err != nil || !bytes.Equal(data, original) {
				t.Fatalf("content mismatch %d %v", len(data), err)
			}
			if left, _ := os.ReadDir(stage); len(left) != 0 || len(job.RemoteArtifacts) != 0 {
				t.Fatalf("staging residue: %+v %+v", left, job.RemoteArtifacts)
			}
			if left, _ := os.ReadDir(destination); len(left) != len(paths) {
				t.Fatalf("restore residue: %+v", left)
			}
		})
	}
}

func TestUploadConflictAndCancellation(t *testing.T) {
	for _, conflict := range []bool{true, false} {
		t.Run(map[bool]string{true: "conflict", false: "cancel"}[conflict], func(t *testing.T) {
			opts, directory, stage := uploadLinuxFixture(t)
			source := filepath.Join(t.TempDir(), "file.txt")
			os.WriteFile(source, []byte("new data"), 0600)
			destination := filepath.Join(directory, "destination")
			os.Mkdir(destination, 0700)
			final := filepath.Join(destination, "file.txt")
			if conflict {
				os.WriteFile(final, []byte("existing data"), 0600)
			}
			started := make(chan struct{})
			s := &uploadTestSFTP{root: stage, closed: make(chan struct{})}
			s.payload = func(f io.WriteCloser) io.WriteCloser {
				return &uploadBlockingWriter{WriteCloser: f, started: started, closed: s.closed}
			}
			root := "${HOME}"
			opts.OpenUploadSFTP = func(context.Context, ResolvedTarget) (UploadSFTP, error) {
				if conflict {
					t.Error("conflict should precede data transfer")
				}
				return UploadSFTP{Client: s, Root: &root}, nil
			}
			e := NewEngine(opts)
			defer e.Shutdown(context.Background())
			zero := 0
			job, err := e.UploadFiles(context.Background(), UploadInput{Target: Target{Profile: "p", Alias: "test"}, Source: "local", LocalPaths: []string{source}, RemoteDirectory: linuxDownloadPath(t, destination), RequestKey: requestKey(1, 721), WaitMS: &zero})
			if err != nil {
				t.Fatal(err)
			}
			if !conflict {
				select {
				case <-started:
				case <-time.After(15 * time.Second):
					t.Fatal("transfer not started")
				}
				e.CancelUpload(context.Background(), UploadIDInput{UploadID: job.ID})
			}
			wait := 25000
			job, err = e.GetUpload(context.Background(), GetUploadInput{UploadID: job.ID, WaitMS: &wait})
			expected := "cancelled"
			if conflict {
				expected = "failed"
			}
			if err != nil || job.Status != expected || job.FilesReady || job.RemoteMayContinue {
				t.Fatalf("result %+v %v", job, err)
			}
			if conflict {
				if data, _ := os.ReadFile(final); string(data) != "existing data" || job.Error.Code != "TARGET_CONFLICT" {
					t.Fatal("conflict modified target", job)
				}
			} else if _, err := os.Stat(final); !os.IsNotExist(err) {
				t.Fatal("cancelled upload published a file")
			}
			if left, _ := os.ReadDir(stage); len(left) != 0 || len(job.RemoteArtifacts) != 0 {
				t.Fatalf("staging residue: %+v %+v", left, job.RemoteArtifacts)
			}
		})
	}
}

func TestUploadPermissionIsIndependentOfProtocol(t *testing.T) {
	for _, actions := range [][]jumpserver.LabelValue{nil, {{Value: "upload"}}, {}, {{Value: "download"}}} {
		allowed := actions == nil || (len(actions) > 0 && actions[0].Value == "upload")
		err := requireUploadPermission(actions)
		if (err == nil) != allowed {
			t.Fatalf("actions=%+v err=%v", actions, err)
		}
	}
}
