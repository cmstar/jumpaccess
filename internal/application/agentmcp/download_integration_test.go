package agentmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type downloadTestSFTP struct {
	root         string
	failArtifact func()
	closed       bool
}

func (s *downloadTestSFTP) Open(name string) (io.ReadCloser, error) {
	if strings.HasPrefix(filepath.Base(name), ".jumpaccess-download-") && s.failArtifact != nil {
		s.failArtifact()
		return nil, errors.New("synthetic SFTP disconnect")
	}
	return os.Open(filepath.Join(s.root, filepath.FromSlash(strings.TrimPrefix(name, "/"))))
}
func (s *downloadTestSFTP) Close() error { s.closed = true; return nil }
func linuxDownloadPath(t *testing.T, name string) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		return name
	}
	drive := strings.ToLower(filepath.VolumeName(name))
	if len(drive) != 2 {
		t.Fatal("test requires drive path")
	}
	return "/mnt/" + drive[:1] + filepath.ToSlash(name[2:])
}
func downloadLinuxFixture(t *testing.T) (EngineOptions, string, string) {
	t.Helper()
	distro := os.Getenv("JUMPACCESS_TEST_WSL")
	if runtime.GOOS == "windows" && distro == "" {
		t.Skip("设置 JUMPACCESS_TEST_WSL 以运行 POSIX SSH 集成测试")
	}
	// macOS 的临时目录可能经过 /var 链接；测试源路径须符合远端拒绝父目录链接的规则。
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(directory, "stage")
	os.Mkdir(stage, 0700)
	opts := poolOptions(t)
	opts.Dial = testSSH(t, func(ch ssh.Channel, requests <-chan *ssh.Request) {
		defer ch.Close()
		for request := range requests {
			if request.Type != "exec" {
				request.Reply(false, nil)
				continue
			}
			var payload struct{ Command string }
			ssh.Unmarshal(request.Payload, &payload)
			request.Reply(true, nil)
			command := "HOME=" + shellQuote(linuxDownloadPath(t, stage)) + " "
			if bin := os.Getenv("JUMPACCESS_TEST_LRZSZ_WSL_DIR"); bin != "" {
				command += "PATH=" + shellQuote(bin) + ":\"$PATH\" "
			}
			command += payload.Command
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var cmd *exec.Cmd
			if runtime.GOOS == "windows" {
				cmd = exec.CommandContext(ctx, "wsl.exe", "-d", distro, "--", "sh", "-c", command)
			} else {
				cmd = exec.CommandContext(ctx, "sh", "-c", command)
			}
			cmd.Stdout = ch
			cmd.Stderr = ch.Stderr()
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Error(err)
				return
			}
			defer stdin.Close()
			go func() { io.Copy(stdin, ch); stdin.Close() }()
			err = cmd.Run()
			code := uint32(0)
			if err != nil {
				code = 1
			}
			exitSSH(ch, code)
			return
		}
	})
	root := "${HOME}"
	opts.OpenSFTP = func(context.Context, ResolvedTarget) (DownloadSFTP, error) {
		return DownloadSFTP{Client: &downloadTestSFTP{root: stage}, Root: &root}, nil
	}
	return opts, directory, stage
}

func TestDownloadThroughSSHAndSFTPFallback(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(fmt.Sprint(fallback), func(t *testing.T) {
			opts, directory, stage := downloadLinuxFixture(t)
			source := filepath.Join(directory, "app.log")
			original := bytes.Repeat([]byte("compressible log line\n"), 40000)
			os.WriteFile(source, original, 0600)
			if fallback {
				root := "${HOME}"
				opts.OpenSFTP = func(context.Context, ResolvedTarget) (DownloadSFTP, error) {
					return DownloadSFTP{Root: &root, Client: &downloadTestSFTP{root: stage, failArtifact: func() { os.WriteFile(source, []byte("rotated"), 0600) }}}, nil
				}
			}
			e := NewEngine(opts)
			defer e.Shutdown(context.Background())
			in := DownloadInput{Target: Target{Profile: "p", Alias: "test"}, RemotePaths: []string{linuxDownloadPath(t, source)}, LocalDirectory: directory, RequestKey: requestKey(1, 620), TimeoutMS: 20000}
			job, err := e.DownloadFiles(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			wait := 25000
			job, err = e.GetDownload(context.Background(), GetDownloadInput{DownloadID: job.ID, WaitMS: &wait})
			if err != nil || job.Status != "completed" || !job.FilesReady {
				t.Fatalf("download %+v err=%v", job, err)
			}
			if len(job.Files) != 1 {
				t.Fatalf("files %+v", job.Files)
			}
			got, err := os.ReadFile(job.Files[0].Path)
			if err != nil || !bytes.Equal(got, original) {
				t.Fatalf("content mismatch %d %v", len(got), err)
			}
			if fallback && job.Protocol != "zmodem" {
				t.Fatal("did not use sz")
			}
			if !fallback && job.Protocol != "sftp" {
				t.Fatal("did not prefer SFTP")
			}
			entries, _ := os.ReadDir(stage)
			if len(entries) != 0 || len(job.RemoteArtifacts) != 0 {
				t.Fatalf("temporary residue: %+v %+v", entries, job.RemoteArtifacts)
			}
		})
	}
}

func TestDownloadRemoteCancellationConfirmed(t *testing.T) {
	opts, directory, stage := downloadLinuxFixture(t)
	source := filepath.Join(directory, "large.log")
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	file.Truncate(2 << 30)
	file.Close()
	client, err := opts.Dial(context.Background(), ResolvedTarget{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(800*time.Millisecond, cancel)
	_, owned, err := remoteDownloadCall(ctx, client, map[string]any{"op": "prepare", "paths": []string{linuxDownloadPath(t, source)}, "stage": linuxDownloadPath(t, stage), "stem": ".jumpaccess-download-cancel-test"}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel not confirmed: %v", err)
	}
	if len(owned) == 0 {
		t.Fatal("did not capture owned partial file")
	}
	cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	_, _, err = remoteDownloadCall(cleanupCtx, client, map[string]any{"op": "cleanup", "files": owned}, nil)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(stage)
	if len(entries) != 0 {
		t.Fatalf("partial files left: %+v", entries)
	}
}

func TestDownloadCancellationWithoutConfirmationIsUnknown(t *testing.T) {
	opts := poolOptions(t)
	opts.Dial = testSSH(t, func(ch ssh.Channel, requests <-chan *ssh.Request) {
		defer ch.Close()
		for req := range requests {
			req.Reply(req.Type == "exec", nil)
			if req.Type == "exec" {
				io.Copy(io.Discard, ch)
				return
			}
		}
	})
	client, err := opts.Dial(context.Background(), ResolvedTarget{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err = remoteDownloadCall(ctx, client, map[string]any{"op": "environment"}, nil)
	if !remoteUnknown(err) {
		t.Fatalf("unknown remote falsely confirmed: %v", err)
	}
}

func TestDownloadConsumerFailureDoesNotWaitForTaskDeadline(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	opts := poolOptions(t)
	opts.Dial = testSSH(t, func(ch ssh.Channel, requests <-chan *ssh.Request) {
		defer ch.Close()
		for req := range requests {
			req.Reply(req.Type == "exec", nil)
			if req.Type == "exec" {
				<-release
				return
			}
		}
	})
	client, err := opts.Dial(context.Background(), ResolvedTarget{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	started := time.Now()
	_, _, err = remoteDownloadCall(ctx, client, map[string]any{"op": "sz"}, func(io.Reader, io.Writer) error { return errors.New("local disk full") })
	if !remoteUnknown(err) || time.Since(started) > 5*time.Second {
		t.Fatalf("read failure cancellation: %v elapsed=%s", err, time.Since(started))
	}
}

func TestDownloadRemoteDirectoryAndOriginalArchive(t *testing.T) {
	opts, directory, stage := downloadLinuxFixture(t)
	source := filepath.Join(directory, "logs")
	os.MkdirAll(filepath.Join(source, "empty"), 0700)
	os.WriteFile(filepath.Join(source, "original.gz"), []byte("preserve original archive"), 0600)
	os.WriteFile(filepath.Join(source, "app.log"), []byte("hello"), 0600)
	client, err := opts.Dial(context.Background(), ResolvedTarget{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	body, owned, err := remoteDownloadCall(ctx, client, map[string]any{"op": "prepare", "paths": []string{linuxDownloadPath(t, source)}, "stage": linuxDownloadPath(t, stage), "stem": ".jumpaccess-download-directory-test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var a remoteArtifact
	if err = json.Unmarshal(body, &a); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(directory, "out")
	os.Mkdir(out, 0700)
	if _, err = restoreDownload(ctx, filepath.Join(stage, filepath.Base(a.Path)), out, a); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "logs", "original.gz"))
	if err != nil || string(got) != "preserve original archive" {
		t.Fatal("nested archive changed", err)
	}
	if _, err = os.Stat(filepath.Join(out, "logs", "empty")); err != nil {
		t.Fatal("empty directory lost", err)
	}
	_, _, err = remoteDownloadCall(ctx, client, map[string]any{"op": "cleanup", "files": owned}, nil)
	if err != nil {
		t.Fatal(err)
	}
}
