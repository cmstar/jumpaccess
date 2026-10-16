package agentmcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestCancelBeforeSendAndDetach(t *testing.T) {
	opts := poolOptions(t)
	entered, gate := make(chan struct{}), make(chan struct{})
	original := opts.Validate
	e := NewEngine(opts)
	defer e.Shutdown(context.Background())
	c := openTest(t, e, 1, "a")
	e.mu.Lock()
	e.opts.Validate = func(ctx context.Context, r ResolvedTarget) error {
		close(entered)
		select {
		case <-gate:
			return original(ctx, r)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	e.mu.Unlock()
	v, err := e.Execute(context.Background(), executeInput(e, c, 2))
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	v, err = e.CancelExecution(context.Background(), CancelInput{ExecutionID: v.ID})
	if err != nil {
		t.Fatal(err)
	}
	v = awaitExecution(t, e, v.ID)
	if v.Status != "cancelled" || v.DispatchState != "not_sent" || v.RemoteMayContinue {
		t.Fatalf("cancel %+v", v)
	}
	close(gate)
}

func TestRemoteWatchdogThroughSSH(t *testing.T) {
	distro := os.Getenv("JUMPACCESS_TEST_WSL")
	if distro == "" {
		t.Skip("需要本机 Linux")
	}
	var channels atomic.Int32
	e, c := execFixture(t, func(ch ssh.Channel, command string) {
		channels.Add(1)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "--", "sh", "-c", command)
		cmd.Stdout = ch
		cmd.Stderr = ch.Stderr()
		err := cmd.Run()
		code := uint32(0)
		if err != nil {
			code = 125
		}
		exitSSH(ch, code)
	})
	for n, tc := range []struct {
		command string
		timeout int
		status  string
		code    int
	}{
		{"printf hello; printf error >&2; exit 124", 1000, "completed", 124},
		{"sleep 30", 100, "timed_out", -1},
		{"printf success", 1000, "completed", 0},
	} {
		in := executeInput(e, c, 20+n)
		in.Command = tc.command
		in.TimeoutStrategy = "remote_required"
		in.TimeoutMS = tc.timeout
		v, err := e.Execute(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(12 * time.Second)
		for !terminal(v.Status) && time.Now().Before(deadline) {
			v, err = e.GetExecution(context.Background(), GetExecutionInput{ExecutionID: v.ID, Cursor: v.Output.NextCursor, WaitMS: 100})
			if err != nil {
				t.Fatal(err)
			}
		}
		if v.Status != tc.status || v.RemoteMayContinue || (tc.code >= 0 && (v.ExitCode == nil || *v.ExitCode != tc.code)) {
			t.Fatalf("remote result %+v", v)
		}
	}
	if channels.Load() != 4 {
		t.Fatalf("capability probe not cached: channels=%d", channels.Load())
	}
}

func TestLocalDeadlineAndDrain(t *testing.T) {
	gate := make(chan struct{})
	defer close(gate)
	e, c := execFixture(t, func(ch ssh.Channel, _ string) { <-gate })
	in := executeInput(e, c, 2)
	in.TimeoutMS = 50
	v, err := e.Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.CloseConnection(context.Background(), CloseInput{ConnectionID: c.ID})
	assertFault(t, err, "CONNECTION_BUSY")
	closed, err := e.CloseConnection(context.Background(), CloseInput{ConnectionID: c.ID, Mode: "drain"})
	if err != nil || closed.Status != "closed" {
		t.Fatalf("drain %+v %v", closed, err)
	}
	v = awaitExecution(t, e, v.ID)
	if v.Status != "unknown" || v.Reason != "local_deadline" || !v.RemoteMayContinue {
		t.Fatalf("deadline %+v", v)
	}
}

func TestIgnoredSignalRemainsUnknownOnDetach(t *testing.T) {
	gate := make(chan struct{})
	defer close(gate)
	e, c := execFixture(t, func(ch ssh.Channel, _ string) { fmt.Fprint(ch, "started"); <-gate })
	v, err := e.Execute(context.Background(), executeInput(e, c, 2))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		v, _ = e.GetExecution(context.Background(), GetExecutionInput{ExecutionID: v.ID, WaitMS: 20})
		if v.Status == "running" {
			break
		}
	}
	v, err = e.CancelExecution(context.Background(), CancelInput{ExecutionID: v.ID, Mode: "detach"})
	if err != nil {
		t.Fatal(err)
	}
	v = awaitExecution(t, e, v.ID)
	if v.Status != "unknown" || v.CancelConfirmed || !v.RemoteMayContinue {
		t.Fatalf("detach %+v", v)
	}
}

func TestRemoteWatchdogShell(t *testing.T) {
	distro := os.Getenv("JUMPACCESS_TEST_WSL")
	if distro == "" {
		t.Skip("设置 JUMPACCESS_TEST_WSL 后用本机 Linux 验证远端封装")
	}
	for _, tc := range []struct {
		name, command, cwd string
		timeout            int
		want               string
	}{
		{"exit124", "printf out; printf err >&2; exit 124", "/tmp", 1000, `"exit_code":124`},
		{"timeout", "sleep 30", "/tmp", 100, `"timed_out":true`},
		{"context", "printf SHOULD_NOT_RUN", "/does-not-exist-jumpaccess", 1000, `"event":"context_failed"`},
		{"quoting", "printf '%s|%s' \"$VAR\" \"$PWD\"", "/tmp", 1000, "quote'\"$value|/tmp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := remoteCommand(ExecuteInput{Command: tc.command, Cwd: tc.cwd, TimeoutMS: tc.timeout, Env: map[string]string{"VAR": "quote'\"$value"}}, "marker")
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			out, _ := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "--", "sh", "-c", command).CombinedOutput()
			if ctx.Err() != nil || !strings.Contains(string(out), tc.want) || strings.Contains(string(out), "SHOULD_NOT_RUN") {
				t.Fatalf("wrapper %s: %s (%v)", tc.name, out, ctx.Err())
			}
			if tc.name == "exit124" && strings.Contains(string(out), `"timed_out":true`) {
				t.Fatal("exit124 confused with watchdog")
			}
		})
	}
}
