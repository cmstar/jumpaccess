package sftpsession

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func validLocalName(name string) bool {
	if !validRemoteName(name) || !filepath.IsLocal(name) || strings.ContainsAny(name, "\\:<>\"|?*") || strings.TrimRight(name, ". ") != name {
		return false
	}
	for _, r := range name {
		if r < 32 {
			return false
		}
	}
	stem := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return false
	}
	if len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9' {
		return false
	}
	return true
}

func (m *Manager) downloadFile(job *transfer, client RemoteClient) error {
	if client == nil {
		return ErrSessionNotActive
	}
	if !validLocalName(job.event.Name) {
		return fmt.Errorf("SFTP filename is not safe for a local download")
	}
	root, err := os.OpenRoot(job.destinationRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	return m.downloadTree(job, client, root, job.event.Source, job.event.Name)
}
func (m *Manager) downloadTree(job *transfer, client RemoteClient, root *os.Root, sourcePath, destination string) error {
	if err := job.ctx.Err(); err != nil {
		return err
	}
	info, err := client.Lstat(sourcePath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errSkipped
	}
	existing, statErr := root.Lstat(destination)
	overwrite := false
	if statErr == nil && !(info.IsDir() && existing.IsDir() && existing.Mode()&os.ModeSymlink == 0) {
		choice, err := m.awaitConflict(job, sourcePath, filepath.Join(job.destinationRoot, destination))
		if err != nil {
			return err
		}
		if choice == "skip" {
			return errSkipped
		}
		if choice == "keep-both" {
			name, err := unusedName(filepath.ToSlash(destination), func(p string) (os.FileInfo, error) { return root.Lstat(filepath.FromSlash(p)) })
			if err != nil {
				return err
			}
			destination = filepath.FromSlash(name)
			existing = nil
			if sourcePath == job.event.Source {
				m.mu.Lock()
				job.event.Destination = filepath.Join(job.destinationRoot, destination)
				m.mu.Unlock()
			}
		}
		if choice == "overwrite" && (info.IsDir() || existing.IsDir() || existing.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("cannot overwrite local entries with incompatible types")
		}
		overwrite = choice == "overwrite"
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if info.IsDir() {
		if existing == nil {
			if err := root.Mkdir(destination, 0700); err != nil {
				return err
			}
		}
		entries, err := client.ReadDir(sourcePath)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !validLocalName(entry.Name()) {
				return fmt.Errorf("SFTP returned an invalid filename")
			}
			if err := m.downloadTree(job, client, root, path.Join(sourcePath, entry.Name()), filepath.Join(destination, entry.Name())); err != nil && err != errSkipped {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("SFTP download requires a regular file")
	}
	source, err := client.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	stopSource := abortOnCancel(job.ctx, source)
	defer stopSource()
	temp := filepath.Join(filepath.Dir(destination), ".jumpaccess-"+job.event.ID+".part")
	dest, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	stopDest := abortOnCancel(job.ctx, dest)
	defer stopDest()
	m.mu.Lock()
	job.event.Total += info.Size()
	m.mu.Unlock()
	_, copyErr := m.copyStream(job, dest, source)
	closeErr := dest.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := job.ctx.Err(); err != nil {
		return err
	}
	if overwrite {
		return root.Rename(temp, destination)
	}
	// 排他发布，防止复制期间出现的同名文件被无确认覆盖。
	return publishLocal(job.ctx, root, temp, destination)
}
