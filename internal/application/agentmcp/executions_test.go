package agentmcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func execFixture(t *testing.T, handle func(ssh.Channel, string)) (*Engine, ConnectionView) {
	opts := poolOptions(t)
	opts.Dial = testSSH(t, func(ch ssh.Channel, requests <-chan *ssh.Request) {
		defer ch.Close()
		for req := range requests {
			if req.Type == "exec" {
				var command struct{ Command string }
				_ = ssh.Unmarshal(req.Payload, &command)
				_ = req.Reply(true, nil)
				handle(ch, command.Command)
				return
			}
			_ = req.Reply(false, nil)
		}
	})
	e := NewEngine(opts)
	t.Cleanup(func() { e.Shutdown(context.Background()) })
	return e, openTest(t, e, 1, "a")
}
func exitSSH(ch ssh.Channel, code uint32) {
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{code}))
}
func executeInput(e *Engine, c ConnectionView, n int) ExecuteInput {
	zero := 0
	return ExecuteInput{ConnectionID: c.ID, RequestKey: requestKey(e.RequestEpoch(), n), Command: "printf ok", Cwd: "/tmp", TimeoutStrategy: "local", WaitMS: &zero}
}
func awaitExecution(t *testing.T, e *Engine, id string) ExecutionView {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		v, err := e.GetExecution(context.Background(), GetExecutionInput{ExecutionID: id, WaitMS: 100})
		if err != nil {
			t.Fatal(err)
		}
		if terminal(v.Status) {
			return v
		}
	}
	t.Fatal("execution did not finish")
	return ExecutionView{}
}

func TestExecutionRetryBusyExitAndHTTPIndependence(t *testing.T) {
	gate := make(chan struct{})
	var sent atomic.Int32
	e, c := execFixture(t, func(ch ssh.Channel, _ string) {
		sent.Add(1)
		<-gate
		fmt.Fprint(ch, "stdout")
		fmt.Fprint(ch.Stderr(), "stderr")
		exitSSH(ch, 7)
	})
	in := executeInput(e, c, 2)
	ctx, cancel := context.WithCancel(context.Background())
	a, err := e.Execute(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	b, err := e.Execute(context.Background(), in)
	if err != nil || a.ID != b.ID {
		t.Fatalf("retry %+v %v", b, err)
	}
	_, err = e.Execute(context.Background(), executeInput(e, c, 3))
	assertFault(t, err, "CONNECTION_BUSY")
	close(gate)
	v := awaitExecution(t, e, a.ID)
	if v.Status != "completed" || v.ExitCode == nil || *v.ExitCode != 7 || v.DispatchState != "acknowledged" || v.RemoteMayContinue {
		t.Fatalf("result %+v", v)
	}
	streams := map[string]string{}
	for _, chunk := range v.Output.Chunks {
		streams[chunk.Stream] += chunk.Data
	}
	if streams["stdout"] != "stdout" || streams["stderr"] != "stderr" || sent.Load() != 1 {
		t.Fatalf("streams=%v sends=%d", streams, sent.Load())
	}
	_, err = e.Execute(context.Background(), ExecuteInput{ConnectionID: c.ID, RequestKey: requestKey(1, 4), Command: "x", Cwd: "relative"})
	assertFault(t, err, "INVALID_ARGUMENT")
}

func TestExecutionOutputLimitsAndExpiredRetry(t *testing.T) {
	e, c := execFixture(t, func(ch ssh.Channel, _ string) { fmt.Fprint(ch, strings.Repeat("x", 100000)); exitSSH(ch, 0) })
	e.mu.Lock()
	e.opts.Limits.OutputBytes = 40000
	e.opts.Limits.TotalOutputBytes = 40000
	e.opts.Limits.Executions = 1
	e.mu.Unlock()
	in := executeInput(e, c, 2)
	v, err := e.Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	v = awaitExecution(t, e, v.ID)
	if !v.Output.Truncated || !v.Output.Gap || v.Output.DroppedBytes == 0 {
		t.Fatalf("unreported gap %+v", v.Output)
	}
	firstID := v.ID
	v, err = e.Execute(context.Background(), executeInput(e, c, 3))
	if err != nil {
		t.Fatal(err)
	}
	awaitExecution(t, e, v.ID)
	_, err = e.Execute(context.Background(), in)
	assertFault(t, err, "EXECUTION_EXPIRED")
	if _, err = e.GetExecution(context.Background(), GetExecutionInput{ExecutionID: firstID}); err == nil {
		t.Fatal("expired result survived")
	}
}

func TestExecutionDisconnectNeverResends(t *testing.T) {
	var sent atomic.Int32
	e, c := execFixture(t, func(ch ssh.Channel, _ string) { sent.Add(1); fmt.Fprint(ch, "partial"); ch.Close() })
	in := executeInput(e, c, 2)
	v, err := e.Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	v = awaitExecution(t, e, v.ID)
	if v.Status != "unknown" || !v.RemoteMayContinue || v.ExitCode != nil || !v.Output.Incomplete {
		t.Fatalf("false certainty %+v", v)
	}
	_, err = e.Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if sent.Load() != 1 {
		t.Fatal("command replayed")
	}
}

func TestControlRecordsAndBinaryOutputAreLossless(t *testing.T) {
	e, c := execFixture(t, func(ch ssh.Channel, command string) {
		start := strings.Index(command, "\x1e")
		end := strings.Index(command, "\x1f")
		if start < 0 || end < start {
			t.Error("missing context control marker")
			return
		}
		marker := command[start : end+1]
		for _, b := range []byte("before" + marker + "after") {
			_, _ = ch.Stderr().Write([]byte{b})
		}
		_, _ = ch.Write([]byte{0xff, 0, 0x81})
		exitSSH(ch, 125)
	})
	v, err := e.Execute(context.Background(), executeInput(e, c, 2))
	if err != nil {
		t.Fatal(err)
	}
	v = awaitExecution(t, e, v.ID)
	if v.Status != "failed" || v.Error.Code != "EXECUTION_CONTEXT_FAILED" || v.RemoteState != "not_started" || v.ExitCode != nil {
		t.Fatalf("context %+v", v)
	}
	streams := map[string][]byte{}
	for _, b := range v.Output.Chunks {
		data := []byte(b.Data)
		if b.Encoding == "base64" {
			data, _ = base64.StdEncoding.DecodeString(b.Data)
		}
		streams[b.Stream] = append(streams[b.Stream], data...)
	}
	if string(streams["stderr"]) != "beforeafter" || !bytes.Equal(streams["stdout"], []byte{0xff, 0, 0x81}) {
		t.Fatalf("corrupt output %v", streams)
	}
}
