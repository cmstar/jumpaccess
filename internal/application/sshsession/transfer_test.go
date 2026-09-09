package sshsession

import (
	"bytes"
	"context"
	"encoding/base64"
	"github.com/cmstar/jumpaccess/internal/sshclient"
	"golang.org/x/crypto/ssh"
	"io"
	"testing"
	"time"
)

func TestBinaryInputPreservesAllOctets(t *testing.T) {
	terminal := newFakeTerminalSession()
	m := &Manager{sessions: map[string]*managedSession{"test": {state: StateEvent{Status: StatusActive}, terminal: terminal}}}
	want := make([]byte, 256)
	for i := range want {
		want[i] = byte(i)
	}
	if err := m.WriteBinary("test", base64.StdEncoding.EncodeToString(want)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(terminal.writes.Bytes(), want) {
		t.Fatal("binary input was corrupted")
	}
	if err := m.WriteBinary("test", "not base64!"); err == nil {
		t.Fatal("accepted invalid base64")
	}
}

func TestTransferPausesTextInputButAllowsProtocolAndCanResume(t *testing.T) {
	terminal := newFakeTerminalSession()
	m := &Manager{sessions: map[string]*managedSession{"test": {state: StateEvent{Status: StatusActive}, terminal: terminal}}}
	if err := m.SetTransferActive("test", true); err != nil {
		t.Fatal(err)
	}
	if !m.HasActiveTransfers() {
		t.Fatal("active transfer missing")
	}
	if err := m.Write("test", "unexpected key"); err == nil {
		t.Fatal("input mixed with transfer")
	}
	if err := m.WriteBinary("test", "AA=="); err != nil {
		t.Fatal(err)
	}
	_ = m.SetTransferActive("test", false)
	if err := m.Write("test", "pwd\r"); err != nil {
		t.Fatal(err)
	}
}

func TestBinaryOutputWaitsForAcknowledgementAndCloseUnblocks(t *testing.T) {
	states := make(chan StateEvent, 10)
	events := make(chan OutputEvent, 10)
	var writer io.Writer
	terminal := newFakeTerminalSession()
	m := &Manager{
		Prepare:         &fakePreparer{},
		HostKeyCallback: func(context.Context) (ssh.HostKeyCallback, error) { return ssh.InsecureIgnoreHostKey(), nil },
		Open: func(_ context.Context, options sshclient.OpenOptions) (TerminalSession, error) {
			writer = options.Stdout
			return terminal, nil
		},
		EmitState: func(event StateEvent) { states <- event }, EmitOutput: func(event OutputEvent) { events <- event },
		BinaryOutput: true, BatchSize: 1,
	}
	state, err := m.Start(context.Background(), StartRequest{Target: "asset", Columns: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, states, state.ID, StatusActive)
	written := make(chan struct{})
	want := bytes.Repeat([]byte{0, 128, 255}, 20_000)
	go func() { _, _ = writer.Write(want); close(written) }()
	first := <-events
	got, err := base64.StdEncoding.DecodeString(first.Data)
	if err != nil || first.Encoding != "base64" || !bytes.Equal(got, want[:len(got)]) {
		t.Fatal("binary output corrupted")
	}
	m.AcknowledgeOutput(state.ID, first.Sequence+1)
	select {
	case <-events:
		t.Fatal("wrong acknowledgement released output")
	case <-time.After(20 * time.Millisecond):
	}
	m.AcknowledgeOutput(state.ID, first.Sequence)
	second := <-events
	if second.Sequence != first.Sequence+1 {
		t.Fatal("output sequence not monotonic")
	}
	if err := m.Close(state.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-written:
	case <-time.After(time.Second):
		t.Fatal("close did not unblock output")
	}
}
