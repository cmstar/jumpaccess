// Package zmodem 实现交互 SSH 使用的 ZMODEM 文件收发，不依赖外部命令。
package zmodem

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

const (
	zrqinit   byte = 0
	zrinit    byte = 1
	zsinit    byte = 2
	zack      byte = 3
	zfile     byte = 4
	zskip     byte = 5
	znak      byte = 6
	zabort    byte = 7
	zfin      byte = 8
	zrpos     byte = 9
	zdata     byte = 10
	zeof      byte = 11
	zferr     byte = 12
	zcrc      byte = 13
	zcan      byte = 16
	zdle      byte = 0x18
	zcrce     byte = 'h'
	zcrcg     byte = 'i'
	zcrcq     byte = 'j'
	zcrcw     byte = 'k'
	maxPacket      = 64 * 1024
)

var ErrCancelled = errors.New("transfer cancelled")
var errCRC = errors.New("ZMODEM checksum mismatch")

type header struct {
	kind  byte
	value uint32
	crc32 bool
}
type wire struct {
	in  io.ByteReader
	out io.Writer
}

func crc16(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// InitialHeader 只接受完整且 CRC 有效的标准起始帧；相似文本不吞掉。
func InitialHeader(data []byte) (upload bool, ok bool) {
	if len(data) != 20 || string(data[:4]) != "**\x18B" || data[18] != '\r' || data[19]&0x7f != '\n' {
		return false, false
	}
	var raw [7]byte
	if _, err := hex.Decode(raw[:], data[4:18]); err != nil || crc16(raw[:5]) != binary.BigEndian.Uint16(raw[5:]) {
		return false, false
	}
	return raw[0] == zrinit, raw[0] == zrinit || raw[0] == zrqinit
}

// ReceiverWindow 读取 rz 声明的接收缓冲；零表示可流式接收。
func ReceiverWindow(data []byte) int {
	if upload, ok := InitialHeader(data); !ok || !upload {
		return 0
	}
	var raw [2]byte
	_, _ = hex.Decode(raw[:], data[6:10])
	return int(binary.LittleEndian.Uint16(raw[:]))
}

func (w *wire) write(data []byte) error {
	n, err := w.out.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

func (w *wire) writeHeader(kind byte, value uint32) error {
	var raw [7]byte
	raw[0] = kind
	binary.LittleEndian.PutUint32(raw[1:5], value)
	binary.BigEndian.PutUint16(raw[5:], crc16(raw[:5]))
	data := append([]byte("**\x18B"), []byte(hex.EncodeToString(raw[:]))...)
	data = append(data, '\r', '\n')
	if kind != zfin && kind != zack {
		data = append(data, 0x11)
	}
	return w.write(data)
}

func escape(dst []byte, b byte) []byte {
	// 统一转义控制字符及高位对应值，包含可能被终端处理的 CR/XON/XOFF。
	if b&0x7f < 0x20 {
		return append(dst, zdle, b^0x40)
	}
	return append(dst, b)
}

func (w *wire) writePacket(data []byte, end byte) error {
	if len(data) > maxPacket {
		return errors.New("ZMODEM packet too large")
	}
	payload := append(append([]byte(nil), data...), end)
	crc := crc16(payload)
	encoded := make([]byte, 0, len(data)*2+8)
	for _, b := range data {
		encoded = escape(encoded, b)
	}
	encoded = append(encoded, zdle, end)
	encoded = escape(encoded, byte(crc>>8))
	encoded = escape(encoded, byte(crc))
	if end == zcrcw {
		encoded = append(encoded, 0x11)
	}
	return w.write(encoded)
}

func (w *wire) rawByte() (byte, error) {
	for {
		b, err := w.in.ReadByte()
		if err != nil {
			return 0, err
		}
		if b != 0x11 && b != 0x13 && b != 0x91 && b != 0x93 {
			return b, nil
		}
	}
}

func (w *wire) decoded() (b byte, marker bool, err error) {
	b, err = w.rawByte()
	if err != nil || b != zdle {
		return
	}
	b, err = w.rawByte()
	if err != nil {
		return
	}
	if b == zdle {
		return 0, false, ErrCancelled
	}
	if b >= zcrce && b <= zcrcw {
		return b, true, nil
	}
	if b == 'l' {
		return 0x7f, false, nil
	}
	if b == 'm' {
		return 0xff, false, nil
	}
	if b&0x60 == 0x40 {
		return b ^ 0x40, false, nil
	}
	return 0, false, fmt.Errorf("invalid ZMODEM escape: %02x", b)
}

func (w *wire) readHeader() (header, error) {
	state := 0
	for range 1024 * 1024 {
		b, err := w.rawByte()
		if err != nil {
			return header{}, err
		}
		switch state {
		case 0:
			if b == '*' {
				state = 1
			} else if b == zdle {
				state = 3
			}
		case 1:
			if b == zdle {
				state = 2
			} else if b != '*' {
				state = 0
			}
		case 3:
			if b == zdle {
				return header{}, ErrCancelled
			}
			state = 0
		case 2:
			if b != 'A' && b != 'B' && b != 'C' {
				state = 0
				continue
			}
			crc32Mode := b == 'C'
			length := 7
			if crc32Mode {
				length = 9
			}
			raw := make([]byte, length)
			if b == 'B' {
				encoded := make([]byte, 14)
				for i := range encoded {
					encoded[i], err = w.rawByte()
					if err != nil {
						return header{}, err
					}
				}
				if _, err = hex.Decode(raw, encoded); err != nil {
					return header{}, err
				}
				cr, err := w.rawByte()
				if err != nil {
					return header{}, err
				}
				lf, err := w.rawByte()
				if err != nil {
					return header{}, err
				}
				if cr != '\r' || lf&0x7f != '\n' {
					return header{}, errors.New("invalid ZMODEM header ending")
				}
			} else {
				for i := range raw {
					value, marker, err := w.decoded()
					if err != nil {
						return header{}, err
					}
					if marker {
						return header{}, errors.New("unexpected ZMODEM marker")
					}
					raw[i] = value
				}
			}
			if crc32Mode {
				if crc32.ChecksumIEEE(raw[:5]) != binary.LittleEndian.Uint32(raw[5:]) {
					return header{}, errCRC
				}
			} else if crc16(raw[:5]) != binary.BigEndian.Uint16(raw[5:]) {
				return header{}, errCRC
			}
			h := header{raw[0], binary.LittleEndian.Uint32(raw[1:5]), crc32Mode}
			if h.kind == zabort || h.kind == zcan || h.kind == zferr {
				return header{}, ErrCancelled
			}
			return h, nil
		}
	}
	return header{}, errors.New("too much data without a ZMODEM header")
}

func (w *wire) readPacket(crc32Mode bool) ([]byte, byte, error) {
	data := make([]byte, 0, 1024)
	for len(data) <= maxPacket {
		b, marker, err := w.decoded()
		if err != nil {
			return nil, 0, err
		}
		if !marker {
			data = append(data, b)
			continue
		}
		length := 2
		if crc32Mode {
			length = 4
		}
		var crc [4]byte
		for i := 0; i < length; i++ {
			v, m, err := w.decoded()
			if err != nil {
				return nil, 0, err
			}
			if m {
				return nil, 0, errors.New("invalid ZMODEM checksum")
			}
			crc[i] = v
		}
		checked := append(data, b)
		if crc32Mode {
			if crc32.ChecksumIEEE(checked) != binary.LittleEndian.Uint32(crc[:]) {
				return nil, 0, errCRC
			}
		} else if crc16(checked) != binary.BigEndian.Uint16(crc[:2]) {
			return nil, 0, errCRC
		}
		return data, b, nil
	}
	return nil, 0, errors.New("ZMODEM packet too large")
}

// Cancel 要求远端结束协议，避免把本地后续键盘输入当成文件数据。
func Cancel(out io.Writer) error {
	return (&wire{out: out}).write([]byte("\x18\x18\x18\x18\x18\x18\x18\x18\b\b\b\b\b\b\b\b\b\b"))
}
