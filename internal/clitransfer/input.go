package clitransfer

import (
	"bytes"
	"context"
	"io"
	"sync"
)

type inputRouter struct {
	mu         sync.Mutex
	remote     io.Writer
	busy       bool
	prompt     chan []byte
	promptDone chan struct{}
	cancel     context.CancelFunc
	eof        bool
}

func (r *inputRouter) mode(busy bool, prompt chan []byte, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.promptDone != nil {
		close(r.promptDone)
	}
	r.promptDone = nil
	if prompt != nil {
		r.promptDone = make(chan struct{})
	}
	r.busy = busy
	r.prompt = prompt
	r.cancel = cancel
	if busy && r.eof && cancel != nil {
		cancel()
	}
}

func (r *inputRouter) run(ctx context.Context, in io.Reader) {
	for {
		buf := make([]byte, 4096)
		n, err := in.Read(buf)
		if ctx.Err() != nil {
			return
		}
		if n > 0 {
			r.mu.Lock()
			prompt, promptDone, cancel := r.prompt, r.promptDone, r.cancel
			if !r.busy {
				r.mu.Unlock()
				_, _ = r.remote.Write(buf[:n])
			} else {
				r.mu.Unlock()
				if prompt != nil {
					select {
					case prompt <- buf[:n]:
					case <-promptDone:
					case <-ctx.Done():
						return
					}
				}
				if bytes.Contains(buf[:n], []byte{3}) && cancel != nil {
					cancel()
				}
			}
		}
		if err != nil {
			r.mu.Lock()
			r.eof = true
			cancel := r.cancel
			r.mu.Unlock()
			if cancel != nil {
				cancel()
			}
			if closer, ok := r.remote.(interface{ CloseInput() error }); ok {
				_ = closer.CloseInput()
			}
			return
		}
	}
}
