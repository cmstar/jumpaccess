package stdioconn

import (
	"bytes"
	"io"
	"testing"
	"time"
)

func TestConnAdaptsReaderWriterWithoutClosingProcessStreams(t *testing.T) {
	input := bytes.NewBufferString("request")
	var output bytes.Buffer
	closed := 0
	connection := New(input, &output, func() error { closed++; return nil })

	data, err := io.ReadAll(io.LimitReader(connection, 7))
	if err != nil || string(data) != "request" {
		t.Fatalf("Read = %q, %v", data, err)
	}
	if _, err := connection.Write([]byte("response")); err != nil {
		t.Fatal(err)
	}
	if output.String() != "response" {
		t.Fatalf("output = %q", output.String())
	}
	if connection.LocalAddr().Network() != "stdio" || connection.RemoteAddr().String() != "ssh-client" {
		t.Fatalf("addresses = %v %v", connection.LocalAddr(), connection.RemoteAddr())
	}
	if err := connection.SetDeadline(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil || closed != 1 {
		t.Fatalf("second Close = %v, close calls = %d", err, closed)
	}
}
