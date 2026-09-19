package sshhostkey

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/cmstar/jumpaccess/internal/filelock"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type Store struct {
	Path    string
	Confirm func(host, fingerprint string) (bool, error)
}

func (s Store) Callback(ctx context.Context, allowPrompt bool) (ssh.HostKeyCallback, error) {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return nil, fmt.Errorf("create SSH known-hosts directory: %w", err)
	}
	file, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create SSH known-hosts file: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close SSH known-hosts file: %w", err)
	}
	_, err = knownhosts.New(s.Path)
	if err != nil {
		return nil, fmt.Errorf("load SSH known-hosts: %w", err)
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		// 每次握手读取共享信任文件，不能使用创建 callback 时的旧快照。
		unlock, err := s.lock(ctx)
		if err != nil {
			return err
		}
		err = s.verify(hostname, remote, key)
		_ = unlock()
		if err == nil {
			return nil
		}
		var keyError *knownhosts.KeyError
		if !errors.As(err, &keyError) || len(keyError.Want) > 0 {
			return fmt.Errorf("SSH host key changed for %s", hostname)
		}
		if !allowPrompt || s.Confirm == nil {
			return fmt.Errorf("unknown SSH host key for %s; connect once with jumpctl ssh to review and trust it", hostname)
		}
		accepted, err := s.Confirm(hostname, ssh.FingerprintSHA256(key))
		if err != nil {
			return err
		}
		if !accepted {
			return fmt.Errorf("SSH host key was not trusted")
		}
		// 不持锁等待用户；确认后重新校验，防止另一个 CLI/GUI 已记录不同密钥。
		unlock, err = s.lock(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = unlock() }()
		err = s.verify(hostname, remote, key)
		if err == nil {
			return nil
		}
		if !errors.As(err, &keyError) || len(keyError.Want) > 0 {
			return fmt.Errorf("SSH host key changed for %s", hostname)
		}
		file, err := os.OpenFile(s.Path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("open SSH known-hosts file: %w", err)
		}
		line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key) + "\n"
		if _, err := file.WriteString(line); err != nil {
			_ = file.Close()
			return fmt.Errorf("save SSH host key: %w", err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close SSH known-hosts file: %w", err)
		}
		return nil
	}, nil
}

func (s Store) lock(ctx context.Context) (func() error, error) {
	// 信任文件操作应很短；即使调用者没有 deadline，也不能无限等待其他进程。
	lockContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := lockContext.Err(); err != nil {
		return nil, err
	}
	return (filelock.Locker{Dir: filepath.Join(filepath.Dir(s.Path), "locks")}).Lock(lockContext, "ssh-known-hosts")
}

func (s Store) verify(hostname string, remote net.Addr, key ssh.PublicKey) error {
	verify, err := knownhosts.New(s.Path)
	if err != nil {
		return fmt.Errorf("load SSH known-hosts: %w", err)
	}
	return verify(hostname, remote, key)
}
