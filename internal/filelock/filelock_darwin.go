//go:build darwin

package filelock

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func tryPlatformLock(file *os.File) (bool, error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock file: %w", err)
	}
	return true, nil
}

func unlockPlatformFile(file *os.File) error {
	if err := unix.Flock(int(file.Fd()), unix.LOCK_UN); err != nil {
		return fmt.Errorf("unlock file: %w", err)
	}
	return nil
}
