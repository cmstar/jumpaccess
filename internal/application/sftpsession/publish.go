package sftpsession

import (
	"context"
	"io"
	"os"
)

type localPublisher interface {
	Link(string, string) error
	Open(string) (*os.File, error)
	OpenFile(string, int, os.FileMode) (*os.File, error)
	Lstat(string) (os.FileInfo, error)
	Remove(string) error
}

func publishLocal(ctx context.Context, root localPublisher, temp, destination string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.Link(temp, destination); err == nil {
		return nil
	}
	source, err := root.Open(temp)
	if err != nil {
		return err
	}
	defer source.Close()
	dest, err := root.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	created, statErr := dest.Stat()
	if statErr != nil {
		_ = dest.Close()
		return statErr
	}
	buffer := make([]byte, 64*1024)
	for {
		if err = ctx.Err(); err != nil {
			break
		}
		var n int
		n, err = source.Read(buffer)
		if n > 0 {
			written, writeErr := dest.Write(buffer[:n])
			if writeErr != nil {
				err = writeErr
				break
			}
			if written != n {
				err = io.ErrShortWrite
				break
			}
		}
		if err == io.EOF {
			err = nil
			break
		}
		if err != nil {
			break
		}
	}
	closeErr := dest.Close()
	if err != nil || closeErr != nil {
		if current, statErr := root.Lstat(destination); statErr == nil && os.SameFile(created, current) {
			_ = root.Remove(destination)
		}
	}
	if err != nil {
		return err
	}
	return closeErr
}
