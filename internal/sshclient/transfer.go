package sshclient

import (
	"context"
	"fmt"
	"io"
	"strings"
)

const transferProbeCommand = `printf 'JUMPACCESS_ZMODEM:'; if command -v rz >/dev/null 2>&1; then printf 1; else printf 0; fi; if command -v sz >/dev/null 2>&1; then printf 1; else printf 0; fi; printf ':END\n'`

// ProbeTransferCommands 的 exec 与终端共享身份和 transport，失败只表示无法探测。
func (s *Session) ProbeTransferCommands(ctx context.Context) (bool, bool, error) {
	type result struct {
		upload, download bool
		err              error
	}
	done := make(chan result, 1)
	go func() {
		remote, err := s.client.NewSession()
		if err != nil {
			done <- result{err: err}
			return
		}
		defer remote.Close()
		finished := make(chan struct{})
		defer close(finished)
		go func() {
			select {
			case <-ctx.Done():
				_ = remote.Close()
			case <-finished:
			}
		}()
		var output boundedProbeOutput
		remote.Stdout = &output
		remote.Stderr = io.Discard
		err = remote.Run(transferProbeCommand)
		if err != nil {
			done <- result{err: err}
			return
		}
		upload, download, err := parseTransferProbe(output.String())
		done <- result{upload, download, err}
	}()
	select {
	case <-ctx.Done():
		return false, false, ctx.Err()
	case result := <-done:
		return result.upload, result.download, result.err
	}
}

type boundedProbeOutput struct{ strings.Builder }

func (w *boundedProbeOutput) Write(data []byte) (int, error) {
	if w.Len()+len(data) > 4096 {
		return 0, fmt.Errorf("command probe output too large")
	}
	return w.Builder.Write(data)
}

func parseTransferProbe(output string) (bool, bool, error) {
	value := strings.TrimSpace(output)
	for _, flags := range []string{"00", "01", "10", "11"} {
		if value == "JUMPACCESS_ZMODEM:"+flags+":END" {
			return flags[0] == '1', flags[1] == '1', nil
		}
	}
	return false, false, fmt.Errorf("unexpected command probe response")
}
