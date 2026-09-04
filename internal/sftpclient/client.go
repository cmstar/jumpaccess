package sftpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/cmstar/jumpaccess/internal/sshupstream"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type OpenOptions struct {
	Connection         jumpserver.ClientConnection
	HostKeyCallback    ssh.HostKeyCallback
	Timeout            time.Duration
	RootPath           *string
	AccountUsername    string
	HomeDirectory      string
	JumpServerUsername string
}

type Client struct {
	ctx              context.Context
	remote           *sftp.Client
	transport        *ssh.Client
	options          OpenOptions
	closeOnce        sync.Once
	stopCancellation func() bool
}

func Open(ctx context.Context, options OpenOptions) (*Client, error) {
	if options.Connection.Protocol != "sftp" {
		return nil, fmt.Errorf("SFTP client received protocol %q", options.Connection.Protocol)
	}
	openCtx := ctx
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		openCtx, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}
	transport, err := (sshupstream.Dialer{HostKeyCallback: options.HostKeyCallback, Timeout: options.Timeout}).Dial(openCtx, options.Connection)
	if err != nil {
		return nil, err
	}
	stopOpening := context.AfterFunc(openCtx, func() { transport.Close() })
	remote, err := sftp.NewClient(transport)
	stopOpening()
	if openCtx.Err() != nil {
		transport.Close()
		return nil, openCtx.Err()
	}
	if err != nil {
		transport.Close()
		return nil, err
	}
	options.Connection = jumpserver.ClientConnection{}
	client := &Client{ctx: ctx, remote: remote, transport: transport, options: options}
	client.stopCancellation = context.AfterFunc(ctx, func() { transport.Close() })
	go func() { remote.Wait(); client.Close() }()
	return client, nil
}
func (c *Client) Getwd() (string, error) {
	return metadata(c, func(remote *sftp.Client) (string, error) { return remote.Getwd() })
}
func (c *Client) RealPath(name string) (string, error) {
	return metadata(c, func(remote *sftp.Client) (string, error) { return remote.RealPath(name) })
}
func (c *Client) ReadDir(name string) ([]os.FileInfo, error) {
	return metadata(c, func(remote *sftp.Client) ([]os.FileInfo, error) { return remote.ReadDir(name) })
}
func (c *Client) Lstat(name string) (os.FileInfo, error) {
	return metadata(c, func(remote *sftp.Client) (os.FileInfo, error) { return lstat(remote, name) })
}

func lstat(remote *sftp.Client, name string) (os.FileInfo, error) {
	name = path.Clean(name)
	// KoKo 的虚拟根是已配置的浏览边界，本身不支持 READLINK。
	if name == "/" {
		return remote.Lstat(name)
	}
	// KoKo 的 Lstat 会退化为 Stat；先 READLINK，避免递归操作进入链接目标。
	target, err := remote.ReadLink(name)
	if err == nil {
		return linkInfo{name: path.Base(name), size: int64(len(target))}, nil
	}
	var status *sftp.StatusError
	if !errors.As(err, &status) || status.FxCode() != sftp.ErrSSHFxFailure || !readlinkNotLink(status) {
		return nil, err
	}
	// OpenSSH 将普通文件的 READLINK EINVAL 编码为 SSH_FX_FAILURE。
	// 权限拒绝、未实现和断线不能据此判定为普通文件，已在上方返回错误。
	return remote.Lstat(name)
}

func readlinkNotLink(status *sftp.StatusError) bool {
	message := status.Error()
	// pkg/sftp 未公开 status message；KoKo 最多再包装一层上游 status。
	for depth := 0; depth < 2; depth++ {
		const prefix, suffix = "sftp: ", " (SSH_FX_FAILURE)"
		if !strings.HasPrefix(message, prefix) || !strings.HasSuffix(message, suffix) {
			break
		}
		decoded, err := strconv.Unquote(strings.TrimSuffix(strings.TrimPrefix(message, prefix), suffix))
		if err != nil {
			return false
		}
		message = decoded
	}
	// SFTP v3 的无说明 Failure 也可能表示其他故障，协议无法完全消除这项歧义。
	return message == "invalid argument" || message == "Failure"
}

type linkInfo struct {
	name string
	size int64
}

func (l linkInfo) Name() string     { return l.name }
func (l linkInfo) Size() int64      { return l.size }
func (linkInfo) Mode() os.FileMode  { return os.ModeSymlink | 0o777 }
func (linkInfo) ModTime() time.Time { return time.Time{} }
func (linkInfo) IsDir() bool        { return false }
func (linkInfo) Sys() any           { return nil }
func (c *Client) Open(path string) (io.ReadCloser, error) {
	file, err := c.openFile(path, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	return file, nil
}
func (c *Client) OpenFile(path string, flags int) (io.WriteCloser, error) {
	file, err := c.openFile(path, flags)
	if err != nil {
		return nil, err
	}
	return file, nil
}
func (c *Client) Mkdir(name string) error {
	return metadataCommand(c, func(remote *sftp.Client) error { return remote.Mkdir(name) })
}
func (c *Client) Rename(oldPath, newPath string) error {
	return metadataCommand(c, func(remote *sftp.Client) error { return remote.Rename(oldPath, newPath) })
}
func (c *Client) PosixRename(oldPath, newPath string) error {
	return metadataCommand(c, func(remote *sftp.Client) error { return remote.PosixRename(oldPath, newPath) })
}
func (c *Client) Remove(name string) error {
	return metadataCommand(c, func(remote *sftp.Client) error { return remote.Remove(name) })
}
func (c *Client) RemoveDirectory(name string) error {
	return metadataCommand(c, func(remote *sftp.Client) error { return remote.RemoveDirectory(name) })
}
func (c *Client) Close() error {
	c.closeOnce.Do(func() { c.stopCancellation(); c.transport.Close(); c.remote.Close() })
	return nil
}
func (c *Client) Wait() error { return c.remote.Wait() }
func (c *Client) InitialDirectory(cwd string) (string, error) {
	var candidates []string
	if root, ok := c.physicalRoot(); ok {
		for _, physical := range []string{cwd, c.options.HomeDirectory} {
			if directory, ok := relativeToRoot(root, physical); ok {
				candidates = append(candidates, directory)
			}
		}
	}
	start, err := c.Getwd()
	if err == nil {
		candidates = append(candidates, start)
	}
	candidates = append(candidates, "/")
	seen := map[string]bool{}
	for _, directory := range candidates {
		if seen[directory] {
			continue
		}
		seen[directory] = true
		if _, readErr := c.ReadDir(directory); readErr == nil {
			return directory, nil
		} else {
			err = readErr
		}
	}
	return "", fmt.Errorf("open SFTP initial directory: %w", err)
}

// KoKo 的 SFTP '/' 映射为平台配置的根目录；必须先确认映射，再使用 SSH 的物理目录。
func (c *Client) physicalRoot() (string, bool) {
	if c.options.RootPath == nil {
		return "", false
	}
	root := *c.options.RootPath
	switch strings.ToLower(root) {
	case "home", "~", "":
		root = c.options.HomeDirectory
		if root == "" {
			return "", false
		}
	}
	for variable, value := range map[string]string{"${ACCOUNT}": c.options.AccountUsername, "${HOME}": c.options.HomeDirectory, "${USER}": c.options.JumpServerUsername} {
		if strings.Contains(root, variable) {
			if value == "" {
				return "", false
			}
			root = strings.ReplaceAll(root, variable, value)
		}
	}
	if strings.Contains(root, "${") || strings.ContainsRune(root, 0) {
		return "", false
	}
	return path.Clean("/" + strings.TrimPrefix(root, "/")), true
}

func relativeToRoot(root, physical string) (string, bool) {
	if !path.IsAbs(physical) || strings.ContainsRune(physical, 0) {
		return "", false
	}
	physical = path.Clean(physical)
	if physical == root {
		return "/", true
	}
	prefix := strings.TrimSuffix(root, "/") + "/"
	if !strings.HasPrefix(physical, prefix) {
		return "", false
	}
	return "/" + strings.TrimPrefix(physical, prefix), true
}

// 每次目录操作使用可单独关闭的 subsystem；超时不会中断同 Tab 的文件传输。
func metadata[T any](c *Client, operation func(*sftp.Client) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(c.ctx, c.operationTimeout())
	defer cancel()
	remote, session, err := c.newSubsystem(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	defer func() { session.Close(); remote.Close() }()
	stop := context.AfterFunc(ctx, func() { session.Close() })
	defer stop()
	result, err := operation(remote)
	if ctx.Err() != nil {
		var zero T
		return zero, ctx.Err()
	}
	return result, err
}

func metadataCommand(c *Client, command func(*sftp.Client) error) error {
	_, err := metadata(c, func(remote *sftp.Client) (struct{}, error) { return struct{}{}, command(remote) })
	return err
}

func (c *Client) operationTimeout() time.Duration {
	if c.options.Timeout > 0 {
		return c.options.Timeout
	}
	return 30 * time.Second
}

// 每个传输使用独立 subsystem，使 Abort 能中断阻塞 I/O 而不影响目录浏览或其他文件。
func (c *Client) openFile(path string, flags int) (*fileStream, error) {
	ctx, cancel := context.WithTimeout(c.ctx, c.operationTimeout())
	defer cancel()
	remote, session, err := c.newSubsystem(ctx)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { session.Close() })
	defer stop()
	file, err := remote.OpenFile(path, flags)
	if err != nil || ctx.Err() != nil {
		session.Close()
		remote.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return &fileStream{file: file, remote: remote, session: session}, nil
}

func (c *Client) newSubsystem(ctx context.Context) (*sftp.Client, *ssh.Session, error) {
	session, err := c.newSession(ctx)
	if err != nil {
		return nil, nil, err
	}
	stop := context.AfterFunc(ctx, func() { session.Close() })
	defer stop()
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, nil, err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		session.Close()
		return nil, nil, err
	}
	go io.Copy(io.Discard, stderr)
	if err := session.RequestSubsystem("sftp"); err != nil {
		session.Close()
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, err
	}
	remote, err := sftp.NewClientPipe(stdout, stdin)
	if err != nil {
		session.Close()
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, err
	}
	if ctx.Err() != nil {
		session.Close()
		remote.Close()
		return nil, nil, ctx.Err()
	}
	return remote, session, nil
}

func (c *Client) newSession(ctx context.Context) (*ssh.Session, error) {
	type result struct {
		session *ssh.Session
		err     error
	}
	done := make(chan result)
	go func() {
		session, err := c.transport.NewSession()
		select {
		case done <- result{session, err}:
		case <-ctx.Done():
			if session != nil {
				session.Close()
			}
		}
	}()
	select {
	case result := <-done:
		return result.session, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type fileStream struct {
	file      *sftp.File
	remote    *sftp.Client
	session   *ssh.Session
	abortOnce sync.Once
	closeOnce sync.Once
	closeErr  error
}

func (f *fileStream) Read(p []byte) (int, error)  { return f.file.Read(p) }
func (f *fileStream) Write(p []byte) (int, error) { return f.file.Write(p) }
func (f *fileStream) Close() error {
	f.closeOnce.Do(func() { f.closeErr = f.file.Close(); f.Abort() })
	return f.closeErr
}
func (f *fileStream) Abort() error {
	f.abortOnce.Do(func() { f.session.Close(); f.remote.Close() })
	return nil
}
