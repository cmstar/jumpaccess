package zmodem

import (
	"bufio"
	"bytes"
	"testing"
)

func TestWireKnownHeadersAndBinaryPackets(t *testing.T) {
	for _, tc := range []struct {
		h       header
		encoded string
	}{
		{header{kind: zrqinit}, "**\x18B00000000000000\r\n\x11"},
		{header{kind: zrinit, value: 0x23000000}, "**\x18B0100000023be50\r\n\x11"},
	} {
		var output bytes.Buffer
		w := wire{in: bufio.NewReader(bytes.NewBufferString(tc.encoded)), out: &output}
		got, err := w.readHeader()
		if err != nil || got.kind != tc.h.kind || got.value != tc.h.value {
			t.Fatalf("header = %+v, %v", got, err)
		}
		if err := w.writeHeader(tc.h.kind, tc.h.value); err != nil {
			t.Fatal(err)
		}
		if output.String() != tc.encoded {
			t.Fatalf("wire = %q", output.String())
		}
	}
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i)
	}
	for _, end := range []byte{zcrce, zcrcg, zcrcq, zcrcw} {
		var output bytes.Buffer
		w := wire{out: &output}
		if err := w.writePacket(data, end); err != nil {
			t.Fatal(err)
		}
		w.in = bufio.NewReader(&output)
		got, marker, err := w.readPacket(false)
		if err != nil || marker != end || !bytes.Equal(data, got) {
			t.Fatalf("packet = %d bytes, %x, %v", len(got), marker, err)
		}
	}
}

func TestWireRejectsCorruptionAndOversizedPackets(t *testing.T) {
	var output bytes.Buffer
	w := wire{out: &output}
	_ = w.writePacket([]byte("original"), zcrce)
	raw := output.Bytes()
	raw[0] ^= 1
	w.in = bufio.NewReader(bytes.NewReader(raw))
	if _, _, err := w.readPacket(false); err == nil {
		t.Fatal("corrupt packet accepted")
	}
	w.in = bufio.NewReader(bytes.NewReader(bytes.Repeat([]byte{'a'}, maxPacket+1)))
	if _, _, err := w.readPacket(false); err == nil {
		t.Fatal("unbounded packet accepted")
	}
}
