//go:build darwin

package credential

import (
	"fmt"
	"os"
	"syscall"
)

func securePrivatePath(path string, directory bool) error {
	mode := os.FileMode(0o600)
	if directory {
		mode = 0o700
	}
	return os.Chmod(path, mode)
}

func validatePrivatePath(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path is a symbolic link")
	}
	if info.IsDir() != directory {
		if directory {
			return fmt.Errorf("path is not a directory")
		}
		return fmt.Errorf("path is not a regular file")
	}
	if !directory && !info.Mode().IsRegular() {
		return fmt.Errorf("path is not a regular file")
	}
	want := os.FileMode(0o600)
	if directory {
		want = 0o700
	}
	if info.Mode().Perm() != want {
		return fmt.Errorf("path permissions are %04o, want %04o", info.Mode().Perm(), want)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("path is not owned by the current user")
	}
	return nil
}

func replacePrivateFile(source, target string) error {
	return os.Rename(source, target)
}

func syncPrivateDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
