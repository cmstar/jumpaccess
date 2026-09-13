// Package zmodemfiles 管理用户通过原生对话框授权的 ZMODEM 文件句柄。
package zmodemfiles

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
)

const ChunkSize = 64 * 1024

// 所有会话共用下载缓冲额度，避免多会话按文件大小无限分配内存。
const downloadBufferLimit = 50 * 1024 * 1024

type File struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Path 是用于本机显示的绝对路径；协议仍只发送 Name。
	Path string `json:"path"`
	Size int64  `json:"size"`
}
type openedFile struct {
	file          *os.File
	buffer        *bufio.Writer
	session       string
	download      bool
	size, written int64
}
type directoryGrant struct{ session, path string }
type Store struct {
	mu             sync.Mutex
	files          map[string]*openedFile
	directories    map[string]directoryGrant
	bufferReserved int64
}

func randomID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func (s *Store) OpenUploads(session string, paths []string) ([]File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]File, 0, len(paths))
	for _, path := range paths {
		absolutePath, err := filepath.Abs(path)
		var file *os.File
		if err == nil {
			file, err = os.Open(absolutePath)
		}
		if err == nil {
			var info os.FileInfo
			info, err = file.Stat()
			if err == nil && !info.Mode().IsRegular() {
				err = fmt.Errorf("只能上传普通文件")
			}
			if err == nil {
				var id string
				id, err = randomID()
				if err == nil {
					if s.files == nil {
						s.files = make(map[string]*openedFile)
					}
					s.files[id] = &openedFile{file: file, session: session, size: info.Size()}
					result = append(result, File{ID: id, Name: info.Name(), Path: absolutePath, Size: info.Size()})
				}
			}
		}
		if err != nil {
			if file != nil {
				_ = file.Close()
			}
			for _, opened := range result {
				_ = s.closeLocked(opened.ID, false)
			}
			return nil, err
		}
	}
	return result, nil
}

func (s *Store) GrantDirectory(session, path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("下载位置必须是文件夹")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	id, err := randomID()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.directories == nil {
		s.directories = make(map[string]directoryGrant)
	}
	s.directories[id] = directoryGrant{session, path}
	return id, nil
}

func (s *Store) CreateDownload(session, grantID, name string, size int64) (File, error) {
	// 同时拒绝 Windows 和 POSIX 路径，以及设备名、ADS 和控制字符。
	if size < 0 || size > 1<<53-1 || name == "" || name == "." || name == ".." || len(name) > 240 || strings.ContainsAny(name, `/\:<>"|?*`) || strings.TrimRight(name, ". ") != name || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return File{}, fmt.Errorf("无效的下载文件名或大小")
	}
	stem := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' && stem[3] <= '9') {
		return File{}, fmt.Errorf("无效的下载文件名")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	grant, ok := s.directories[grantID]
	if !ok || grant.session != session {
		return File{}, fmt.Errorf("下载位置授权已失效")
	}
	id, err := randomID()
	if err != nil {
		return File{}, err
	}
	for suffix := 0; suffix < 10000; suffix++ {
		candidate := name
		if suffix > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, filepath.Ext(name)), suffix, filepath.Ext(name))
		}
		file, err := os.OpenFile(filepath.Join(grant.path, candidate), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return File{}, err
		}
		if s.files == nil {
			s.files = make(map[string]*openedFile)
		}
		opened := &openedFile{file: file, session: session, download: true, size: size}
		capacity := min(size, downloadBufferLimit-s.bufferReserved)
		// 无额度时沿用操作系统的文件缓存，不继续增加应用缓冲。
		if capacity > 0 {
			opened.buffer = bufio.NewWriterSize(file, int(capacity))
			s.bufferReserved += int64(opened.buffer.Size())
		}
		s.files[id] = opened
		return File{ID: id, Name: candidate, Path: file.Name(), Size: size}, nil
	}
	return File{}, fmt.Errorf("同名文件过多，无法创建下载文件")
}

func (s *Store) Read(id string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[id]
	if !ok || f.download {
		return nil, fmt.Errorf("上传文件句柄已失效")
	}
	data := make([]byte, ChunkSize)
	n, err := f.file.Read(data)
	if err == io.EOF {
		err = nil
	}
	return data[:n], err
}

func (s *Store) Write(id string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[id]
	if !ok || !f.download {
		return fmt.Errorf("下载文件句柄已失效")
	}
	if len(data) > ChunkSize || f.written+int64(len(data)) > f.size {
		return fmt.Errorf("下载数据超出声明大小")
	}
	var writer io.Writer = f.file
	if f.buffer != nil {
		writer = f.buffer
	}
	n, err := writer.Write(data)
	f.written += int64(n)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return err
}

func (s *Store) Close(id string, complete bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeLocked(id, complete)
}

func (s *Store) closeLocked(id string, complete bool) error {
	f, ok := s.files[id]
	if !ok {
		return nil
	}
	delete(s.files, id)
	defer func() {
		if f.buffer != nil {
			s.bufferReserved -= int64(f.buffer.Size())
			f.buffer = nil
		}
	}()
	var err error
	if f.download && complete {
		if f.written != f.size {
			err = fmt.Errorf("下载未完成")
		} else {
			if f.buffer != nil {
				err = f.buffer.Flush()
			}
			if err == nil {
				err = f.file.Sync()
			}
		}
	}
	if closeErr := f.file.Close(); err == nil {
		err = closeErr
	}
	if f.download && (!complete || err != nil) {
		_ = os.Remove(f.file.Name())
	}
	return err
}

func (s *Store) CloseSession(session string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, file := range s.files {
		if session == "" || file.session == session {
			_ = s.closeLocked(id, false)
		}
	}
	for id, grant := range s.directories {
		if session == "" || grant.session == session {
			delete(s.directories, id)
		}
	}
}
