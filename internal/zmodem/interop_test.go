package zmodem

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestJavaScriptPeerInteroperability(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is needed for the independent protocol peer")
	}
	if _, err := os.Stat(filepath.Join("..", "..", "cmd", "jumpaccess", "frontend", "node_modules", "zmodem.js")); err != nil {
		t.Skip("run npm ci in the GUI frontend to install the independent protocol peer")
	}
	for _, mode := range []string{"send", "receive"} {
		for _, size := range []int{0, 70000} {
			t.Run(mode+strconv.Itoa(size), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "node", "testdata/peer.cjs", mode, strconv.Itoa(size))
				output, err := cmd.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				input, err := cmd.StdinPipe()
				if err != nil {
					t.Fatal(err)
				}
				var diagnostic bytes.Buffer
				cmd.Stderr = &diagnostic
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				defer cmd.Process.Kill()
				reader := bufio.NewReader(output)
				if _, err := (&wire{in: reader}).readHeader(); err != nil {
					t.Fatal(err)
				}
				data := make([]byte, size)
				for i := range data {
					data[i] = byte(i)
				}
				if mode == "receive" {
					_, err = Send(reader, input, Upload{"binary.dat", int64(size), bytes.NewReader(data)}, SendOptions{})
				} else {
					var saved bytes.Buffer
					err = Receive(reader, input, ReceiveOptions{Open: func(string, int64) (Download, error) {
						return Download{Write: func(p []byte) error { _, e := saved.Write(p); return e }, Close: func(bool) error { return nil }}, nil
					}})
					if !bytes.Equal(saved.Bytes(), data) {
						t.Error("download data differs")
					}
				}
				tail, _ := io.ReadAll(reader)
				processErr := cmd.Wait()
				if err != nil || processErr != nil {
					t.Fatalf("protocol=%v process=%v\n%s", err, processErr, diagnostic.String())
				}
				if string(tail) != "shell$ " {
					t.Fatalf("terminal tail = %q", tail)
				}
			})
		}
	}
}
