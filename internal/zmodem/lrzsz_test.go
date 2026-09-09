package zmodem

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// 默认在 PATH 中找真实 lrzsz；Windows 可显式指定 WSL 测试包目录，不安装系统软件。
func TestLrzszInteroperability(t *testing.T) {
	wslDir := os.Getenv("JUMPACCESS_TEST_LRZSZ_WSL_DIR")
	if wslDir == "" {
		if _, err := exec.LookPath("rz"); err != nil {
			t.Skip("optional: install lrzsz or set JUMPACCESS_TEST_LRZSZ_WSL_DIR")
		}
	}
	linuxPath := func(path string) string {
		path = filepath.ToSlash(path)
		if runtime.GOOS == "windows" && len(path) > 2 && path[1] == ':' {
			return "/mnt/" + strings.ToLower(path[:1]) + path[2:]
		}
		return path
	}
	for _, mode := range []string{"rz", "sz"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			dir := t.TempDir()
			data := make([]byte, 2*1024*1024+257)
			for i := range data {
				data[i] = byte(i)
			}
			path := filepath.Join(dir, "binary.dat")
			if mode == "sz" {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"-b", "-e", "-q"}
			if mode == "sz" {
				args = append(args, "binary.dat")
			}
			var cmd *exec.Cmd
			if wslDir != "" {
				all := append([]string{"-d", "Ubuntu-22.04", "--cd", linuxPath(dir), "--", wslDir + "/" + mode}, args...)
				cmd = exec.CommandContext(ctx, "wsl", all...)
			} else {
				cmd = exec.CommandContext(ctx, mode, args...)
				cmd.Dir = dir
			}
			output, _ := cmd.StdoutPipe()
			input, _ := cmd.StdinPipe()
			var diagnostic bytes.Buffer
			cmd.Stderr = &diagnostic
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Kill()
			reader := bufio.NewReader(output)
			if _, err := (&wire{in: reader}).readHeader(); err != nil {
				_ = cmd.Wait()
				t.Fatalf("initial: %v %s", err, diagnostic.String())
			}
			var err error
			if mode == "rz" {
				_, err = Send(reader, input, Upload{"binary.dat", int64(len(data)), bytes.NewReader(data)}, SendOptions{})
			} else {
				var got bytes.Buffer
				err = Receive(reader, input, ReceiveOptions{Open: func(string, int64) (Download, error) {
					return Download{Write: func(p []byte) error { _, err := got.Write(p); return err }, Close: func(bool) error { return nil }}, nil
				}})
				if !bytes.Equal(got.Bytes(), data) {
					t.Error("download differs")
				}
			}
			if err != nil {
				_ = cmd.Process.Kill()
			}
			_, _ = io.Copy(io.Discard, reader)
			processErr := cmd.Wait()
			if err != nil || processErr != nil {
				t.Fatalf("protocol=%v process=%v\n%s", err, processErr, diagnostic.String())
			}
			if mode == "rz" {
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, data) {
					t.Fatalf("upload differs: %v", err)
				}
			}
		})
	}
}
