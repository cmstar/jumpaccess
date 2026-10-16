package agentmcp

import "strings"

// 仅移除本执行随机标识包围的控制记录，普通 stderr 保持原始字节。
type controlWriter struct {
	engine  *Engine
	job     *execution
	pending string
}

func (w *controlWriter) Write(p []byte) (int, error) {
	w.pending += string(p)
	prefix := "\x1e" + w.job.marker + ":"
	for len(w.pending) > 0 {
		index := strings.Index(w.pending, prefix)
		if index < 0 {
			keep := min(len(prefix)-1, len(w.pending))
			for keep > 0 && !strings.HasSuffix(w.pending, prefix[:keep]) {
				keep--
			}
			w.output(w.pending[:len(w.pending)-keep])
			w.pending = w.pending[len(w.pending)-keep:]
			break
		}
		if index > 0 {
			w.output(w.pending[:index])
			w.pending = w.pending[index:]
		}
		end := strings.IndexByte(w.pending[len(prefix):], 0x1f)
		if end < 0 {
			if len(w.pending) > 4096 {
				w.output(w.pending[:1])
				w.pending = w.pending[1:]
				continue
			}
			break
		}
		end += len(prefix)
		payload := w.pending[len(prefix):end]
		if payload == "context_failed" {
			w.engine.mu.Lock()
			w.job.contextFailed = true
			w.engine.mu.Unlock()
		} else {
			w.output(w.pending[:end+1])
		}
		w.pending = w.pending[end+1:]
	}
	return len(p), nil
}
func (w *controlWriter) output(s string) {
	if s != "" {
		_, _ = (executionWriter{w.engine, w.job, "stderr"}).Write([]byte(s))
	}
}
func (w *controlWriter) flush() { w.output(w.pending); w.pending = "" }
