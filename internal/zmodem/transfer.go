package zmodem

import (
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// Download 由调用方决定路径、缓冲和完成时的同步保存。
type Download struct {
	Write func([]byte) error
	Close func(complete bool) error
}
type ReceiveOptions struct {
	Open     func(name string, size int64) (Download, error)
	Progress func(name string, size, transferred int64, complete bool)
}
type Upload struct {
	Name   string
	Size   int64
	Reader io.ReadSeeker
}
type SendOptions struct {
	Progress   func(name string, size, transferred int64, complete bool)
	WindowSize int
}

func report(fn func(string, int64, int64, bool), name string, size, offset int64, complete bool) {
	if fn != nil {
		fn(name, size, offset, complete)
	}
}

// Receive 在调用方已消费 ZRQINIT 起始帧后接收一批文件。
func Receive(in io.ByteReader, out io.Writer, options ReceiveOptions) error {
	w := wire{in: in, out: out}
	var current *Download
	var name string
	var size, offset int64
	retries := 0
	defer func() {
		if current != nil {
			_ = current.Close(false)
		}
	}()
	// CRC16、全双工、流式接收、要求转义控制字符。
	ready := func() error { return w.writeHeader(zrinit, 0x43000000) }
	if err := ready(); err != nil {
		return err
	}
	for {
		h, err := w.readHeader()
		if err != nil {
			return err
		}
		switch h.kind {
		case zrqinit:
			if err := ready(); err != nil {
				return err
			}
		case zsinit:
			if _, _, err := w.readPacket(h.crc32); err != nil {
				return err
			}
			if err := w.writeHeader(zack, 0); err != nil {
				return err
			}
		case zfile:
			if current != nil {
				return errors.New("another file was offered before completion")
			}
			data, _, err := w.readPacket(h.crc32)
			if err != nil {
				return err
			}
			parts := bytes.SplitN(data, []byte{0}, 2)
			if len(parts) != 2 || len(parts[0]) == 0 {
				return errors.New("invalid ZMODEM file metadata")
			}
			name = string(parts[0])
			fields := strings.Fields(strings.TrimRight(string(parts[1]), "\x00"))
			if len(fields) == 0 {
				return errors.New("missing ZMODEM file size")
			}
			size, err = strconv.ParseInt(fields[0], 10, 64)
			if err != nil || size < 0 || size > math.MaxUint32 {
				return errors.New("ZMODEM file size must be below 4 GiB")
			}
			file, err := options.Open(name, size)
			if err != nil {
				return fmt.Errorf("create local download: %w", err)
			}
			current = &file
			offset = 0
			retries = 0
			report(options.Progress, name, size, offset, false)
			if err := w.writeHeader(zrpos, 0); err != nil {
				return err
			}
		case zdata:
			if current == nil {
				return errors.New("ZMODEM data without a file")
			}
			if int64(h.value) != offset {
				return errors.New("unexpected ZMODEM file offset")
			}
			for {
				data, end, err := w.readPacket(h.crc32)
				if errors.Is(err, errCRC) && retries < 10 {
					retries++
					if err := w.writeHeader(zrpos, uint32(offset)); err != nil {
						return err
					}
					break
				}
				if err != nil {
					return err
				}
				if offset+int64(len(data)) > size {
					return errors.New("download exceeds declared size")
				}
				if err := current.Write(data); err != nil {
					return fmt.Errorf("write local download: %w", err)
				}
				offset += int64(len(data))
				report(options.Progress, name, size, offset, false)
				if end == zcrcq || end == zcrcw {
					if err := w.writeHeader(zack, uint32(offset)); err != nil {
						return err
					}
				}
				if end == zcrce || end == zcrcw {
					break
				}
			}
		case zeof:
			if current == nil || int64(h.value) != offset || offset != size {
				return errors.New("incomplete ZMODEM download")
			}
			err := current.Close(true)
			current = nil
			if err != nil {
				return fmt.Errorf("save local download: %w", err)
			}
			report(options.Progress, name, size, offset, true)
			if err := ready(); err != nil {
				return err
			}
		case zfin:
			if current != nil {
				return errors.New("ZMODEM session ended before file completion")
			}
			if err := w.writeHeader(zfin, 0); err != nil {
				return err
			}
			for range 2 {
				b, err := w.rawByte()
				if err != nil {
					return err
				}
				if b != 'O' {
					return errors.New("invalid ZMODEM session ending")
				}
			}
			return nil
		default:
			return fmt.Errorf("unexpected ZMODEM header: %d", h.kind)
		}
	}
}

// Send 在调用方已消费 ZRINIT 起始帧后上传一个用户明确选择的普通文件。
func Send(in io.ByteReader, out io.Writer, file Upload, options SendOptions) (bool, error) {
	if file.Size < 0 || file.Size > math.MaxUint32 {
		return false, errors.New("ZMODEM file size must be below 4 GiB")
	}
	if file.Name == "" || strings.ContainsAny(file.Name, "/\\") || strings.IndexFunc(file.Name, unicode.IsControl) >= 0 {
		return false, errors.New("invalid upload filename")
	}
	w := wire{in: in, out: out}
	windowSize := 1024 * 1024
	if options.WindowSize > 0 {
		windowSize = min(windowSize, options.WindowSize)
	}
	if err := w.writeHeader(zsinit, 0x40000000); err != nil {
		return false, err
	}
	if err := w.writePacket([]byte{0}, zcrcw); err != nil {
		return false, err
	}
	// 选择本地路径期间远端 rz 可能重发 ZRINIT。
	for i := 0; ; i++ {
		h, err := w.readHeader()
		if err != nil {
			return false, err
		}
		if h.kind == zack {
			break
		}
		if h.kind != zrinit || i >= 10 {
			return false, errors.New("receiver did not acknowledge ZSINIT")
		}
	}
	metadata := []byte(fmt.Sprintf("%s\x00%d 0 100644 0 1 %d\x00", file.Name, file.Size, file.Size))
	if err := w.writeHeader(zfile, 0); err != nil {
		return false, err
	}
	if err := w.writePacket(metadata, zcrcw); err != nil {
		return false, err
	}
	var offset int64
	skipped := false
	for tries := 0; ; tries++ {
		if tries > 10 {
			return false, errors.New("too many ZMODEM file retries")
		}
		h, err := w.readHeader()
		if err != nil {
			return false, err
		}
		if h.kind == zcrc {
			count := file.Size
			if h.value != 0 && int64(h.value) < count {
				count = int64(h.value)
			}
			if _, err := file.Reader.Seek(0, io.SeekStart); err != nil {
				return false, err
			}
			crc := crc32.NewIEEE()
			if _, err := io.CopyN(crc, file.Reader, count); err != nil {
				return false, err
			}
			if err := w.writeHeader(zcrc, crc.Sum32()); err != nil {
				return false, err
			}
			continue
		}
		if h.kind == zskip {
			skipped = true
			break
		}
		if h.kind != zrpos {
			return false, fmt.Errorf("expected ZRPOS, got %d", h.kind)
		}
		offset = int64(h.value)
		if offset > file.Size {
			return false, errors.New("invalid upload resume offset")
		}
		if _, err := file.Reader.Seek(offset, io.SeekStart); err != nil {
			return false, err
		}
		report(options.Progress, file.Name, file.Size, offset, false)
		newFrame := true
		for {
			if newFrame {
				if err := w.writeHeader(zdata, uint32(offset)); err != nil {
					return false, err
				}
			}
			// 最多每 1 MiB 请求确认，并遵守远端接收窗口；子包最多 8 KiB。
			for frameBytes := 0; ; {
				n := int(min(int64(8192), file.Size-offset, int64(windowSize-frameBytes)))
				data := make([]byte, n)
				if _, err := io.ReadFull(file.Reader, data); err != nil {
					return false, err
				}
				offset += int64(n)
				frameBytes += n
				end := zcrcg
				if offset == file.Size {
					end = zcrce
				} else if frameBytes >= windowSize {
					end = zcrcq
				}
				if err := w.writePacket(data, end); err != nil {
					return false, err
				}
				report(options.Progress, file.Name, file.Size, offset, false)
				if end == zcrce || end == zcrcq {
					break
				}
			}
			if offset == file.Size {
				if err := w.writeHeader(zeof, uint32(offset)); err != nil {
					return false, err
				}
			}
			response, err := w.readHeader()
			if err != nil {
				return false, err
			}
			if response.kind == zrinit && offset == file.Size {
				report(options.Progress, file.Name, file.Size, offset, true)
				break
			}
			if response.kind == zack && int64(response.value) == offset && offset < file.Size {
				newFrame = false
				continue
			}
			if response.kind == zrpos {
				newFrame = true
				// 把重传请求交回外层处理，避免丢失刚刚读取的头。
				offset = int64(response.value)
				if offset > file.Size {
					return false, errors.New("invalid retry offset")
				}
				if _, err := file.Reader.Seek(offset, io.SeekStart); err != nil {
					return false, err
				}
				tries++
				if tries > 10 {
					return false, errors.New("too many ZMODEM retries")
				}
				continue
			}
			return false, fmt.Errorf("unexpected ZMODEM upload response: %d", response.kind)
		}
		break
	}
	if err := w.writeHeader(zfin, 0); err != nil {
		return false, err
	}
	h, err := w.readHeader()
	if err != nil {
		return false, err
	}
	if h.kind != zfin {
		return false, errors.New("receiver did not finish ZMODEM")
	}
	return skipped, w.write([]byte("OO"))
}
