package sftpsession

import (
	"context"
	"io"
	"os"
	"time"

	connectapp "github.com/cmstar/jumpaccess/internal/application/connect"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"golang.org/x/crypto/ssh"
)

const (
	StatusConnecting = "connecting"
	StatusActive     = "active"
	StatusClosed     = "closed"
	StatusFailed     = "failed"
)

type StartRequest struct {
	Profile            string `json:"profile"`
	Organization       string `json:"organization"`
	Target             string `json:"target"`
	Account            string `json:"account"`
	Directory          string `json:"directory"`
	SourceSSHSessionID string `json:"sourceSSHSessionId"`
}

type StateEvent struct {
	Asset        string       `json:"asset"`
	Permissions  *Permissions `json:"permissions,omitempty"`
	ID           string       `json:"id"`
	Status       string       `json:"status"`
	Title        string       `json:"title"`
	Profile      string       `json:"profile"`
	Organization string       `json:"organization"`
	Target       string       `json:"target"`
	Alias        string       `json:"alias"`
	AssetID      string       `json:"assetId"`
	AssetName    string       `json:"assetName"`
	Account      string       `json:"account"`
	Directory    string       `json:"directory"`
	Error        string       `json:"error"`
}
type Permissions struct {
	Upload   *bool `json:"upload,omitempty"`
	Download *bool `json:"download,omitempty"`
	Delete   *bool `json:"delete,omitempty"`
}

type FileEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	Size        int64  `json:"size"`
	ModifiedAt  string `json:"modifiedAt"`
	Permissions string `json:"permissions"`
}

type Directory struct {
	Path    string      `json:"path"`
	Entries []FileEntry `json:"entries"`
}

type Preparer interface {
	Prepare(context.Context, connectapp.Options) (connectapp.Prepared, error)
}
type OpenOptions struct {
	Connection      jumpserver.ClientConnection
	Asset           jumpserver.AssetDetail
	Account         jumpserver.Account
	HostKeyCallback ssh.HostKeyCallback
	Timeout         time.Duration
}
type RemoteClient interface {
	Getwd() (string, error)
	RealPath(string) (string, error)
	InitialDirectory(string) (string, error)
	ReadDir(string) ([]os.FileInfo, error)
	Lstat(string) (os.FileInfo, error)
	Open(string) (io.ReadCloser, error)
	OpenFile(string, int) (io.WriteCloser, error)
	Mkdir(string) error
	Rename(string, string) error
	Remove(string) error
	RemoveDirectory(string) error
	Close() error
	Wait() error
}
type OpenFunc func(context.Context, OpenOptions) (RemoteClient, error)
