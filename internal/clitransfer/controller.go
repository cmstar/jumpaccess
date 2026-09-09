// Package clitransfer 负责 CLI 的 ZMODEM 识别、本地路径输入和进度展示。
package clitransfer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/cmstar/jumpaccess/internal/application/zmodemfiles"
	"github.com/cmstar/jumpaccess/internal/downloads"
	"github.com/cmstar/jumpaccess/internal/zmodem"
)

var errExpired = errors.New("transfer request expired")

type Options struct {
	DownloadDirectory string
	// 注入点供测试验证系统目录，不修改用户系统设置。
	DefaultDirectory func() (string, error)
}

type controller struct {
	ctx     context.Context
	stream  *stream
	input   *inputRouter
	remote  *remoteWriter
	display io.Writer
	options Options
	files   zmodemfiles.Store
}

// Run 在直接交互式 SSH 中默认识别 ZMODEM；ProxyCommand 不调用此适配器。
func Run(ctx context.Context, keyboard io.Reader, display io.Writer, remote io.Writer, incoming io.Reader, options Options) error {
	ctx, cancel := context.WithCancel(ctx)
	writer := newRemoteWriter(ctx, remote)
	c := controller{ctx: ctx, remote: writer, display: display, options: options}
	c.stream = &stream{ctx: ctx, incoming: pump(ctx, incoming)}
	c.input = &inputRouter{remote: writer}
	go c.input.run(ctx, keyboard)
	defer c.files.CloseSession("")
	defer c.input.mode(true, nil, nil)
	defer cancel()
	pending := []byte(nil)
	for {
		wait := time.Duration(0)
		if len(pending) > 0 {
			wait = time.Second
		}
		data, err := c.stream.next(wait)
		if err != nil {
			if len(pending) > 0 {
				_, _ = display.Write(pending)
				pending = nil
			}
			if errors.Is(err, errIdle) {
				continue
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		pending = append(pending, data...)
		for len(pending) > 0 {
			start := bytes.Index(pending, []byte("**\x18B"))
			if start < 0 {
				keep := 0
				for n := 1; n < 4 && n <= len(pending); n++ {
					if bytes.HasSuffix(pending, []byte("**\x18B")[:n]) {
						keep = n
					}
				}
				if _, err := display.Write(pending[:len(pending)-keep]); err != nil {
					return err
				}
				pending = append([]byte(nil), pending[len(pending)-keep:]...)
				break
			}
			if start > 0 {
				if _, err := display.Write(pending[:start]); err != nil {
					return err
				}
				pending = pending[start:]
			}
			if len(pending) < 20 {
				break
			}
			upload, valid := zmodem.InitialHeader(pending[:20])
			if !valid {
				if _, err := display.Write(pending[:1]); err != nil {
					return err
				}
				pending = pending[1:]
				continue
			}
			windowSize := zmodem.ReceiverWindow(pending[:20])
			c.stream.rest = append([]byte(nil), pending[20:]...)
			pending = nil
			c.transfer(upload, windowSize)
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}
}

func (c *controller) transfer(upload bool, windowSize int) {
	ctx, cancel := context.WithCancel(c.ctx)
	defer cancel()
	c.stream.ctx = ctx
	c.stream.timeout = 60 * time.Second
	c.input.mode(true, nil, cancel)
	defer func() {
		c.files.CloseSession("")
		c.stream.ctx = c.ctx
		c.stream.timeout = 0
		c.input.mode(false, nil, nil)
	}()
	progress := newProgress(c.display, upload)
	var err error
	if upload {
		var path string
		path, err = c.prompt(ctx, "Local file to upload (empty to cancel): ", cancel)
		if err == nil && path == "" {
			err = zmodem.ErrCancelled
		}
		if err == nil {
			var file *os.File
			file, err = os.Open(path)
			if err != nil {
				err = errors.New("cannot open local upload file")
			} else {
				defer file.Close()
				info, statErr := file.Stat()
				if statErr != nil || !info.Mode().IsRegular() {
					err = errors.New("upload requires a regular file")
				} else {
					var skipped bool
					skipped, err = zmodem.Send(c.stream, contextWriter{ctx, c.remote}, zmodem.Upload{Name: info.Name(), Size: info.Size(), Reader: file}, zmodem.SendOptions{Progress: progress.update, WindowSize: windowSize})
					if skipped {
						_, _ = fmt.Fprint(c.display, "\r\nSkipped by remote.\r\n")
					}
				}
			}
		}
	} else {
		var grant string
		var localName string
		grant, err = c.directory(ctx, cancel)
		if err == nil {
			err = zmodem.Receive(c.stream, contextWriter{ctx, c.remote}, zmodem.ReceiveOptions{
				Open: func(name string, size int64) (zmodem.Download, error) {
					file, err := c.files.CreateDownload("cli", grant, name, size)
					if err != nil {
						return zmodem.Download{}, errors.New("cannot create local download file")
					}
					localName = file.Name
					return zmodem.Download{
						Write: func(data []byte) error {
							if err := c.files.Write(file.ID, data); err != nil {
								return errors.New("cannot write local download file")
							}
							return nil
						},
						Close: func(complete bool) error {
							if err := c.files.Close(file.ID, complete); err != nil {
								return errors.New("cannot save local download file")
							}
							return nil
						},
					}, nil
				}, Progress: func(_ string, size, offset int64, complete bool) { progress.update(localName, size, offset, complete) },
			})
		}
	}
	if err != nil {
		if errors.Is(err, errExpired) {
			_, _ = fmt.Fprint(c.display, "\r\nTransfer request expired. Run the command again.\r\n")
			return
		}
		cancelContext, stopCancel := context.WithTimeout(c.ctx, time.Second)
		_ = zmodem.Cancel(contextWriter{cancelContext, c.remote})
		stopCancel()
		c.stream.ctx = c.ctx
		// 丢弃取消后残留的协议字节，等待有限的静默窗口，防止乱码或误入下一次传输。
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if _, e := c.stream.next(100 * time.Millisecond); e != nil {
				break
			}
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, zmodem.ErrCancelled) {
			_, _ = fmt.Fprint(c.display, "\r\nTransfer cancelled. Press Enter to refresh the remote prompt.\r\n")
		} else {
			_, _ = fmt.Fprintf(c.display, "\r\nTransfer failed: %s. Press Enter to refresh the remote prompt.\r\n", safeText(err.Error()))
		}
	}
}

func (c *controller) directory(ctx context.Context, cancel context.CancelFunc) (string, error) {
	path := c.options.DownloadDirectory
	if path == "" {
		resolve := c.options.DefaultDirectory
		if resolve == nil {
			resolve = downloads.Directory
		}
		path, _ = resolve()
	}
	for {
		if path != "" {
			info, err := os.Stat(path)
			if err == nil && info.IsDir() {
				probe, err := os.CreateTemp(path, ".jumpaccess-write-check-*")
				if err == nil {
					_ = probe.Close()
					_ = os.Remove(probe.Name())
					grant, err := c.files.GrantDirectory("cli", path)
					if err == nil {
						_, _ = fmt.Fprintf(c.display, "\r\nDownload directory: %s\r\n", safeText(path))
						return grant, nil
					}
				}
			}
		}
		_, _ = fmt.Fprint(c.display, "\r\nDownload directory is unavailable.\r\n")
		var err error
		path, err = c.prompt(ctx, "Local download directory (empty to cancel): ", cancel)
		if err != nil {
			return "", err
		}
		if path == "" {
			return "", zmodem.ErrCancelled
		}
	}
}

func (c *controller) prompt(ctx context.Context, label string, cancel context.CancelFunc) (string, error) {
	keys := make(chan []byte, 1)
	c.input.mode(true, keys, cancel)
	defer c.input.mode(true, nil, cancel)
	_, _ = fmt.Fprint(c.display, "\r\n"+label)
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	line := []byte(nil)
	echo := []byte(nil)
	remote := c.stream.rest
	c.stream.rest = nil
	escapeState := 0
	for {
		// 包括起始帧同一读取块中的尾部，不能漏掉已经返回 Shell 的情况。
		for len(remote) > 0 {
			if remote[0] == 0x11 {
				remote = remote[1:]
				continue
			}
			prefix := []byte("**\x18B")
			if len(remote) < 4 && bytes.Equal(remote, prefix[:len(remote)]) {
				break
			}
			if len(remote) >= 4 && bytes.Equal(remote[:4], prefix) {
				if len(remote) < 20 {
					break
				}
				if _, valid := zmodem.InitialHeader(remote[:20]); valid {
					remote = remote[20:]
					continue
				}
			}
			c.stream.rest = append(c.stream.rest, remote...)
			return "", errExpired
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
			return "", errIdle
		case result, ok := <-c.stream.incoming:
			if !ok {
				return "", io.EOF
			}
			if result.err != nil {
				c.stream.terminalErr = result.err
			}
			remote = append(remote, result.data...)
			if len(remote) == 0 && result.err != nil {
				return "", result.err
			}
		case data := <-keys:
			for _, b := range data {
				if b == 3 {
					return "", zmodem.ErrCancelled
				}
				if escapeState != 0 {
					if escapeState == 1 && b == '[' {
						escapeState = 2
						continue
					}
					if b >= 0x40 && b <= 0x7e {
						escapeState = 0
					}
					continue
				}
				if b == 27 {
					escapeState = 1
					continue
				}
				switch b {
				case '\r', '\n':
					_, _ = fmt.Fprint(c.display, "\r\n")
					// 起始帧后的剩余 XON 不交给本地提示；保留未收齐的重发帧。
					c.stream.rest = append(c.stream.rest, remote...)
					return localPath(string(line)), nil
				case 8, 127:
					if len(line) > 0 {
						_, n := utf8.DecodeLastRune(line)
						line = line[:len(line)-n]
						_, _ = fmt.Fprint(c.display, "\b \b")
					}
				case 21:
					for len(line) > 0 {
						_, n := utf8.DecodeLastRune(line)
						line = line[:len(line)-n]
						_, _ = fmt.Fprint(c.display, "\b \b")
					}
				default:
					if b >= 32 {
						if len(line) >= 4096 {
							return "", errors.New("local path is too long")
						}
						line = append(line, b)
						echo = append(echo, b)
						for utf8.FullRune(echo) {
							_, n := utf8.DecodeRune(echo)
							_, _ = c.display.Write(echo[:n])
							echo = echo[n:]
						}
					}
				}
			}
		}
	}
}

func localPath(path string) string {
	path = strings.TrimSpace(path)
	if len(path) >= 2 && ((path[0] == '"' && path[len(path)-1] == '"') || (path[0] == '\'' && path[len(path)-1] == '\'')) {
		path = path[1 : len(path)-1]
	}
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimLeft(path[1:], "/\\"))
		}
	}
	return path
}
func safeText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 {
			return -1
		}
		return r
	}, text)
}

type contextWriter struct {
	ctx context.Context
	out *remoteWriter
}

func (w contextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(w.ctx, 60*time.Second)
	defer cancel()
	return w.out.write(ctx, p)
}

type progress struct {
	out    io.Writer
	upload bool
	name   string
	last   time.Time
}

func newProgress(out io.Writer, upload bool) *progress { return &progress{out: out, upload: upload} }
func (p *progress) update(name string, size, offset int64, complete bool) {
	if p.name != name {
		p.name = name
		p.last = time.Time{}
		direction := "Download"
		if p.upload {
			direction = "Upload"
		}
		_, _ = fmt.Fprintf(p.out, "\r\n%s %s\r\n", direction, safeText(name))
	}
	if !complete && offset < size && time.Since(p.last) < 100*time.Millisecond {
		return
	}
	p.last = time.Now()
	percent := int64(0)
	if size > 0 {
		percent = min(99, offset*100/size)
	}
	status := ""
	if offset == size {
		status = " - Saving"
		if p.upload {
			status = " - Waiting for confirmation"
		}
	}
	ending := ""
	if complete {
		percent = 100
		status = " - Complete"
		ending = "\r\n"
	}
	_, _ = fmt.Fprintf(p.out, "\r\x1b[2K%d%% - %.2f / %.2f MiB%s%s", percent, float64(offset)/(1024*1024), float64(size)/(1024*1024), status, ending)
	if complete {
		p.name = ""
	}
}
