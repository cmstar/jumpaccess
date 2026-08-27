package sshhostkey

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type Store struct {
	Path    string
	Confirm func(host, fingerprint string) (bool, error)
}

var appendMu sync.Mutex

func (s Store) Callback(allowPrompt bool) (ssh.HostKeyCallback, error) {
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
	verify, err := knownhosts.New(s.Path)
	if err != nil {
		return nil, fmt.Errorf("load SSH known-hosts: %w", err)
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := verify(hostname, remote, key)
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
		appendMu.Lock()
		defer appendMu.Unlock()
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
