package agentmcp

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

//go:embed download_remote.py
var downloadRemoteScript string

//go:embed transfer_remote.py
var transferRemoteScript string

// 单次 SSH exec 内装配共享监督程序，无需在目标服务器安装模块。
func fileRemoteProgram(script string) string {
	return "import sys,types,base64\n_transfer=types.ModuleType('transfer_remote')\nexec(base64.b64decode('" + base64.StdEncoding.EncodeToString([]byte(transferRemoteScript)) + "'),_transfer.__dict__)\nsys.modules['transfer_remote']=_transfer\n" + script
}

type ownedRemoteFile struct {
	Path     string   `json:"path"`
	Identity []uint64 `json:"identity"`
}
type remoteEntry struct {
	Name       string `json:"name"`
	Source     string `json:"source"`
	Directory  bool   `json:"directory"`
	Size       int64  `json:"size"`
	CapturedAt string `json:"captured_at"`
}
type remoteArtifact struct {
	ownedRemoteFile
	Format             string        `json:"format"`
	CompressionSkipped bool          `json:"compression_skipped"`
	Size               int64         `json:"size"`
	Entries            []remoteEntry `json:"entries"`
	SuggestedName      string        `json:"suggested_name"`
	RootName           string        `json:"root_name"`
	Wrapped            bool          `json:"wrapped"`
}
type remoteReply struct {
	OK   bool            `json:"ok"`
	Code string          `json:"code"`
	Data json.RawMessage `json:"data"`
}
type downloadControl struct {
	Stopped  bool             `json:"stopped"`
	Reason   string           `json:"reason"`
	ExitCode int              `json:"exit_code"`
	Artifact *ownedRemoteFile `json:"artifact"`
}

// stderr 只解析带随机标记的控制消息；sz 的诊断文本不进入任务日志。
type downloadControlWriter struct {
	mu      sync.Mutex
	marker  string
	pending []byte
	control downloadControl
	owned   []ownedRemoteFile
}

func (w *downloadControlWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(b)
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			if len(w.pending)+len(b) <= 16<<10 {
				w.pending = append(w.pending, b...)
			} else {
				w.pending = nil
			}
			break
		}
		line := append(w.pending, b[:i]...)
		w.pending = nil
		b = b[i+1:]
		prefix := w.marker + ":"
		if !bytes.HasPrefix(line, []byte(prefix)) {
			continue
		}
		var v downloadControl
		if json.Unmarshal(line[len(prefix):], &v) != nil {
			continue
		}
		if v.Artifact != nil && len(w.owned) < 8 {
			w.owned = append(w.owned, *v.Artifact)
		}
		if v.Stopped {
			w.control = v
		}
	}
	return n, nil
}
func (w *downloadControlWriter) snapshot() (downloadControl, []ownedRemoteFile) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.control, append([]ownedRemoteFile(nil), w.owned...)
}

type cappedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, fault("REMOTE_METADATA_TOO_LARGE", "远端元数据超出限制")
	}
	return b.Buffer.Write(p)
}

// remoteDownloadCall 只在本任务专用 transport 上运行。EOF 请求取消，stderr 确认停止。
func remoteDownloadCall(ctx context.Context, client *ssh.Client, p map[string]any, consume func(io.Reader, io.Writer) error) (json.RawMessage, []ownedRemoteFile, error) {
	return remoteFileCall(ctx, client, p, consume, downloadRemoteScript, "download")
}

func remoteFileCall(ctx context.Context, client *ssh.Client, p map[string]any, consume func(io.Reader, io.Writer) error, script, operation string) (json.RawMessage, []ownedRemoteFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	marker := rand.Text()
	p["marker"] = marker
	duration := 30 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		duration = time.Until(deadline)
	}
	p["timeout_ms"] = max(1, duration.Milliseconds())
	var inputPayload []byte
	if operation == "upload" {
		var err error
		inputPayload, err = json.Marshal(p)
		if err != nil {
			return nil, nil, err
		}
		if len(inputPayload) > 8<<20 {
			return nil, nil, fault("INVALID_MANIFEST", "上传清单超过元数据上限")
		}
		p = map[string]any{"marker": marker, "timeout_ms": p["timeout_ms"], "input_bytes": len(inputPayload)}
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return nil, nil, err
	}
	program := "import base64;exec(base64.b64decode('" + base64.StdEncoding.EncodeToString([]byte(fileRemoteProgram(script))) + "'))"
	command := "exec python3 -c " + shellQuote(program) + " " + shellQuote(base64.StdEncoding.EncodeToString(payload))
	// SSH channel-open 与 exec 接受也有截止时间；传输仅归当前任务所有。
	startupCtx, stopStartup := context.WithTimeout(ctx, 10*time.Second)
	defer stopStartup()
	stopClose := context.AfterFunc(startupCtx, func() { client.Close() })
	session, err := client.NewSession()
	if err != nil {
		stopClose()
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, fault(strings.ToUpper(operation)+"_CHANNEL_UNAVAILABLE", "无法打开文件传输专用 SSH 通道")
	}
	defer session.Close()
	input, err := session.StdinPipe()
	if err != nil {
		stopClose()
		return nil, nil, err
	}
	output, err := session.StdoutPipe()
	if err != nil {
		stopClose()
		return nil, nil, err
	}
	control := &downloadControlWriter{marker: marker}
	session.Stderr = control
	if err = session.Start(command); err != nil {
		stopClose()
		return nil, nil, fault("REMOTE_STATE_UNKNOWN", "无法确认远端文件传输命令是否已启动")
	}
	if !stopClose() {
		return nil, nil, fault("REMOTE_STATE_UNKNOWN", "文件传输命令启动确认超时")
	}
	stopStartup()
	var body cappedBuffer
	body.limit = 4 << 20
	done := make(chan error, 1)
	readFailed := make(chan error, 1)
	go func() {
		var readErr error
		if len(inputPayload) > 0 {
			_, readErr = input.Write(inputPayload)
		}
		if readErr != nil {
			// 后续停止流程会关闭写方向，并等待远端监督程序确认结束。
		} else if consume != nil {
			readErr = consume(output, input)
		} else {
			_, readErr = io.Copy(&body, output)
		}
		if readErr != nil {
			readFailed <- readErr
		}
		_, _ = io.Copy(io.Discard, output)
		waitErr := session.Wait()
		if readErr != nil {
			done <- readErr
		} else {
			done <- waitErr
		}
	}()
	var operationErr error
	stopRequested := false
	select {
	case operationErr = <-done:
	case <-ctx.Done():
		stopRequested = true
	case <-readFailed:
		stopRequested = true
	}
	if stopRequested {
		// 不发送 SSH signal（网关可能忽略）；关闭写方向，保留读方向接收确认。
		go input.Close()
		timer := time.NewTimer(3 * time.Second)
		select {
		case operationErr = <-done:
			timer.Stop()
		case <-timer.C:
			client.Close()
			<-done
			_, owned := control.snapshot()
			return nil, owned, fault("REMOTE_STATE_UNKNOWN", "停止请求未在期限内得到确认；远端可能仍在运行")
		}
	}
	confirmed, owned := control.snapshot()
	if !confirmed.Stopped {
		return nil, owned, fault("REMOTE_STATE_UNKNOWN", "缺少远端进程结束确认；目标需支持 POSIX、Python 3 和 SSH exec")
	}
	if confirmed.Reason == "cancelled" {
		if ctx.Err() != nil {
			return nil, owned, ctx.Err()
		}
		if operationErr != nil {
			return nil, owned, operationErr
		}
		return nil, owned, context.Canceled
	}
	if confirmed.Reason == "timed_out" {
		return nil, owned, context.DeadlineExceeded
	}
	if consume != nil {
		if operationErr != nil {
			return nil, owned, operationErr
		}
		if confirmed.ExitCode != 0 {
			return nil, owned, fault("ZMODEM_FAILED", "远端 ZMODEM 未正常完成传输")
		}
		return nil, owned, nil
	}
	var reply remoteReply
	if err := json.Unmarshal(body.Bytes(), &reply); err != nil {
		return nil, owned, fault("REMOTE_METADATA_INVALID", "远端文件准备结果无效")
	}
	if !reply.OK {
		code := reply.Code
		switch code {
		case "SOURCE_CHANGED", "SOURCE_UNREADABLE", "UNSUPPORTED_FILE_TYPE", "UNSUPPORTED_PATH", "TOO_MANY_FILES", "STAGING_INSIDE_SOURCE", "COMPRESSION_UNAVAILABLE", "FILE_PERMISSION_DENIED", "TEMPORARY_FILE_CHANGED", "TRANSFER_TOO_LARGE", "TARGET_CONFLICT", "PUBLISH_UNSUPPORTED", "UNSAFE_ARCHIVE", "INTEGRITY_FAILED", "INVALID_MANIFEST", "SFTP_ROOT_UNCONFIRMED":
		default:
			code = "REMOTE_PREPARATION_FAILED"
		}
		return nil, owned, fault(code, "远端文件准备失败："+code)
	}
	if operationErr != nil || confirmed.ExitCode != 0 {
		return nil, owned, fault("REMOTE_PREPARATION_FAILED", "远端文件准备未正常结束")
	}
	return reply.Data, owned, nil
}
func remoteUnknown(err error) bool {
	var f *Fault
	return errors.As(err, &f) && f.Code == "REMOTE_STATE_UNKNOWN"
}
func isDownloadIdentityError(err error) bool {
	var f *Fault
	return errors.As(err, &f) && (f.Code == "IDENTITY_CHANGED" || strings.HasPrefix(f.Code, "AUTH_") || f.Code == "LOGIN_REQUIRED")
}
