package sftpclient

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func TestSFTPConnectionTransfersFilesThroughSubsystemWithoutShell(t *testing.T) {
	opts := startSFTPServer(t, false)
	client, err := Open(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Mkdir("/uploads"); err != nil {
		t.Fatal(err)
	}
	file, err := client.OpenFile("/uploads/incoming", os.O_CREATE|os.O_WRONLY|os.O_EXCL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(file, "hello SFTP"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Rename("/uploads/incoming", "/uploads/ready"); err != nil {
		t.Fatal(err)
	}
	entries, err := client.ReadDir("/uploads")
	if err != nil || len(entries) != 1 || entries[0].Name() != "ready" {
		t.Fatalf("entries=%v error=%v", entries, err)
	}
	r, err := client.Open("/uploads/ready")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(data) != "hello SFTP" {
		t.Fatalf("read=%q error=%v", data, err)
	}
	if err := client.Remove("/uploads/ready"); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveDirectory("/uploads"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Lstat("/uploads"); !os.IsNotExist(err) {
		t.Fatalf("removed directory still exists: %v", err)
	}
}

func TestSFTPCancelClosesActiveConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := Open(ctx, startSFTPServer(t, false))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	done := make(chan error, 1)
	go func() { done <- client.Wait() }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SFTP Wait did not unblock after cancellation")
	}
	if _, err := client.ReadDir("/"); err == nil {
		t.Fatal("SFTP remained usable after cancellation")
	}
}

func TestSFTPCancelInterruptsSubsystemNegotiation(t *testing.T) {
	options := startSFTPServer(t, true)
	options.Timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		client, err := Open(ctx, options)
		if client != nil {
			client.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("subsystem negotiation ignored cancellation")
	}
}

func TestSFTPRejectsSSHCredentialAndUntrustedHost(t *testing.T) {
	options := startSFTPServer(t, false)
	options.Connection.Protocol = "ssh"
	if client, err := Open(context.Background(), options); err == nil {
		client.Close()
		t.Fatal("SFTP accepted SSH token")
	}
	options = startSFTPServer(t, false)
	options.HostKeyCallback = func(string, net.Addr, ssh.PublicKey) error { return errors.New("untrusted host") }
	if client, err := Open(context.Background(), options); err == nil {
		client.Close()
		t.Fatal("SFTP ignored host key refusal")
	}
}

func TestInitialDirectoryMapsPhysicalSSHDirectoryWithinKnownRoot(t *testing.T) {
	options := startSFTPServer(t, false)
	root := "/srv/${ACCOUNT}"
	options.RootPath = &root
	options.AccountUsername = "ubuntu"
	client, err := Open(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Mkdir("/project"); err != nil {
		t.Fatal(err)
	}
	directory, err := client.InitialDirectory("/srv/ubuntu/project")
	if err != nil || directory != "/project" {
		t.Fatalf("directory=%q error=%v", directory, err)
	}
}

func TestInitialDirectorySilentlyFallsBackWithoutGuessingPhysicalPath(t *testing.T) {
	for _, tc := range []struct{ name, root, cwd, home, want string }{
		{"same virtual spelling is not physical cwd", "/tmp", "/home/u", "", "/"},
		{"root prefix must end on segment", "/srv/u", "/srv/user/project", "", "/"},
		{"missing cwd uses known home", "/", "/missing", "/home/u", "/home/u"},
		{"asset entry uses known home", "/", "", "/home/u", "/home/u"},
		{"inaccessible home uses start", "/", "", "/missing", "/"},
		{"home root uses virtual root", "home", "/home/u", "", "/"},
		{"unknown home variable uses start", "${HOME}/uploads", "/home/u", "", "/"},
		{"unknown user variable uses start", "/srv/${USER}", "/home/u", "", "/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := startSFTPServer(t, false)
			options.RootPath = &tc.root
			options.HomeDirectory = tc.home
			client, err := Open(context.Background(), options)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			for _, directory := range []string{"/home", "/home/u", "/srv", "/srv/user", "/srv/user/project"} {
				if err := client.Mkdir(directory); err != nil {
					t.Fatal(err)
				}
			}
			directory, err := client.InitialDirectory(tc.cwd)
			if err != nil || directory != tc.want {
				t.Fatalf("directory=%q error=%v want=%q", directory, err, tc.want)
			}
		})
	}
}

func TestAbortingBlockedFileReadKeepsOtherSFTPOperationsAvailable(t *testing.T) {
	handlers := sftp.InMemHandler()
	started := make(chan struct{}, 1)
	handlers.FileGet = blockedFileReader{started: started}
	client, err := Open(context.Background(), startSFTPServerWithHandlers(t, false, handlers))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	reader, err := client.Open("/blocked")
	if err != nil {
		t.Fatal(err)
	}
	aborter, ok := reader.(interface{ Abort() error })
	if !ok {
		reader.Close()
		t.Fatal("file stream cannot abort an in-flight read")
	}
	done := make(chan error, 1)
	go func() { _, err := reader.Read(make([]byte, 32)); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("file read did not reach server")
	}
	if err := aborter.Abort(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("aborted read succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("file read was not interrupted")
	}
	if _, err := client.ReadDir("/"); err != nil {
		t.Fatalf("aborting a file closed the SFTP tab: %v", err)
	}
}

func TestPosixRenamePublishesReplacementFile(t *testing.T) {
	client, err := Open(context.Background(), startSFTPServer(t, false))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for name, contents := range map[string]string{"/old": "old contents", "/temporary": "new contents"} {
		file, err := client.OpenFile(name, os.O_CREATE|os.O_WRONLY)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(file, contents); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.PosixRename("/temporary", "/old"); err != nil {
		t.Fatal(err)
	}
	reader, err := client.Open("/old")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	contents, err := io.ReadAll(reader)
	if err != nil || string(contents) != "new contents" {
		t.Fatalf("replacement=%q error=%v", contents, err)
	}
	if _, err := client.Lstat("/temporary"); !os.IsNotExist(err) {
		t.Fatalf("temporary path remains: %v", err)
	}
}

func TestInitialDirectoryWithoutRootMetadataUsesServerStart(t *testing.T) {
	client, err := Open(context.Background(), startSFTPServer(t, false))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Mkdir("/same-name"); err != nil {
		t.Fatal(err)
	}
	directory, err := client.InitialDirectory("/same-name")
	if err != nil || directory != "/" {
		t.Fatalf("directory=%q error=%v", directory, err)
	}
}

func TestFileOpenTimeoutDoesNotWaitForeverForSSHChannel(t *testing.T) {
	options := startSFTPServerConfigured(t, false, sftp.InMemHandler(), true)
	options.Timeout = 100 * time.Millisecond
	client, err := Open(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		reader, err := client.Open("/file")
		if err == nil {
			reader.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("file open ignored its timeout while waiting for a channel")
	}
	if _, err := client.ReadDir("/"); err != nil {
		t.Fatalf("file-open timeout closed the tab: %v", err)
	}
}

func TestFailedFileOpenReturnsNilStream(t *testing.T) {
	client, err := Open(context.Background(), startSFTPServer(t, false))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	reader, err := client.Open("/does-not-exist")
	if err == nil || reader != nil {
		t.Fatalf("failed Open returned non-nil stream or no error: stream=%v error=%v", reader, err)
	}
	writer, err := client.OpenFile("/missing/file", os.O_WRONLY)
	if err == nil || writer != nil {
		t.Fatalf("failed OpenFile returned non-nil stream or no error: stream=%v error=%v", writer, err)
	}
}

func TestLstatRecognizesKoKoLinksWithoutFollowingTheirTargets(t *testing.T) {
	handlers := sftp.InMemHandler()
	if err := handlers.FileCmd.Filecmd(sftp.NewRequest("Mkdir", "/target")); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"/link": "/target", "/dangling": "/missing"} {
		request := sftp.NewRequest("Symlink", target)
		request.Target = name
		if err := handlers.FileCmd.Filecmd(request); err != nil {
			t.Fatal(err)
		}
	}
	handlers.FileList = koKoFileLister{FileLister: handlers.FileList}
	client, err := Open(context.Background(), startSFTPServerWithHandlers(t, false, handlers))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for _, name := range []string{"/link", "/dangling"} {
		info, err := client.Lstat(name)
		if err != nil {
			t.Errorf("Lstat(%q): %v", name, err)
			continue
		}
		if info.IsDir() || info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("Lstat(%q) followed link: mode=%v", name, info.Mode())
		}
	}
	info, err := client.Lstat("/target")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("ordinary directory changed type: info=%v error=%v", info, err)
	}
}

func TestLstatRefusesIndeterminateLinkType(t *testing.T) {
	for _, readlinkErr := range []error{os.ErrPermission, sftp.ErrSSHFxOpUnsupported, errors.New("unexpected server failure"), errors.New(`sftp: "unsupported" (SSH_FX_OP_UNSUPPORTED)`)} {
		t.Run(readlinkErr.Error(), func(t *testing.T) {
			handlers := sftp.InMemHandler()
			if err := handlers.FileCmd.Filecmd(sftp.NewRequest("Mkdir", "/target")); err != nil {
				t.Fatal(err)
			}
			handlers.FileList = koKoFileLister{FileLister: handlers.FileList, readlinkErr: readlinkErr}
			client, err := Open(context.Background(), startSFTPServerWithHandlers(t, false, handlers))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			if info, err := client.Lstat("/target"); err == nil || info != nil {
				t.Fatalf("unknown link type accepted: info=%v error=%v", info, err)
			}
		})
	}
}

// KoKo 没有实现 LstatFileLister，其 Lstat 请求会回退到跟随链接的 Stat。
type koKoFileLister struct {
	sftp.FileLister
	readlinkErr error
}

func TestMetadataDeadlineKeepsSFTPSessionUsable(t *testing.T) {
	handlers := sftp.InMemHandler()
	started := make(chan struct{}, 1)
	handlers.FileList = &stalledFileLister{FileLister: handlers.FileList, started: started}
	options := startSFTPServerWithHandlers(t, false, handlers)
	options.Timeout = 100 * time.Millisecond
	client, err := Open(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	done := make(chan error, 1)
	go func() { _, err := client.ReadDir("/blocked"); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("directory request did not reach server")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("directory timeout error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("directory request ignored its timeout")
	}
	if _, err := client.ReadDir("/"); err != nil {
		t.Fatalf("metadata timeout closed the SFTP tab: %v", err)
	}
}

func TestFileSubsystemDrainsStderrBeyondSSHWindow(t *testing.T) {
	options := startSFTPServerConfigured(t, false, sftp.InMemHandler(), false, 3*1024*1024)
	options.Timeout = 500 * time.Millisecond
	client, err := Open(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	file, err := client.OpenFile("/file", os.O_CREATE|os.O_WRONLY)
	if err != nil {
		t.Fatalf("server stderr blocked the file subsystem: %v", err)
	}
	defer file.Close()
	if _, err := io.WriteString(file, "still writable"); err != nil {
		t.Fatal(err)
	}
}

type stalledFileLister struct {
	sftp.FileLister
	started chan struct{}
	blocked atomic.Bool
}

func (s *stalledFileLister) Filelist(request *sftp.Request) (sftp.ListerAt, error) {
	if request.Method == "List" && request.Filepath == "/blocked" && !s.blocked.Swap(true) {
		s.started <- struct{}{}
		<-request.Context().Done()
		return nil, request.Context().Err()
	}
	return s.FileLister.Filelist(request)
}

func (k koKoFileLister) Readlink(name string) (string, error) {
	if k.readlinkErr != nil {
		return "", k.readlinkErr
	}
	return k.FileLister.(sftp.ReadlinkFileLister).Readlink(name)
}

type blockedFileReader struct{ started chan struct{} }

func (b blockedFileReader) Fileread(request *sftp.Request) (io.ReaderAt, error) {
	return blockedReaderAt{ctx: request.Context(), started: b.started}, nil
}

type blockedReaderAt struct {
	ctx     context.Context
	started chan struct{}
}

func (b blockedReaderAt) ReadAt([]byte, int64) (int, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

// 仅使用隔离的内存文件系统，服务端拒绝所有 PTY 与 shell 请求。
func startSFTPServer(t *testing.T, stallSubsystem bool) OpenOptions {
	return startSFTPServerWithHandlers(t, stallSubsystem, sftp.InMemHandler())
}

func startSFTPServerWithHandlers(t *testing.T, stallSubsystem bool, handlers sftp.Handlers) OpenOptions {
	return startSFTPServerConfigured(t, stallSubsystem, handlers, false)
}

func startSFTPServerConfigured(t *testing.T, stallSubsystem bool, handlers sftp.Handlers, stallFileChannel bool, stderrBytes ...int) OpenOptions {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	configuration := &ssh.ServerConfig{PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if meta.User() != "JMS-sftp-test" || string(password) != "test-password" {
			return nil, fmt.Errorf("unexpected gateway credentials")
		}
		return nil, nil
	}}
	configuration.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		raw, err := listener.Accept()
		if err != nil {
			return
		}
		defer raw.Close()
		t.Cleanup(func() { raw.Close() })
		conn, channels, requests, err := ssh.NewServerConn(raw, configuration)
		if err != nil {
			return
		}
		defer conn.Close()
		go ssh.DiscardRequests(requests)
		channelCount := 0
		for incoming := range channels {
			channelCount++
			if stallFileChannel && channelCount == 2 {
				continue
			}
			if incoming.ChannelType() != "session" {
				incoming.Reject(ssh.UnknownChannelType, "session required")
				continue
			}
			channel, reqs, err := incoming.Accept()
			if err != nil {
				return
			}
			sendStderr := len(stderrBytes) > 0 && channelCount > 1
			go func() {
				defer channel.Close()
				for req := range reqs {
					var subsystem struct{ Name string }
					if req.Type != "subsystem" || ssh.Unmarshal(req.Payload, &subsystem) != nil || subsystem.Name != "sftp" {
						req.Reply(false, nil)
						continue
					}
					if stallSubsystem {
						io.Copy(io.Discard, channel)
						return
					}
					req.Reply(true, nil)
					go ssh.DiscardRequests(reqs)
					if sendStderr {
						if _, err := channel.Stderr().Write(bytes.Repeat([]byte("x"), stderrBytes[0])); err != nil {
							return
						}
					}
					server := sftp.NewRequestServer(channel, handlers)
					defer server.Close()
					server.Serve()
					return
				}
			}()
		}
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	return OpenOptions{Connection: jumpserver.ClientConnection{Protocol: "sftp", Endpoint: jumpserver.Endpoint{Host: host, Port: port}, Token: jumpserver.ConnectionCredential{ID: "sftp-test", Value: "test-password"}}, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if !bytes.Equal(key.Marshal(), signer.PublicKey().Marshal()) {
			return fmt.Errorf("wrong gateway host key")
		}
		return nil
	}, Timeout: time.Second}
}
