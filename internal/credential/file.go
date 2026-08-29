package credential

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const credentialFileSuffix = ".json"

type fileBackend struct {
	directory string
}

// NewFileBackend stores each opaque credential key in its own private file.
// The key is hashed instead of normalized so character replacement cannot
// collapse distinct profile names to the same filesystem name.
func NewFileBackend(directory string) Backend {
	return fileBackend{directory: directory}
}

func (b fileBackend) Get(key string) ([]byte, error) {
	path, err := b.path(key)
	if err != nil {
		return nil, err
	}
	if err := validatePrivatePath(b.directory, true); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("validate credential directory: %w", err)
	}
	if err := validatePrivatePath(path, false); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("validate credential file: %w", err)
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read credential file: %w", err)
	}
	return data, nil
}

func (b fileBackend) Set(key string, value []byte) error {
	path, err := b.path(key)
	if err != nil {
		return err
	}
	if err := ensurePrivateDirectory(b.directory); err != nil {
		return fmt.Errorf("prepare credential directory: %w", err)
	}

	temporary, err := os.CreateTemp(b.directory, ".credential-*")
	if err != nil {
		return fmt.Errorf("create temporary credential file: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		if keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("restrict temporary credential file: %w", err)
	}
	if _, err := temporary.Write(value); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary credential file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary credential file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary credential file: %w", err)
	}
	if err := securePrivatePath(temporaryPath, false); err != nil {
		return fmt.Errorf("secure temporary credential file: %w", err)
	}
	if err := validatePrivatePath(temporaryPath, false); err != nil {
		return fmt.Errorf("validate temporary credential file: %w", err)
	}
	if err := replacePrivateFile(temporaryPath, path); err != nil {
		return fmt.Errorf("replace credential file: %w", err)
	}
	keepTemporary = false
	if err := validatePrivatePath(path, false); err != nil {
		return fmt.Errorf("validate stored credential file: %w", err)
	}
	if err := syncPrivateDirectory(b.directory); err != nil {
		return fmt.Errorf("sync credential directory: %w", err)
	}
	return nil
}

func (b fileBackend) Delete(key string) error {
	path, err := b.path(key)
	if err != nil {
		return err
	}
	if err := validatePrivatePath(b.directory, true); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("validate credential directory: %w", err)
	}
	if err := validatePrivatePath(path, false); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("validate credential file: %w", err)
	}
	if err := os.Remove(path); errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("delete credential file: %w", err)
	}
	if err := syncPrivateDirectory(b.directory); err != nil {
		return fmt.Errorf("sync credential directory: %w", err)
	}
	return nil
}

func (b fileBackend) path(key string) (string, error) {
	if strings.TrimSpace(b.directory) == "" {
		return "", fmt.Errorf("credential directory is empty")
	}
	if key == "" {
		return "", fmt.Errorf("credential key is empty")
	}
	digest := sha256.Sum256([]byte(key))
	return filepath.Join(b.directory, fmt.Sprintf("%x%s", digest, credentialFileSuffix)), nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if err := securePrivatePath(path, true); err != nil {
		return err
	}
	return validatePrivatePath(path, true)
}
