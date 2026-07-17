package lsp

import (
	"bufio"
	"encoding/json"
	"io"
	"testing"
	"time"
)

// scriptedServer replies to an initialize request over pipes, emulating gopls.
func TestInitializeHandshake(t *testing.T) {
	cin, sin := io.Pipe()   // cin = reader, sin = writer (brief's original order - variable names are just confusing)
	sout, cout := io.Pipe() // sout = reader, cout = writer
	c := newClientPipe(writeCloser{sin}, sout)

	go func() {
		r := bufio.NewReader(cin)
		body, err := readMessage(r)
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &req)
		if req.Method != "initialize" {
			t.Errorf("method = %q, want initialize", req.Method)
		}
		resp := map[string]any{
			"jsonrpc": "2.0", "id": req.ID,
			"result": map[string]any{
				"capabilities": map[string]any{"positionEncoding": "utf-8"},
			},
		}
		_ = writeMessage(writeCloser{cout}, resp)
		// Drain any further client messages (e.g. the "initialized"
		// notification Initialize sends after the request) so the client's
		// blocking pipe writes do not deadlock.
		for {
			if _, err := readMessage(r); err != nil {
				return
			}
		}
	}()

	enc, err := c.Initialize("file:///tmp")
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if enc != "utf-8" {
		t.Fatalf("encoding = %q, want utf-8", enc)
	}
	select {
	case <-time.After(time.Second):
	default:
	}
}

// writeCloser adapts an io.Writer to io.WriteCloser for the test seam.
type writeCloser struct{ io.Writer }

func (writeCloser) Close() error { return nil }
