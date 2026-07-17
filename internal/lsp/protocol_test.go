package lsp

import (
	"bufio"
	"bytes"
	"testing"
)

func TestWriteReadMessageRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := writeMessage(&buf, map[string]any{"jsonrpc": "2.0", "method": "ping"}); err != nil {
		t.Fatalf("writeMessage: %v", err)
	}
	// Header must use CRLF and a correct Content-Length.
	if !bytes.HasPrefix(buf.Bytes(), []byte("Content-Length: ")) {
		t.Fatalf("missing Content-Length header: %q", buf.String())
	}
	body, err := readMessage(bufio.NewReader(&buf))
	if err != nil {
		t.Fatalf("readMessage: %v", err)
	}
	want := `{"jsonrpc":"2.0","method":"ping"}`
	if string(body) != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}
