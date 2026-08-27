package filelock

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var safeKey = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type Locker struct {
	Dir           string
	RetryInterval time.Duration
}

func (l Locker) Lock(ctx context.Context, key string) (func() error, error) {
	if key == "" || key == "." || key == ".." || !safeKey.MatchString(key) {
		return nil, fmt.Errorf("invalid lock key")
	}
	if err := os.MkdirAll(l.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(l.Dir, key+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	interval := l.RetryInterval
	if interval <= 0 {
		interval = 50 * time.Millisecond
	}
	for {
		acquired, err := tryPlatformLock(file)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if acquired {
			var once sync.Once
			var unlockErr error
			return func() error {
				once.Do(func() {
					if err := unlockPlatformFile(file); err != nil {
						unlockErr = err
					}
					if err := file.Close(); unlockErr == nil && err != nil {
						unlockErr = err
					}
				})
				return unlockErr
			}, nil
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
