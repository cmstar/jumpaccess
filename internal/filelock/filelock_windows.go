//go:build windows

package filelock

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
)

func tryPlatformLock(file *os.File) (bool, error) {
	overlapped := new(windows.Overlapped)
	err := windows.LockFileEx(
		windows.Handle(file.Fd()),
		lockfileFailImmediately|lockfileExclusiveLock,
		0,
		1,
		0,
		overlapped,
	)
	if err == windows.ERROR_LOCK_VIOLATION {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock file: %w", err)
	}
	return true, nil
}

func unlockPlatformFile(file *os.File) error {
	if err := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, new(windows.Overlapped)); err != nil {
		return fmt.Errorf("unlock file: %w", err)
	}
	return nil
}
