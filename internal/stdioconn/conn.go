package stdioconn

import (
	"io"
	"net"
	"sync"
	"time"
)

type Conn struct {
	reader   io.Reader
	writer   io.Writer
	close    func() error
	once     sync.Once
	closeErr error
}

func New(reader io.Reader, writer io.Writer, closeFunc func() error) *Conn {
	return &Conn{reader: reader, writer: writer, close: closeFunc}
}

func (c *Conn) Read(buffer []byte) (int, error)  { return c.reader.Read(buffer) }
func (c *Conn) Write(buffer []byte) (int, error) { return c.writer.Write(buffer) }

func (c *Conn) Close() error {
	c.once.Do(func() {
		if c.close != nil {
			c.closeErr = c.close()
		}
	})
	return c.closeErr
}

func (c *Conn) LocalAddr() net.Addr  { return address("jumpaccess") }
func (c *Conn) RemoteAddr() net.Addr { return address("ssh-client") }

// Process standard streams do not provide deadline controls. SSH cancellation
// closes the surrounding connection instead.
func (c *Conn) SetDeadline(time.Time) error      { return nil }
func (c *Conn) SetReadDeadline(time.Time) error  { return nil }
func (c *Conn) SetWriteDeadline(time.Time) error { return nil }

type address string

func (address) Network() string  { return "stdio" }
func (a address) String() string { return string(a) }
