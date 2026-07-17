package lsp

import (
	"bufio"
	"context"
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

// TestCallAfterTransportCloseReturnsError verifies that once the transport
// has died (readLoop hit EOF/an error), any subsequent call() returns an
// error immediately instead of blocking forever waiting for a response that
// will never come.
func TestCallAfterTransportCloseReturnsError(t *testing.T) {
	pr, pw := io.Pipe()
	c := newClientPipe(writeCloser{io.Discard}, pr)

	// Kill the read end so readLoop sees EOF and marks the client closed.
	if err := pw.Close(); err != nil {
		t.Fatalf("pw.Close: %v", err)
	}

	type result struct {
		err error
	}
	resCh := make(chan result, 1)
	deadline := time.After(2 * time.Second)

	go func() {
		for {
			_, err := c.call(context.Background(), "ping", nil)
			if err != nil {
				resCh <- result{err: err}
				return
			}
			select {
			case <-deadline:
				resCh <- result{err: nil}
				return
			default:
			}
		}
	}()

	select {
	case res := <-resCh:
		if res.err == nil {
			t.Fatal("call after transport close: got nil error, want non-nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("call hung after transport close")
	}
}
