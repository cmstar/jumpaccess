package clitransfer

import (
	"context"
	"errors"
	"io"
	"time"
)

var errIdle = errors.New("transfer timed out")

type readResult struct {
	data []byte
	err  error
}
type stream struct {
	ctx         context.Context
	incoming    <-chan readResult
	rest        []byte
	terminalErr error
	timeout     time.Duration
}

func pump(ctx context.Context, r io.Reader) <-chan readResult {
	ch := make(chan readResult, 1)
	go func() {
		defer close(ch)
		for {
			data := make([]byte, 32*1024)
			n, err := r.Read(data)
			select {
			case ch <- readResult{data[:n], err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return ch
}

func (s *stream) next(timeout time.Duration) ([]byte, error) {
	if len(s.rest) > 0 {
		data := s.rest
		s.rest = nil
		return data, nil
	}
	if s.terminalErr != nil {
		return nil, s.terminalErr
	}
	var timer *time.Timer
	var timeoutC <-chan time.Time
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		defer timer.Stop()
		timeoutC = timer.C
	}
	select {
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case <-timeoutC:
		return nil, errIdle
	case result, ok := <-s.incoming:
		if !ok {
			return nil, io.EOF
		}
		s.terminalErr = result.err
		if len(result.data) > 0 {
			return result.data, nil
		}
		return nil, result.err
	}
}

func (s *stream) ReadByte() (byte, error) {
	for len(s.rest) == 0 {
		data, err := s.next(s.timeout)
		if err != nil {
			return 0, err
		}
		s.rest = data
	}
	b := s.rest[0]
	s.rest = s.rest[1:]
	return b, nil
}
