package guiconfig

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/cmstar/jumpaccess/internal/filelock"
)

type Store struct {
	Path string
}

func (s Store) Update(ctx context.Context, change func(*Config) error) (err error) {
	unlock, err := (filelock.Locker{Dir: filepath.Join(filepath.Dir(s.Path), "locks")}).Lock(ctx, "gui")
	if err != nil {
		return err
	}
	defer func() {
		if unlockErr := unlock(); err == nil && unlockErr != nil {
			err = unlockErr
		}
	}()
	value, err := s.Load()
	if err != nil {
		return err
	}
	if err = change(&value); err != nil {
		return err
	}
	return s.Save(value)
}

func (s Store) Load() (Config, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Default(), nil
		}
		return Config{}, fmt.Errorf("read GUI config: %w", err)
	}
	return Decode(data)
}

func (s Store) Save(value Config) (err error) {
	if err := value.Validate(); err != nil {
		return err
	}
	directory := filepath.Dir(s.Path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create GUI config directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".gui-*.toml")
	if err != nil {
		return fmt.Errorf("create temporary GUI config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if err != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err = temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("protect temporary GUI config: %w", err)
	}
	if err = toml.NewEncoder(temporary).Encode(value); err != nil {
		return fmt.Errorf("encode GUI config: %w", err)
	}
	if err = temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary GUI config: %w", err)
	}
	if err = temporary.Close(); err != nil {
		return fmt.Errorf("close temporary GUI config: %w", err)
	}
	if err = os.Rename(temporaryPath, s.Path); err != nil {
		return fmt.Errorf("replace GUI config: %w", err)
	}
	return nil
}
