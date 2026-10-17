package sshclient

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"
	"unsafe"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
)

// 使用真实本地 PTY 和 SSH 服务端，覆盖 Runner 自动转发尺寸的完整链路。
func TestRunnerForwardsTerminalResize(t *testing.T) {
	for _, size := range []struct{ columns, rows uint16 }{{132, 42}, {55, 16}} {
		t.Run(fmt.Sprintf("%dx%d", size.columns, size.rows), func(t *testing.T) {
			master, slave := openResizeTestPTY(t)
			if err := unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: 80, Row: 24}); err != nil {
				t.Fatal(err)
			}
			address, _, requests, serverDone := startInteractiveSSHServer(t, "JMS-resize", "synthetic-secret")
			host, portText, _ := net.SplitHostPort(address)
			var port int
			_, _ = fmt.Sscanf(portText, "%d", &port)
			ctx, cancel := context.WithCancel(context.Background())
			runner := Runner{Stdin: slave, Stdout: slave, Stderr: io.Discard, Timeout: time.Second, HostKeyCallback: ssh.InsecureIgnoreHostKey()}
			finished := make(chan error, 1)
			go func() {
				finished <- runner.Run(ctx, jumpserver.ClientConnection{
					Protocol: "ssh", Endpoint: jumpserver.Endpoint{Host: host, Port: port},
					Token: jumpserver.ConnectionCredential{ID: "resize", Value: "synthetic-secret"},
				})
			}()
			t.Cleanup(func() {
				cancel()
				_ = master.Close()
				_ = slave.Close()
			})

			// Runner 进入 raw mode 后再输入，避免 canonical mode 的回显干扰。
			deadline := time.Now().Add(3 * time.Second)
			for {
				state, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TIOCGETA)
				if err != nil {
					t.Fatal(err)
				}
				if state.Lflag&unix.ICANON == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("Runner did not enter raw terminal mode")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if err := unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: size.columns, Row: size.rows}); err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(master, "whoami\n"); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-requests:
				if got.InitialColumns != 80 || got.InitialRows != 24 || got.ResizedColumns != uint32(size.columns) || got.ResizedRows != uint32(size.rows) {
					t.Fatalf("unexpected SSH terminal dimensions: %#v", got)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("SSH server did not receive window-change after local terminal resize")
			}
			select {
			case err := <-finished:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Runner did not stop after the remote session exited")
			}
			if err := <-serverDone; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func openResizeTestPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = master.Close() })
	for _, request := range []uint{unix.TIOCPTYGRANT, unix.TIOCPTYUNLK} {
		if err := unix.IoctlSetInt(int(master.Fd()), request, 0); err != nil {
			t.Fatal(err)
		}
	}
	var name [128]byte
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, master.Fd(), unix.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0])))
	if errno != 0 {
		t.Fatal(errno)
	}
	slave, err := os.OpenFile(unix.ByteSliceToString(name[:]), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	return master, slave
}
