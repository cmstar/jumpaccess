package sftpsession

import (
	"errors"
	"fmt"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

func (m *Manager) remote(id, directory string) (RemoteClient, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || s.state.Status != StatusActive || s.client == nil {
		return nil, "", fmt.Errorf("%w: %s", ErrSessionNotActive, id)
	}
	if strings.ContainsRune(directory, 0) {
		return nil, "", fmt.Errorf("SFTP path contains NUL")
	}
	if directory == "" {
		directory = s.state.Directory
	} else if !path.IsAbs(directory) {
		directory = path.Join(s.state.Directory, directory)
	}
	return s.client, path.Clean(directory), nil
}
func (m *Manager) ReadDirectory(id, directory string) (Directory, error) {
	client, resolved, err := m.remote(id, directory)
	if err != nil {
		return Directory{}, err
	}
	canonical, err := client.RealPath(resolved)
	if err != nil {
		return Directory{}, err
	}
	infos, err := client.ReadDir(canonical)
	if err != nil {
		return Directory{}, err
	}
	result := Directory{Path: canonical, Entries: make([]FileEntry, 0, len(infos))}
	for _, info := range infos {
		if !validRemoteName(info.Name()) {
			return Directory{}, fmt.Errorf("SFTP returned an invalid filename")
		}
		kind := "file"
		if info.Mode()&os.ModeSymlink != 0 {
			kind = "symlink"
		} else if info.IsDir() {
			kind = "directory"
		}
		result.Entries = append(result.Entries, FileEntry{Name: info.Name(), Path: path.Join(canonical, info.Name()), Type: kind, Size: info.Size(), ModifiedAt: info.ModTime().UTC().Format(time.RFC3339), Permissions: info.Mode().String()})
	}
	sort.Slice(result.Entries, func(i, j int) bool {
		a, b := result.Entries[i], result.Entries[j]
		if (a.Type == "directory") != (b.Type == "directory") {
			return a.Type == "directory"
		}
		return a.Name < b.Name
	})
	m.mu.Lock()
	if s, ok := m.sessions[id]; ok {
		s.state.Directory = canonical
	}
	m.mu.Unlock()
	return result, nil
}
func validRemoteName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\x00")
}
func (m *Manager) HomeDirectory(id string) (string, error) {
	client, _, err := m.remote(id, "")
	if err != nil {
		return "", err
	}
	return client.InitialDirectory("")
}
func (m *Manager) MakeDirectory(id, directory string) error {
	if err := m.authorize(id, "upload"); err != nil {
		return err
	}
	client, p, err := m.remote(id, directory)
	if err != nil {
		return err
	}
	return client.Mkdir(p)
}
func (m *Manager) Rename(id, source, newName string) error {
	if err := m.authorize(id, "upload"); err != nil {
		return err
	}
	if !validRemoteName(newName) || strings.ContainsRune(newName, '\\') {
		return fmt.Errorf("SFTP new name must be a filename")
	}
	client, p, err := m.remote(id, source)
	if err != nil {
		return err
	}
	if p == "/" {
		return fmt.Errorf("cannot rename SFTP root")
	}
	dest := path.Join(path.Dir(p), newName)
	if p == dest {
		return nil
	}
	if _, err := client.Lstat(dest); err == nil {
		return fmt.Errorf("SFTP destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return client.Rename(p, dest)
}
func (m *Manager) Remove(id string, paths []string) error {
	if err := m.authorize(id, "delete"); err != nil {
		return err
	}
	for _, p := range paths {
		client, resolved, err := m.remote(id, p)
		if err != nil {
			return err
		}
		if p == "" || resolved == "/" {
			return fmt.Errorf("cannot remove SFTP root")
		}
		if err := removeTree(client, resolved); err != nil {
			return err
		}
	}
	return nil
}

func accountPermissions(actions []jumpserver.LabelValue) *Permissions {
	if actions == nil {
		return nil
	}
	var upload, download, remove bool
	for _, a := range actions {
		switch a.Value {
		case "upload":
			upload = true
		case "download":
			download = true
		case "delete":
			remove = true
		}
	}
	return &Permissions{Upload: &upload, Download: &download, Delete: &remove}
}
func (m *Manager) authorize(id, action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || s.state.Status != StatusActive {
		return ErrSessionNotActive
	}
	if s.state.Permissions == nil {
		return nil
	}
	var allowed *bool
	switch action {
	case "upload":
		allowed = s.state.Permissions.Upload
	case "download":
		allowed = s.state.Permissions.Download
	case "delete":
		allowed = s.state.Permissions.Delete
	}
	if allowed != nil && !*allowed {
		return fmt.Errorf("SFTP account does not permit %s", action)
	}
	return nil
}
func removeTree(client RemoteClient, p string) error {
	info, err := client.Lstat(p)
	if err != nil {
		return err
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		entries, err := client.ReadDir(p)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !validRemoteName(entry.Name()) {
				return fmt.Errorf("SFTP returned an invalid filename")
			}
			if err := removeTree(client, path.Join(p, entry.Name())); err != nil {
				return err
			}
		}
		return client.RemoveDirectory(p)
	}
	return client.Remove(p)
}
