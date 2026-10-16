package agentmcp

import (
	"container/list"
	"encoding/base64"
	"unicode/utf8"
)

const outputBlockSize = 32 << 10

type OutputChunk struct {
	Sequence uint64 `json:"seq"`
	Stream   string `json:"stream"`
	Encoding string `json:"encoding"`
	Data     string `json:"data"`
}
type OutputView struct {
	Chunks         []OutputChunk `json:"chunks"`
	NextCursor     uint64        `json:"next_cursor"`
	EarliestCursor uint64        `json:"earliest_cursor"`
	HasMore        bool          `json:"has_more"`
	Gap            bool          `json:"output_gap"`
	Truncated      bool          `json:"output_truncated"`
	Incomplete     bool          `json:"output_incomplete"`
	DroppedBytes   int64         `json:"dropped_bytes"`
}
type outputBlock struct {
	job           *execution
	sequence      uint64
	stream        string
	data          []byte
	local, global *list.Element
}
type executionWriter struct {
	engine *Engine
	job    *execution
	stream string
}

func (w executionWriter) Write(p []byte) (int, error) {
	size := len(p)
	for len(p) > 0 {
		n := min(len(p), outputBlockSize)
		w.engine.appendOutput(w.job, w.stream, p[:n])
		p = p[n:]
	}
	return size, nil
}
func (e *Engine) appendOutput(job *execution, stream string, p []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if terminal(job.view.Status) {
		return
	}
	job.outputSequence++
	b := &outputBlock{job: job, sequence: job.outputSequence, stream: stream, data: append([]byte(nil), p...)}
	b.local = job.output.PushBack(b)
	b.global = e.outputQueue.PushBack(b)
	job.outputBytes += len(p)
	e.outputBytes += len(p)
	for job.outputBytes > e.opts.Limits.OutputBytes {
		e.removeOutput(job.output.Front().Value.(*outputBlock), true)
	}
	for e.outputBytes > e.opts.Limits.TotalOutputBytes {
		e.removeOutput(e.outputQueue.Front().Value.(*outputBlock), true)
	}
	e.notifyExecution(job)
}
func (e *Engine) removeOutput(b *outputBlock, dropped bool) {
	if dropped {
		b.job.droppedBytes += int64(len(b.data))
	}
	b.job.outputBytes -= len(b.data)
	e.outputBytes -= len(b.data)
	b.job.output.Remove(b.local)
	e.outputQueue.Remove(b.global)
	b.data = nil
}
func (e *Engine) outputView(job *execution, cursor uint64, maxBytes int) OutputView {
	out := OutputView{Chunks: []OutputChunk{}, NextCursor: cursor, EarliestCursor: job.outputSequence, Truncated: job.droppedBytes > 0, Incomplete: job.incomplete, DroppedBytes: job.droppedBytes}
	if first := job.output.Front(); first != nil {
		out.EarliestCursor = first.Value.(*outputBlock).sequence - 1
	}
	out.Gap = cursor < out.EarliestCursor
	if out.Gap {
		out.NextCursor = out.EarliestCursor
	}
	size := 0
	for node := job.output.Front(); node != nil; node = node.Next() {
		b := node.Value.(*outputBlock)
		if b.sequence <= cursor {
			continue
		}
		if size+len(b.data) > maxBytes {
			out.HasMore = true
			break
		}
		chunk := OutputChunk{Sequence: b.sequence, Stream: b.stream, Encoding: "utf8", Data: string(b.data)}
		// 跨块字符也以 Base64 原样返回，避免 UTF-8 替换字符损失原始字节。
		if !utf8.Valid(b.data) {
			chunk.Encoding = "base64"
			chunk.Data = base64.StdEncoding.EncodeToString(b.data)
		}
		out.Chunks = append(out.Chunks, chunk)
		out.NextCursor = b.sequence
		size += len(b.data)
	}
	return out
}
