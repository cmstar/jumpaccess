package zmodem

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestTransferRoundTripAndSaveBeforeCompletion(t *testing.T) {
	for _, size := range []int{0, 1, 70000, 2 * 1024 * 1024} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			local, remote := net.Pipe()
			defer local.Close()
			defer remote.Close()
			_ = local.SetDeadline(time.Now().Add(10 * time.Second))
			_ = remote.SetDeadline(time.Now().Add(10 * time.Second))
			data := make([]byte, size)
			for i := range data {
				data[i] = byte(i)
			}
			var received bytes.Buffer
			saved := false
			receiveDone := make(chan error, 1)
			go func() {
				receiveDone <- Receive(bufio.NewReader(remote), remote, ReceiveOptions{
					Open: func(name string, n int64) (Download, error) {
						if name != "binary.dat" || n != int64(size) {
							t.Errorf("metadata: %s %d", name, n)
						}
						return Download{
							Write: func(p []byte) error { _, err := received.Write(p); return err },
							Close: func(complete bool) error { saved = complete; return nil },
						}, nil
					},
					Progress: func(_ string, _, _ int64, complete bool) {
						if complete && !saved {
							t.Error("completed before save")
						}
					},
				})
			}()
			reader := bufio.NewReader(local)
			if _, err := (&wire{in: reader}).readHeader(); err != nil {
				t.Fatal(err)
			}
			skipped, err := Send(reader, local, Upload{"binary.dat", int64(size), bytes.NewReader(data)}, SendOptions{})
			if err != nil || skipped {
				t.Fatalf("send: %v skipped=%v", err, skipped)
			}
			if err := <-receiveDone; err != nil {
				t.Fatal(err)
			}
			if !saved || !bytes.Equal(received.Bytes(), data) {
				t.Fatal("file corrupted")
			}
		})
	}
}

func TestReceiveCleansTruncatedFile(t *testing.T) {
	var stream bytes.Buffer
	w := wire{out: &stream}
	_ = w.writeHeader(zfile, 0)
	_ = w.writePacket([]byte("short\x0010 0\x00"), zcrcw)
	_ = w.writeHeader(zdata, 0)
	_ = w.writePacket([]byte("short"), zcrce)
	closed := false
	err := Receive(bufio.NewReader(&stream), io.Discard, ReceiveOptions{Open: func(string, int64) (Download, error) {
		return Download{
			Write: func([]byte) error { return nil }, Close: func(complete bool) error {
				if complete {
					t.Error("partial file committed")
				}
				closed = true
				return nil
			},
		}, nil
	}})
	if err == nil || !closed {
		t.Fatalf("partial file: %v closed=%v", err, closed)
	}
}
