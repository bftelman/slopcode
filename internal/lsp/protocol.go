// Package lsp is a minimal JSON-RPC 2.0 client for Language Server Protocol
// servers. It has no UI dependencies.
package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Position is a zero-based LSP line/character location. Character offsets are
// in the negotiated position encoding (UTF-16 code units by default).
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// TextEdit replaces the text in Range with NewText.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// Range is a start/end position pair.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// CompletionItem is one candidate returned by textDocument/completion.
type CompletionItem struct {
	Label      string    `json:"label"`
	Detail     string    `json:"detail"`
	InsertText string    `json:"insertText"`
	Kind       int       `json:"kind"`
	TextEdit   *TextEdit `json:"textEdit"`
}

// writeMessage frames v as a JSON-RPC message with a Content-Length header.
func writeMessage(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// readMessage reads one framed JSON-RPC message body from r.
func readMessage(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length: %q", line)
			}
			length = n
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("message without Content-Length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

type rpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("lsp error %d: %s", e.Code, e.Message) }

// serverMessage is the minimal shape used to route an incoming message: a
// response (has id, no method) vs a server-initiated request/notification.
type serverMessage struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}
