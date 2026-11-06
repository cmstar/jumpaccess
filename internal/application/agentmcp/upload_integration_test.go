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

	"github.com/cmstar/jumpaccess/internal/application/uploadcache"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

type uploadTestSFTP struct {
	root    string
	ctx     context.Context
	payload func(io.WriteCloser) io.WriteCloser
	closed  chan struct{}
	once    sync.Once
}

func TestStagedUploadThroughRemoteRestoreAndFallback(t *testing.T) {
	for _, mode := range []string{"sftp", "fallback", "invalid_gzip"} {
		t.Run(mode, func(t *testing.T) {
			opts, directory, stage := uploadLinuxFixture(t)
			cache, err := uploadcache.New(uploadcache.Options{Directory: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer cache.Close()
			opts.UploadCache = cache
			local := uploadSourceTestDirectory(t)
			source := filepath.Join(local, "config.txt")
			content := []byte("HTTP staged config\n")
			os.WriteFile(source, content, 0600)
			prepared, err := prepareUpload(context.Background(), []string{source}, 1<<30)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			data, err := io.ReadAll(prepared.File)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "invalid_gzip" {
				data = []byte("not a gzip")
			}
			root := "${HOME}"
			opts.OpenUploadSFTP = func(context.Context, ResolvedTarget) (UploadSFTP, error) {
				s := &uploadTestSFTP{root: stage}
				if mode == "fallback" {
					s.payload = func(f io.WriteCloser) io.WriteCloser { return uploadFailWriter{f, func() {}} }
				}
				return UploadSFTP{Root: &root, Client: s}, nil
			}
			destination := filepath.Join(directory, "destination")
			os.Mkdir(destination, 0700)
			e := NewEngine(opts)
			defer e.Shutdown(context.Background())
			receipt, err := e.PrepareUpload(context.Background(), PrepareUploadInput{Target: Target{Profile: "p", Alias: "test"}, RemoteDirectory: linuxDownloadPath(t, destination), Format: "gzip", Size: int64(len(data)), Entries: []UploadEntry{{Name: "config.txt", Size: int64(len(content))}}, RequestKey: requestKey(1, 830)})
			if err != nil {
				t.Fatal(err)
			}
			signature, _ := cache.Signature(receipt.ID)
			receiver, _ := cache.BeginReceive(receipt.ID, signature)
			if _, err = receiver.Write(data); err != nil {
				t.Fatal(err)
			}
			if err = receiver.Complete(); err != nil {
				t.Fatal(err)
			}
			os.Remove(source) // 中转完成后远端流程只依赖缓存，不再读取调用方路径。
			job, err := e.CommitUpload(context.Background(), CommitUploadInput{SourceID: receipt.ID, RequestKey: requestKey(1, 831), TimeoutMS: 25000})
			if err != nil {
				t.Fatal(err)
			}
			wait := 25000
			job, err = e.GetUpload(context.Background(), GetUploadInput{UploadID: job.ID, WaitMS: &wait})
			if err != nil {
				t.Fatal(err)
			}
			final := filepath.Join(destination, "config.txt")
			if mode == "invalid_gzip" {
				if job.Status != "failed" || job.FilesReady {
					t.Fatalf("invalid package published %+v", job)
				}
				if _, err = os.Stat(final); !os.IsNotExist(err) {
					t.Fatal("invalid package changed target")
				}
			} else {
				if job.Status != "completed" || !job.FilesReady {
					t.Fatalf("staged upload %+v", job)
				}
				if actual, _ := os.ReadFile(final); !bytes.Equal(actual, content) {
					t.Fatal("wrong final content")
				}
				if mode == "fallback" && job.Protocol != "zmodem" {
					t.Fatal("fallback not used")
				}
			}
			if _, err = cache.Get(receipt.ID); err == nil {
				t.Fatal("consumed staged file retained")
			}
			if left, _ := os.ReadDir(stage); len(left) != 0 || len(job.RemoteArtifacts) != 0 {
				t.Fatalf("remote residue %+v %+v", left, job.RemoteArtifacts)
			}
		})
	}
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
			local := uploadSourceTestDirectory(t)
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
			source := filepath.Join(uploadSourceTestDirectory(t), "file.txt")
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
