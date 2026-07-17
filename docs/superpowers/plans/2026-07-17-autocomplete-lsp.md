# Autocomplete + LSP Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add automatic code completion to slopcode via an extensible, provider-based engine whose first-class provider connects to LSP servers (gopls for Go).

**Architecture:** A UI-free `completion` engine runs as a single owner goroutine (actor model), debounces edits, and dispatches to a `Provider`. The LSP provider wraps a minimal JSON-RPC client in a new UI-free `lsp` package. Results flow back on a channel; the editor bridges them to `tcell` via `PostEvent`. The buffer stays UI-thread-only; snapshots are handed to the engine as immutable values.

**Tech Stack:** Go 1.25+, `github.com/gdamore/tcell/v2`, `encoding/json`, `os/exec`, stdlib only for `lsp` and `completion`. gopls as the external LSP server.

Design of record: `docs/superpowers/specs/2026-07-17-autocomplete-lsp-design.md`.

## Global Constraints

- Go 1.25+. Standard tooling only: `gofmt` and `go vet` must be clean; run `go test ./...` before claiming any task done.
- Dependency rule (from `AGENTS.md`): `lsp` and `completion` must **not** import `tcell`, `buffer`, or `render`. `buffer`, `fileio`, `filebrowser`, `theme` stay UI-free. Only `editor` wires everything.
- The `buffer` is **UI-thread-only**; never access it from a goroutine. Hand the engine immutable `Document` values.
- Completion is **best-effort and non-fatal**: a missing/failing gopls must never crash, block, or panic the editor.
- LSP uses **full-text sync** (`TextDocumentSyncKind.Full`). No incremental sync.
- Automatic trigger with a **150 ms** default debounce. Identifier char = `[A-Za-z0-9_]`. Trigger char = `.`.
- Document versioning is **editor-owned** (`buffer` gets no version field). Version bumps on every content mutation including undo/redo.
- Position encoding: advertise preferred `utf-8` via `general.positionEncodings`; read back the server's chosen `capabilities.positionEncoding`; fall back to `utf-16`. ASCII-correct only (tracked as R4 in the conventions spec).
- Commits: Conventional Commits with a package scope (`feat(lsp):`, `feat(completion):`, `feat(render):`, `feat(editor):`, `test(...)`). End messages with the repo's `Co-Authored-By` trailer.

## File structure

| Path | Responsibility |
| --- | --- |
| `internal/lsp/protocol.go` | JSON-RPC + LSP wire types; message framing (read/write). |
| `internal/lsp/client.go` | Subprocess JSON-RPC client: start, reader goroutine, request/response matching, initialize, doc sync, completion, shutdown. |
| `internal/completion/completion.go` | Public types: `Item`, `Kind`, `Document`, `Position`, `Result`, `Provider`, `DocSink`. |
| `internal/completion/engine.go` | Actor `Engine`: debounce, dispatch, cancellation, `Results()` channel. |
| `internal/completion/registry.go` | `Registry` (ext→provider factory) + `LSPRegistry` for gopls. |
| `internal/completion/lspprovider.go` | `Provider`+`DocSink` backed by an `lspConn` (satisfied by `*lsp.Client`). |
| `internal/completion/fake_test.go` | Shared in-package fake provider for engine tests. |
| `internal/render/completion.go` | `Popup` type + `DrawCompletion`. |
| `internal/editor/editor.go` | Wiring: `docVersion`, engine lifecycle, edit→`Update`, bridge goroutine, popup state, key interception, accept-insert. |
| `internal/editor/complete.go` | Editor helpers: word/prefix extraction, trigger gate, accept logic (kept out of `editor.go` for focus). |

---

### Task 1: LSP protocol types and message framing

**Files:**
- Create: `internal/lsp/protocol.go`
- Test: `internal/lsp/protocol_test.go`

**Interfaces:**
- Produces: `Position{Line,Character int}`; `CompletionItem{Label,Detail,InsertText string; Kind int; TextEdit *TextEdit}`; `writeMessage(w io.Writer, v any) error`; `readMessage(r *bufio.Reader) ([]byte, error)`.

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/lsp/ -run TestWriteReadMessageRoundTrip -v`
Expected: FAIL — `undefined: writeMessage` / `readMessage`.

- [ ] **Step 3: Write minimal implementation**

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/lsp/ -run TestWriteReadMessageRoundTrip -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/lsp/protocol.go internal/lsp/protocol_test.go
git commit -m "feat(lsp): JSON-RPC message framing and protocol types"
```

---

### Task 2: LSP client — start, reader loop, request/response, initialize

**Files:**
- Create: `internal/lsp/client.go`
- Modify: `internal/lsp/protocol.go` (add envelope types)
- Test: `internal/lsp/client_test.go`

**Interfaces:**
- Consumes: `writeMessage`, `readMessage`, `Position`, `CompletionItem` (Task 1).
- Produces:
  - `func Start(cmd string, args []string) (*Client, error)`
  - `func newClientPipe(in io.WriteCloser, out io.Reader) *Client` (test seam; wraps a running reader loop)
  - `func (c *Client) Initialize(rootURI string) (positionEncoding string, err error)`
  - `func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error)`
  - `func (c *Client) notify(method string, params any) error`

- [ ] **Step 1: Write the failing test**

```go
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
	cin, sin := io.Pipe()  // client writes -> server reads (sin)
	sout, cout := io.Pipe() // server writes (cout) -> client reads (sout)
	c := newClientPipe(writeCloser{cin}, sout)

	go func() {
		r := bufio.NewReader(sin)
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/lsp/ -run TestInitializeHandshake -v`
Expected: FAIL — `undefined: newClientPipe`.

- [ ] **Step 3: Add envelope types to `protocol.go`**

```go
// Append to internal/lsp/protocol.go

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
```

- [ ] **Step 4: Write `client.go`**

```go
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// Client is a JSON-RPC 2.0 client speaking LSP to a subprocess over stdio.
type Client struct {
	in  io.WriteCloser
	out *bufio.Reader

	wmu sync.Mutex // serializes writes

	mu      sync.Mutex // guards nextID and pending
	nextID  int
	pending map[int]chan rpcResponse

	cmd *exec.Cmd
}

// Start launches cmd and begins reading its stdout. It does not initialize.
func Start(cmd string, args []string) (*Client, error) {
	c := exec.Command(cmd, args...)
	stdin, err := c.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := c.Start(); err != nil {
		return nil, err
	}
	cl := newClientPipe(stdin, stdout)
	cl.cmd = c
	return cl, nil
}

// newClientPipe wraps an already-connected transport and starts the reader.
func newClientPipe(in io.WriteCloser, out io.Reader) *Client {
	c := &Client{in: in, out: bufio.NewReader(out), pending: map[int]chan rpcResponse{}}
	go c.readLoop()
	return c
}

func (c *Client) readLoop() {
	for {
		body, err := readMessage(c.out)
		if err != nil {
			c.failPending(err)
			return
		}
		var m serverMessage
		if json.Unmarshal(body, &m) != nil {
			continue
		}
		switch {
		case m.ID != nil && m.Method == "":
			// Response to our request.
			c.deliver(*m.ID, rpcResponse{ID: *m.ID, Result: m.Result, Error: m.Error})
		case m.ID != nil && m.Method != "":
			// Server-initiated request: reply null so gopls does not block.
			_ = c.reply(*m.ID)
		default:
			// Notification (diagnostics, logs): ignored in MVP.
		}
	}
}

func (c *Client) deliver(id int, resp rpcResponse) {
	c.mu.Lock()
	ch := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ch != nil {
		ch <- resp
	}
}

func (c *Client) failPending(err error) {
	c.mu.Lock()
	for id, ch := range c.pending {
		ch <- rpcResponse{ID: id, Error: &rpcError{Code: -1, Message: err.Error()}}
		delete(c.pending, id)
	}
	c.mu.Unlock()
}

func (c *Client) reply(id int) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return writeMessage(c.in, map[string]any{"jsonrpc": "2.0", "id": id, "result": nil})
}

// call sends a request and waits for its response or ctx cancellation.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan rpcResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	c.wmu.Lock()
	err := writeMessage(c.in, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	c.wmu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// notify sends a notification (no response expected).
func (c *Client) notify(method string, params any) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return writeMessage(c.in, map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// Initialize performs the initialize/initialized handshake and returns the
// server's chosen position encoding ("utf-16" if unspecified).
func (c *Client) Initialize(rootURI string) (string, error) {
	params := map[string]any{
		"processId": nil,
		"rootUri":   rootURI,
		"capabilities": map[string]any{
			"general":      map[string]any{"positionEncodings": []string{"utf-8", "utf-16"}},
			"textDocument": map[string]any{"completion": map[string]any{}},
		},
	}
	raw, err := c.call(context.Background(), "initialize", params)
	if err != nil {
		return "", err
	}
	var res struct {
		Capabilities struct {
			PositionEncoding string `json:"positionEncoding"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", err
	}
	enc := res.Capabilities.PositionEncoding
	if enc == "" {
		enc = "utf-16"
	}
	if err := c.notify("initialized", map[string]any{}); err != nil {
		return "", err
	}
	return enc, nil
}

var _ = fmt.Sprintf
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/lsp/ -run TestInitializeHandshake -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/lsp/client.go internal/lsp/protocol.go internal/lsp/client_test.go
git commit -m "feat(lsp): subprocess client with initialize handshake"
```

---

### Task 3: LSP client — document sync, completion, shutdown

**Files:**
- Modify: `internal/lsp/client.go`
- Test: `internal/lsp/client_test.go` (add case)

**Interfaces:**
- Produces:
  - `func (c *Client) DidOpen(uri, languageID string, version int, text string) error`
  - `func (c *Client) DidChange(uri string, version int, text string) error`
  - `func (c *Client) DidClose(uri string) error`
  - `func (c *Client) Completion(ctx context.Context, uri string, pos Position) ([]CompletionItem, error)`
  - `func (c *Client) Shutdown() error`

- [ ] **Step 1: Write the failing test**

```go
func TestCompletionDecodesList(t *testing.T) {
	cin, sin := io.Pipe()
	sout, cout := io.Pipe()
	c := newClientPipe(writeCloser{cin}, sout)

	go func() {
		r := bufio.NewReader(sin)
		body, _ := readMessage(r)
		var req struct {
			ID int `json:"id"`
		}
		_ = json.Unmarshal(body, &req)
		resp := map[string]any{
			"jsonrpc": "2.0", "id": req.ID,
			"result": map[string]any{
				"isIncomplete": false,
				"items": []map[string]any{
					{"label": "Println", "insertText": "Println", "kind": 3},
				},
			},
		}
		_ = writeMessage(writeCloser{cout}, resp)
	}()

	items, err := c.Completion(context.Background(), "file:///x.go", Position{Line: 0, Character: 0})
	if err != nil {
		t.Fatalf("Completion: %v", err)
	}
	if len(items) != 1 || items[0].Label != "Println" {
		t.Fatalf("items = %+v, want one Println", items)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/lsp/ -run TestCompletionDecodesList -v`
Expected: FAIL — `c.Completion undefined`.

- [ ] **Step 3: Implement the methods**

```go
// Append to internal/lsp/client.go

// DidOpen notifies the server a document was opened.
func (c *Client) DidOpen(uri, languageID string, version int, text string) error {
	return c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri": uri, "languageId": languageID, "version": version, "text": text,
		},
	})
}

// DidChange notifies the server of a full-text document replacement.
func (c *Client) DidChange(uri string, version int, text string) error {
	return c.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": version},
		"contentChanges": []map[string]any{{"text": text}},
	})
}

// DidClose notifies the server a document was closed.
func (c *Client) DidClose(uri string) error {
	return c.notify("textDocument/didClose", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
}

// Completion requests completions at pos. It accepts both a bare item array
// and a CompletionList result shape.
func (c *Client) Completion(ctx context.Context, uri string, pos Position) ([]CompletionItem, error) {
	raw, err := c.call(ctx, "textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	})
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	// Try CompletionList first, then a bare array.
	var list struct {
		Items []CompletionItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err == nil && list.Items != nil {
		return list.Items, nil
	}
	var arr []CompletionItem
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

// Shutdown asks the server to shut down and terminates the process.
func (c *Client) Shutdown() error {
	_, _ = c.call(context.Background(), "shutdown", nil)
	_ = c.notify("exit", nil)
	_ = c.in.Close()
	if c.cmd != nil {
		_ = c.cmd.Wait()
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/lsp/ -v`
Expected: PASS (all three lsp tests).

- [ ] **Step 5: Commit**

```bash
git add internal/lsp/client.go internal/lsp/client_test.go
git commit -m "feat(lsp): document sync, completion, and shutdown"
```

---

### Task 4: Completion public types and the fake provider

**Files:**
- Create: `internal/completion/completion.go`
- Create: `internal/completion/fake_test.go`
- Test: `internal/completion/completion_test.go`

**Interfaces:**
- Produces: `Item`, `Kind`, `Document`, `Position`, `Result`, `Provider`, `DocSink` (see code). Test helper `fakeProvider`.

- [ ] **Step 1: Write the failing test**

```go
package completion

import (
	"context"
	"testing"
)

func TestFakeProviderReturnsItems(t *testing.T) {
	f := &fakeProvider{items: []Item{{Label: "foo", Insert: "foo"}}}
	got, err := f.Complete(context.Background(), Document{}, Position{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(got) != 1 || got[0].Label != "foo" {
		t.Fatalf("got %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/completion/ -run TestFakeProviderReturnsItems -v`
Expected: FAIL — undefined types.

- [ ] **Step 3: Write `completion.go`**

```go
// Package completion is an extensible, UI-free autocompletion engine. It has no
// tcell, buffer, or render dependencies.
package completion

import "context"

// Kind categorizes a completion item (mirrors a subset of LSP CompletionItemKind).
type Kind int

const (
	KindText Kind = iota
	KindFunction
	KindVariable
	KindKeyword
	KindField
)

// Item is one completion candidate.
type Item struct {
	Label  string // shown in the popup
	Insert string // text inserted on accept
	Detail string // e.g. a type signature (optional)
	Kind   Kind
}

// Position is a zero-based line/character location in a Document.
type Position struct {
	Line, Character int
}

// Document is an immutable snapshot handed to a provider. It never aliases the
// editor's buffer.
type Document struct {
	URI     string
	LangID  string
	Text    string
	Version int
}

// Result is a completed request, tagged with the document version it ran
// against so the consumer can drop stale results.
type Result struct {
	Version int
	Items   []Item
	Err     error
}

// Provider is one source of completions.
type Provider interface {
	Complete(ctx context.Context, doc Document, pos Position) ([]Item, error)
	Close() error
}

// DocSink is an optional Provider capability for document lifecycle sync
// (LSP didOpen/didChange/didClose). Providers that do not need it omit it.
type DocSink interface {
	DidOpen(doc Document) error
	DidChange(doc Document) error
	DidClose(uri string) error
}
```

- [ ] **Step 4: Write `fake_test.go`**

```go
package completion

import (
	"context"
	"sync"
)

// fakeProvider is a controllable in-package Provider for engine tests.
type fakeProvider struct {
	mu       sync.Mutex
	items    []Item
	err      error
	calls    int
	block    chan struct{} // if non-nil, Complete blocks until closed/received
	lastCtx  context.Context
}

func (f *fakeProvider) Complete(ctx context.Context, _ Document, _ Position) ([]Item, error) {
	f.mu.Lock()
	f.calls++
	f.lastCtx = ctx
	block := f.block
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.items, f.err
}

func (f *fakeProvider) Close() error { return nil }

func (f *fakeProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/completion/ -run TestFakeProviderReturnsItems -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/completion/completion.go internal/completion/fake_test.go internal/completion/completion_test.go
git commit -m "feat(completion): public types and test fake provider"
```

---

### Task 5: Completion engine (actor: debounce, dispatch, cancel, results)

**Files:**
- Create: `internal/completion/registry.go` (Registry type only; LSP factory in Task 6)
- Create: `internal/completion/engine.go`
- Test: `internal/completion/engine_test.go`

**Interfaces:**
- Consumes: `Provider`, `DocSink`, `Document`, `Position`, `Result` (Task 4).
- Produces:
  - `type Registry struct { Factory func(ext, root string) (Provider, error) }`
  - `func New(reg Registry, opts ...Option) *Engine`
  - `func WithDebounce(d time.Duration) Option`
  - `func (e *Engine) Open(doc Document)`
  - `func (e *Engine) Update(doc Document, pos Position)`
  - `func (e *Engine) Cancel()`
  - `func (e *Engine) CloseDoc(uri string)`
  - `func (e *Engine) Results() <-chan Result`
  - `func (e *Engine) Close() error`

- [ ] **Step 1: Write the failing tests**

```go
package completion

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func regFor(p Provider) Registry {
	return Registry{Factory: func(ext, root string) (Provider, error) {
		if ext == ".go" {
			return p, nil
		}
		return nil, nil
	}}
}

func TestEngineDebounceCollapsesBurst(t *testing.T) {
	f := &fakeProvider{items: []Item{{Label: "x"}}}
	e := New(regFor(f), WithDebounce(40*time.Millisecond))
	defer e.Close()

	doc := Document{URI: "file:///a.go", Version: 1}
	for i := 0; i < 5; i++ {
		e.Update(doc, Position{})
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case <-e.Results():
	case <-time.After(time.Second):
		t.Fatal("no result")
	}
	if got := f.callCount(); got != 1 {
		t.Fatalf("Complete called %d times, want 1", got)
	}
}

func TestEngineNewUpdateCancelsInflight(t *testing.T) {
	block := make(chan struct{})
	f := &fakeProvider{block: block}
	e := New(regFor(f), WithDebounce(5*time.Millisecond))
	defer e.Close()

	doc := Document{URI: "file:///a.go", Version: 1}
	e.Update(doc, Position{})
	// Wait until the provider is inside Complete.
	deadline := time.After(time.Second)
	for f.callCount() == 0 {
		select {
		case <-deadline:
			t.Fatal("first Complete never started")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	firstCtx := f.lastCtx
	e.Update(Document{URI: "file:///a.go", Version: 2}, Position{}) // supersede
	select {
	case <-firstCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("first request context was not cancelled")
	}
	close(block)
}

func TestEngineUnsupportedExtensionYieldsNothing(t *testing.T) {
	f := &fakeProvider{items: []Item{{Label: "x"}}}
	e := New(regFor(f), WithDebounce(10*time.Millisecond))
	defer e.Close()

	e.Update(Document{URI: "file:///a.txt", Version: 1}, Position{})
	select {
	case r := <-e.Results():
		t.Fatalf("unexpected result %+v", r)
	case <-time.After(80 * time.Millisecond):
	}
	_ = filepath.Ext // used indirectly by the engine
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/completion/ -run TestEngine -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write `registry.go`**

```go
package completion

// Registry resolves a completion Provider for a file extension and root dir.
// Factory returns (nil, nil) when the extension is unsupported.
type Registry struct {
	Factory func(ext, root string) (Provider, error)
}
```

- [ ] **Step 4: Write `engine.go`**

```go
package completion

import (
	"context"
	"path/filepath"
	"time"
)

const defaultDebounce = 150 * time.Millisecond

// Option configures an Engine.
type Option func(*Engine)

// WithDebounce sets the idle delay before a completion request is dispatched.
func WithDebounce(d time.Duration) Option { return func(e *Engine) { e.debounce = d } }

type cmdKind int

const (
	cmdUpdate cmdKind = iota
	cmdCancel
	cmdOpen
	cmdCloseDoc
	cmdClose
)

type command struct {
	kind cmdKind
	doc  Document
	pos  Position
	uri  string
	done chan struct{}
}

// Engine debounces edits and dispatches completion requests to a provider
// selected by file extension. All state lives on one owner goroutine.
type Engine struct {
	reg      Registry
	debounce time.Duration
	cmds     chan command
	results  chan Result
}

// New builds and starts an Engine.
func New(reg Registry, opts ...Option) *Engine {
	e := &Engine{
		reg:      reg,
		debounce: defaultDebounce,
		cmds:     make(chan command),
		results:  make(chan Result, 1),
	}
	for _, o := range opts {
		o(e)
	}
	go e.loop()
	return e
}

// Results delivers completed requests. Each carries the document version it ran
// against; consumers drop results whose version is stale.
func (e *Engine) Results() <-chan Result { return e.results }

// Open registers a document (LSP didOpen for providers that implement DocSink).
func (e *Engine) Open(doc Document) { e.cmds <- command{kind: cmdOpen, doc: doc} }

// Update records an edit; the engine debounces then requests completions.
func (e *Engine) Update(doc Document, pos Position) {
	e.cmds <- command{kind: cmdUpdate, doc: doc, pos: pos}
}

// Cancel abandons any pending request without issuing a new one.
func (e *Engine) Cancel() { e.cmds <- command{kind: cmdCancel} }

// CloseDoc registers that a document is gone (LSP didClose).
func (e *Engine) CloseDoc(uri string) { e.cmds <- command{kind: cmdCloseDoc, uri: uri} }

// Close stops the engine, cancels in-flight work, and closes all providers.
func (e *Engine) Close() error {
	done := make(chan struct{})
	e.cmds <- command{kind: cmdClose, done: done}
	<-done
	return nil
}

func (e *Engine) loop() {
	providers := map[string]Provider{} // by extension
	var (
		timer   *time.Timer
		fire    = make(chan struct{}, 1)
		curDoc  Document
		curPos  Position
		cancel  context.CancelFunc
	)
	arm := func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(e.debounce, func() {
			select {
			case fire <- struct{}{}:
			default:
			}
		})
	}
	provFor := func(uri string) Provider {
		ext := filepath.Ext(uri)
		if p, ok := providers[ext]; ok {
			return p
		}
		if e.reg.Factory == nil {
			providers[ext] = nil
			return nil
		}
		root := filepath.Dir(uriToPath(uri))
		p, err := e.reg.Factory(ext, root)
		if err != nil {
			p = nil
		}
		providers[ext] = p
		return p
	}

	for {
		select {
		case c := <-e.cmds:
			switch c.kind {
			case cmdOpen:
				if p := provFor(c.doc.URI); p != nil {
					if s, ok := p.(DocSink); ok {
						_ = s.DidOpen(c.doc)
					}
				}
			case cmdUpdate:
				curDoc, curPos = c.doc, c.pos
				if cancel != nil {
					cancel()
					cancel = nil
				}
				arm()
			case cmdCancel:
				if timer != nil {
					timer.Stop()
				}
				if cancel != nil {
					cancel()
					cancel = nil
				}
			case cmdCloseDoc:
				if p := providers[filepath.Ext(c.uri)]; p != nil {
					if s, ok := p.(DocSink); ok {
						_ = s.DidClose(c.uri)
					}
				}
			case cmdClose:
				if timer != nil {
					timer.Stop()
				}
				if cancel != nil {
					cancel()
				}
				for _, p := range providers {
					if p != nil {
						_ = p.Close()
					}
				}
				close(e.results)
				close(c.done)
				return
			}
		case <-fire:
			p := provFor(curDoc.URI)
			if p == nil {
				continue
			}
			ctx, cf := context.WithCancel(context.Background())
			cancel = cf
			doc, pos := curDoc, curPos
			go func() {
				if s, ok := p.(DocSink); ok {
					_ = s.DidChange(doc) // sync before requesting
				}
				items, err := p.Complete(ctx, doc, pos)
				if ctx.Err() != nil {
					return // superseded; drop
				}
				select {
				case e.results <- Result{Version: doc.Version, Items: items, Err: err}:
				case <-ctx.Done():
				}
			}()
		}
	}
}

// uriToPath converts a file:// URI to a filesystem path (best-effort).
func uriToPath(uri string) string {
	if p, ok := stripFileScheme(uri); ok {
		return p
	}
	return uri
}
```

- [ ] **Step 5: Add `stripFileScheme` to `registry.go`**

```go
// Append to internal/completion/registry.go

import "strings"

// stripFileScheme returns the path for a file:// URI. On Windows a leading
// "file:///C:/x" becomes "C:/x".
func stripFileScheme(uri string) (string, bool) {
	rest, ok := strings.CutPrefix(uri, "file://")
	if !ok {
		return "", false
	}
	rest = strings.TrimPrefix(rest, "/")
	if len(rest) >= 2 && rest[1] == ':' { // Windows drive
		return rest, true
	}
	return "/" + rest, true
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/completion/ -run TestEngine -v`
Expected: PASS (all three engine tests).

- [ ] **Step 7: Commit**

```bash
git add internal/completion/engine.go internal/completion/registry.go internal/completion/engine_test.go
git commit -m "feat(completion): actor engine with debounce, cancel, and results"
```

---

### Task 6: LSP-backed provider and the gopls registry

**Files:**
- Create: `internal/completion/lspprovider.go`
- Test: `internal/completion/lspprovider_test.go`

**Interfaces:**
- Consumes: `lsp.Position`, `lsp.CompletionItem`, `Document`, `Item`, `Provider`, `DocSink`, `Registry`.
- Produces:
  - `type lspConn interface { ... }` (DIP seam over `*lsp.Client`)
  - `func newLSPProvider(conn lspConn, langID, encoding string) *lspProvider`
  - `func LSPRegistry(specs map[string]ServerSpec) Registry`
  - `type ServerSpec struct { Cmd string; Args []string; LangID string }`

- [ ] **Step 1: Write the failing test**

```go
package completion

import (
	"context"
	"testing"

	"github.com/bftelman/slopcode/internal/lsp"
)

type fakeConn struct {
	opened, changed int
	items           []lsp.CompletionItem
}

func (c *fakeConn) DidOpen(uri, lang string, ver int, text string) error { c.opened++; return nil }
func (c *fakeConn) DidChange(uri string, ver int, text string) error     { c.changed++; return nil }
func (c *fakeConn) DidClose(uri string) error                            { return nil }
func (c *fakeConn) Completion(ctx context.Context, uri string, pos lsp.Position) ([]lsp.CompletionItem, error) {
	return c.items, nil
}
func (c *fakeConn) Shutdown() error { return nil }

func TestLSPProviderMapsItemsAndSyncsOnce(t *testing.T) {
	conn := &fakeConn{items: []lsp.CompletionItem{{Label: "Println", InsertText: "Println", Kind: 3}}}
	p := newLSPProvider(conn, "go", "utf-8")

	doc := Document{URI: "file:///x.go", Version: 1, Text: "package x"}
	if err := p.DidOpen(doc); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	got, err := p.Complete(context.Background(), doc, Position{Line: 0, Character: 3})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(got) != 1 || got[0].Label != "Println" || got[0].Insert != "Println" {
		t.Fatalf("mapped items = %+v", got)
	}
	if conn.opened != 1 {
		t.Fatalf("DidOpen sent %d times, want 1", conn.opened)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/completion/ -run TestLSPProvider -v`
Expected: FAIL — `undefined: newLSPProvider`.

- [ ] **Step 3: Write `lspprovider.go`**

```go
package completion

import (
	"context"

	"github.com/bftelman/slopcode/internal/lsp"
)

// ServerSpec describes how to launch a language server for an extension.
type ServerSpec struct {
	Cmd    string
	Args   []string
	LangID string
}

// lspConn is the subset of *lsp.Client the provider needs (DIP seam for tests).
type lspConn interface {
	DidOpen(uri, langID string, version int, text string) error
	DidChange(uri string, version int, text string) error
	DidClose(uri string) error
	Completion(ctx context.Context, uri string, pos lsp.Position) ([]lsp.CompletionItem, error)
	Shutdown() error
}

// lspProvider adapts an lspConn to the completion Provider + DocSink interfaces.
type lspProvider struct {
	conn   lspConn
	langID string
	enc    string
}

func newLSPProvider(conn lspConn, langID, encoding string) *lspProvider {
	return &lspProvider{conn: conn, langID: langID, enc: encoding}
}

func (p *lspProvider) DidOpen(doc Document) error {
	return p.conn.DidOpen(doc.URI, p.langID, doc.Version, doc.Text)
}

func (p *lspProvider) DidChange(doc Document) error {
	return p.conn.DidChange(doc.URI, doc.Version, doc.Text)
}

func (p *lspProvider) DidClose(uri string) error { return p.conn.DidClose(uri) }

func (p *lspProvider) Complete(ctx context.Context, doc Document, pos Position) ([]Item, error) {
	raw, err := p.conn.Completion(ctx, doc.URI, lsp.Position{Line: pos.Line, Character: pos.Character})
	if err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(raw))
	for _, ci := range raw {
		insert := ci.InsertText
		if insert == "" {
			insert = ci.Label
		}
		items = append(items, Item{
			Label:  ci.Label,
			Insert: insert,
			Detail: ci.Detail,
			Kind:   mapKind(ci.Kind),
		})
	}
	return items, nil
}

func (p *lspProvider) Close() error { return p.conn.Shutdown() }

// mapKind maps a subset of LSP CompletionItemKind numbers to our Kind.
func mapKind(k int) Kind {
	switch k {
	case 3: // Function
		return KindFunction
	case 6: // Variable
		return KindVariable
	case 5: // Field
		return KindField
	case 14: // Keyword
		return KindKeyword
	default:
		return KindText
	}
}

// LSPRegistry builds a Registry that starts one gopls-style server per
// extension. A start or initialize failure yields (nil, nil): completion is
// disabled for that extension, never fatal.
func LSPRegistry(specs map[string]ServerSpec) Registry {
	return Registry{Factory: func(ext, root string) (Provider, error) {
		spec, ok := specs[ext]
		if !ok {
			return nil, nil
		}
		client, err := lsp.Start(spec.Cmd, spec.Args)
		if err != nil {
			return nil, nil
		}
		rootURI := "file:///" + root
		enc, err := client.Initialize(rootURI)
		if err != nil {
			_ = client.Shutdown()
			return nil, nil
		}
		return newLSPProvider(client, spec.LangID, enc), nil
	}}
}

// GoplsSpecs is the MVP registry: Go via gopls.
func GoplsSpecs() map[string]ServerSpec {
	return map[string]ServerSpec{".go": {Cmd: "gopls", LangID: "go"}}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/completion/ -v`
Expected: PASS (all completion tests).

- [ ] **Step 5: Commit**

```bash
git add internal/completion/lspprovider.go internal/completion/lspprovider_test.go
git commit -m "feat(completion): LSP-backed provider and gopls registry"
```

---

### Task 7: Completion popup renderer

**Files:**
- Create: `internal/render/completion.go`
- Test: `internal/render/completion_test.go`

**Interfaces:**
- Consumes: `completion.Item`.
- Produces:
  - `type Popup struct { Items []completion.Item; Sel int; Anchor struct{ X, Y int } }`
  - `func DrawCompletion(s tcell.Screen, p Popup)`

- [ ] **Step 1: Write the failing test**

```go
package render

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/completion"
)

func cellText(s tcell.SimulationScreen, x, y, n int) string {
	cells, _, _ := s.GetContents()
	w, _ := s.Size()
	out := make([]rune, 0, n)
	for i := 0; i < n; i++ {
		c := cells[y*w+x+i]
		if len(c.Runes) > 0 {
			out = append(out, c.Runes[0])
		}
	}
	return string(out)
}

func TestDrawCompletionListsItemsBelowAnchor(t *testing.T) {
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.SetSize(40, 10)

	p := Popup{Items: []completion.Item{{Label: "alpha"}, {Label: "beta"}}, Sel: 1}
	p.Anchor.X, p.Anchor.Y = 2, 3
	DrawCompletion(s, p)

	// Row below the anchor holds the first item.
	if got := cellText(s, 2, 4, 5); got != "alpha" {
		t.Fatalf("row 4 = %q, want alpha", got)
	}
	if got := cellText(s, 2, 5, 4); got != "beta" {
		t.Fatalf("row 5 = %q, want beta", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/render/ -run TestDrawCompletion -v`
Expected: FAIL — `undefined: Popup`.

- [ ] **Step 3: Write `completion.go`**

```go
package render

import (
	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/completion"
)

// completionMaxRows caps the visible popup height.
const completionMaxRows = 8

// Popup is the completion list to draw, anchored at a screen cell.
type Popup struct {
	Items  []completion.Item
	Sel    int
	Anchor struct{ X, Y int }
}

// DrawCompletion renders p as a list below its anchor, flipping above when it
// would clip the bottom. The selected row is highlighted. A nil/empty popup
// draws nothing.
func DrawCompletion(s tcell.Screen, p Popup) {
	if len(p.Items) == 0 {
		return
	}
	width, height := s.Size()

	rows := len(p.Items)
	if rows > completionMaxRows {
		rows = completionMaxRows
	}
	// Scroll so the selection is visible.
	start := 0
	if p.Sel >= rows {
		start = p.Sel - rows + 1
	}

	boxW := 0
	for _, it := range p.Items {
		if l := len(it.Label); l > boxW {
			boxW = l
		}
	}
	boxW += 2 // padding
	if boxW > width {
		boxW = width
	}

	top := p.Anchor.Y + 1
	if top+rows > height { // flip above
		top = p.Anchor.Y - rows
	}
	if top < 0 {
		top = 0
	}
	x := p.Anchor.X
	if x+boxW > width {
		x = width - boxW
	}
	if x < 0 {
		x = 0
	}

	normal := tcell.StyleDefault.Reverse(true)
	sel := tcell.StyleDefault.Background(active.accent).Foreground(tcell.ColorBlack)
	for i := 0; i < rows; i++ {
		idx := start + i
		st := normal
		if idx == p.Sel {
			st = sel
		}
		label := clipPad(" "+p.Items[idx].Label, boxW)
		drawText(s, x, top+i, label, st)
	}
	s.Show()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/render/ -run TestDrawCompletion -v`
Expected: PASS.

- [ ] **Step 5: Run the full render suite to confirm no regression**

Run: `go test ./internal/render/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/render/completion.go internal/render/completion_test.go
git commit -m "feat(render): completion popup drawer"
```

---

### Task 8: Editor helpers — versioning, word/prefix, trigger gate

**Files:**
- Create: `internal/editor/complete.go`
- Test: `internal/editor/complete_test.go`

**Interfaces:**
- Consumes: `completion.Position`.
- Produces (all on `*Editor`, added in Task 9's struct changes — this task defines the free functions they use):
  - `func identChar(r rune) bool`
  - `func wordStart(line string, col int) int` — byte index of the current word's start
  - `func shouldTrigger(prevRune rune) bool`

- [ ] **Step 1: Write the failing tests**

```go
package editor

import "testing"

func TestWordStart(t *testing.T) {
	cases := []struct {
		line string
		col  int
		want int
	}{
		{"fmt.Pri", 7, 4}, // after '.', word is "Pri"
		{"foo", 3, 0},
		{"  bar", 5, 2},
		{"", 0, 0},
		{"a.b.c", 5, 4},
	}
	for _, c := range cases {
		if got := wordStart(c.line, c.col); got != c.want {
			t.Errorf("wordStart(%q,%d) = %d, want %d", c.line, c.col, got, c.want)
		}
	}
}

func TestShouldTrigger(t *testing.T) {
	if !shouldTrigger('a') || !shouldTrigger('.') || !shouldTrigger('_') {
		t.Fatal("ident/trigger chars must trigger")
	}
	if shouldTrigger(' ') || shouldTrigger('\t') {
		t.Fatal("whitespace must not trigger")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/editor/ -run 'TestWordStart|TestShouldTrigger' -v`
Expected: FAIL — undefined functions.

- [ ] **Step 3: Write `complete.go`**

```go
package editor

// identChar reports whether r is part of an identifier ([A-Za-z0-9_]).
func identChar(r rune) bool {
	return r == '_' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}

// wordStart returns the byte index where the identifier ending at col begins.
// col is a byte offset into line. The scan stops at the first non-identifier
// byte (ASCII-correct; see the R4 limitation).
func wordStart(line string, col int) int {
	i := col
	for i > 0 && identChar(rune(line[i-1])) {
		i--
	}
	return i
}

// shouldTrigger reports whether typing r should request completions.
func shouldTrigger(r rune) bool {
	return identChar(r) || r == '.'
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/editor/ -run 'TestWordStart|TestShouldTrigger' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/editor/complete.go internal/editor/complete_test.go
git commit -m "feat(editor): completion word and trigger helpers"
```

---

### Task 9: Editor wiring — engine lifecycle, edit→Update, bridge, popup keys, accept

**Files:**
- Modify: `internal/editor/editor.go`
- Modify: `internal/editor/complete.go` (add methods on `*Editor`)
- Test: `internal/editor/editor_test.go` (add cases)

**Interfaces:**
- Consumes: `completion.Engine`, `completion.New`, `completion.LSPRegistry`, `completion.GoplsSpecs`, `completion.Document`, `completion.Position`, `completion.Result`, `render.Popup`, `render.DrawCompletion`, and Task 8 helpers.
- Produces: `completionEvent` (tcell custom event); popup state on `*Editor`; behavior described below.

Design notes for the implementer:

- The `Editor` gains fields: `eng *completion.Engine`, `docVersion int`, `popup *render.Popup` (nil when closed), and `encoding` is handled inside the engine, not here.
- `New` accepts the engine so tests can inject one built from a fake registry:
  add `func NewWithEngine(s tcell.Screen, b *buffer.Buffer, path string, eng *completion.Engine) *Editor` and keep `New` as a wrapper that builds the gopls engine.
- The bridge goroutine turns `Result`s into `completionEvent`s posted with `s.PostEvent`. The `Run` loop handles `*completionEvent` by applying the result (dropping stale versions) and redrawing.
- `docVersion` increments in the mutating branches of `handleKey` (rune, tab, newline, backspace, **undo, redo**).
- After a mutating key, call `e.requestCompletion(ev.Rune())` which gates on `shouldTrigger` and calls `e.eng.Update(snapshot, pos)`.
- When `popup != nil`, `handleKey` routes Up/Down/Enter/Tab/Esc to popup control before normal handling.

- [ ] **Step 1: Write the failing test (popup opens on results, accept inserts)**

```go
// Add to internal/editor/editor_test.go

func TestCompletionPopupOpensAndAccepts(t *testing.T) {
	items := []completion.Item{{Label: "Println", Insert: "Println"}}
	reg := completion.Registry{Factory: func(ext, root string) (completion.Provider, error) {
		return &stubProvider{items: items}, nil
	}}
	eng := completion.New(reg, completion.WithDebounce(5*time.Millisecond))

	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.SetSize(80, 24)
	b := buffer.New([]string{"Pri"})
	b.MoveEnd()
	e := NewWithEngine(s, b, "main.go", eng)

	// Simulate typing a trigger and receiving a result via the event loop.
	go e.Run()
	s.InjectKey(tcell.KeyRune, 'n', tcell.ModNone) // now "Prin"
	// Allow debounce + bridge to deliver and the loop to render.
	waitFor(t, func() bool { return e.popupOpenForTest() })

	// Accept with Enter.
	s.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, func() bool { return !e.popupOpenForTest() })

	if got := b.Lines()[0]; got != "Println" {
		t.Fatalf("line = %q, want Println", got)
	}
	s.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone)
}
```

Add these test helpers to `editor_test.go`:

```go
// stubProvider returns canned items for editor tests.
type stubProvider struct{ items []completion.Item }

func (p *stubProvider) Complete(ctx context.Context, _ completion.Document, _ completion.Position) ([]completion.Item, error) {
	return p.items, nil
}
func (p *stubProvider) Close() error { return nil }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for !cond() {
		select {
		case <-deadline:
			t.Fatal("condition not met in time")
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
}
```

And a test-only accessor in `complete.go`:

```go
// popupOpenForTest reports whether the completion popup is visible.
func (e *Editor) popupOpenForTest() bool { return e.popup != nil }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/editor/ -run TestCompletionPopupOpensAndAccepts -v`
Expected: FAIL — `undefined: NewWithEngine` / `completionEvent`.

- [ ] **Step 3: Add engine fields and constructor to `editor.go`**

```go
// Add imports: "github.com/bftelman/slopcode/internal/completion"
//               "github.com/bftelman/slopcode/internal/render" (already present)

// Add to the Editor struct:
//     eng        *completion.Engine
//     docVersion int
//     popup      *render.Popup

// New builds an Editor with the default gopls-backed completion engine.
func New(s tcell.Screen, b *buffer.Buffer, path string) *Editor {
	eng := completion.New(completion.LSPRegistry(completion.GoplsSpecs()))
	return NewWithEngine(s, b, path, eng)
}

// NewWithEngine builds an Editor with an injected completion engine (tests).
func NewWithEngine(s tcell.Screen, b *buffer.Buffer, path string, eng *completion.Engine) *Editor {
	e := &Editor{s: s, b: b, path: path, baseline: cloneLines(b.Lines()), eng: eng}
	if path != "" {
		e.eng.Open(e.document())
	}
	go e.bridge()
	return e
}
```

- [ ] **Step 4: Add the event type, bridge, document snapshot, and request logic to `complete.go`**

```go
// Add to internal/editor/complete.go

import (
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/buffer"
	"github.com/bftelman/slopcode/internal/completion"
	"github.com/bftelman/slopcode/internal/fileio"
	"github.com/bftelman/slopcode/internal/render"
)

// completionEvent carries an engine Result into the tcell event loop.
type completionEvent struct {
	when time.Time
	res  completion.Result
}

func (c *completionEvent) When() time.Time { return c.when }

// bridge forwards engine results into the event loop as tcell events.
func (e *Editor) bridge() {
	for r := range e.eng.Results() {
		_ = e.s.PostEvent(&completionEvent{res: r})
	}
}

// document snapshots the buffer as an immutable completion.Document.
func (e *Editor) document() completion.Document {
	return completion.Document{
		URI:     e.uri(),
		LangID:  "go",
		Text:    strings.Join(e.b.Lines(), "\n"),
		Version: e.docVersion,
	}
}

// uri returns a file:// URI for the current path (empty path -> empty URI).
func (e *Editor) uri() string {
	if e.path == "" {
		return ""
	}
	abs, err := filepath.Abs(e.path)
	if err != nil {
		abs = e.path
	}
	return "file:///" + filepath.ToSlash(abs)
}

// cursorPos returns the cursor as a completion.Position (byte column).
func (e *Editor) cursorPos() completion.Position {
	row, col := e.b.Cursor()
	return completion.Position{Line: row, Character: col}
}

// requestCompletion asks the engine for completions when r warrants it.
func (e *Editor) requestCompletion(r rune) {
	if e.path == "" || filepath.Ext(e.path) == "" {
		return // no route without an on-disk extension
	}
	if !shouldTrigger(r) {
		e.dismissPopup()
		return
	}
	e.eng.Update(e.document(), e.cursorPos())
}

// applyResult shows the popup for a fresh, non-empty result; drops stale ones.
func (e *Editor) applyResult(res completion.Result) {
	if res.Version != e.docVersion || res.Err != nil || len(res.Items) == 0 {
		e.dismissPopup()
		return
	}
	p := &render.Popup{Items: res.Items, Sel: 0}
	row, col := e.b.Cursor()
	gw := render.GutterWidth(e.b.LineCount())
	p.Anchor.X = gw + col
	p.Anchor.Y = row - e.scroll + 1
	e.popup = p
}

func (e *Editor) dismissPopup() {
	if e.popup != nil {
		e.popup = nil
		e.eng.Cancel()
	}
}

// handlePopupKey routes a key while the popup is open. Returns true if consumed.
func (e *Editor) handlePopupKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyUp:
		if e.popup.Sel > 0 {
			e.popup.Sel--
		}
		return true
	case tcell.KeyDown:
		if e.popup.Sel < len(e.popup.Items)-1 {
			e.popup.Sel++
		}
		return true
	case tcell.KeyEnter, tcell.KeyTab:
		e.acceptCompletion()
		return true
	case tcell.KeyEscape:
		e.dismissPopup()
		return true
	}
	return false // fall through to normal editing
}

// acceptCompletion replaces the current word with the selected item's insert
// text and closes the popup.
func (e *Editor) acceptCompletion() {
	item := e.popup.Items[e.popup.Sel]
	row, col := e.b.Cursor()
	line := e.b.Lines()[row]
	start := wordStart(line, col)
	e.b.Checkpoint()
	// Delete the existing prefix, then insert the full text.
	for i := 0; i < col-start; i++ {
		e.b.Backspace()
	}
	for _, r := range item.Insert {
		e.b.InsertRune(r)
	}
	e.docVersion++
	e.dismissPopup()
	e.eng.DidChangeNudge(e.document()) // keep server in sync after accept
}
```

Note: `DidChangeNudge` is a thin helper — add to the engine in Step 5.

- [ ] **Step 5: Add `DidChangeNudge` to the engine (`internal/completion/engine.go`)**

```go
// SyncOnly records a document change without requesting completions (e.g. after
// an accept), keeping the provider's view current.
func (e *Engine) SyncOnly(doc Document) { e.cmds <- command{kind: cmdSync, doc: doc} }
```

Add `cmdSync` to the `cmdKind` const block and handle it in `loop`:

```go
case cmdSync:
	if p := provFor(c.doc.URI); p != nil {
		if s, ok := p.(DocSink); ok {
			_ = s.DidChange(c.doc)
		}
	}
```

Then rename the call in Step 4 from `e.eng.DidChangeNudge(...)` to `e.eng.SyncOnly(...)`.

- [ ] **Step 6: Wire `handleKey`, `docVersion`, and `draw` in `editor.go`**

Modify `handleKey` so that, at the top (after the browser check), the popup gets first refusal:

```go
	if e.popup != nil {
		if e.handlePopupKey(ev) {
			return false
		}
	}
```

In the mutating cases, bump the version and request completion. For example the rune case becomes:

```go
	case tcell.KeyRune:
		e.b.Checkpoint()
		autopair.InsertRune(e.b, ev.Rune())
		e.docVersion++
		e.requestCompletion(ev.Rune())
```

Apply the same `e.docVersion++` to the Tab, Enter, Backspace, Undo, and Redo cases (Undo/Redo call `e.dismissPopup()` instead of `requestCompletion`). Add a `completionEvent` case to `Run`:

```go
	case *completionEvent:
		e.applyResult(ev.res)
```

And in `draw`, after the main `render.Draw(...)` call (non-splash path), draw the popup:

```go
	if e.popup != nil {
		render.DrawCompletion(e.s, *e.popup)
	}
```

- [ ] **Step 7: Run the new test to verify it passes**

Run: `go test ./internal/editor/ -run TestCompletionPopupOpensAndAccepts -v`
Expected: PASS.

- [ ] **Step 8: Add stale-drop and degradation tests**

```go
func TestCompletionDropsStaleVersion(t *testing.T) {
	eng := completion.New(completion.Registry{}, completion.WithDebounce(time.Millisecond))
	s := tcell.NewSimulationScreen("")
	_ = s.Init()
	s.SetSize(80, 24)
	e := NewWithEngine(s, buffer.New([]string{""}), "main.go", eng)
	e.docVersion = 5
	e.applyResult(completion.Result{Version: 2, Items: []completion.Item{{Label: "old"}}})
	if e.popupOpenForTest() {
		t.Fatal("stale result must not open the popup")
	}
}

func TestCompletionMissingServerIsNonFatal(t *testing.T) {
	// Registry that always fails to produce a provider.
	reg := completion.Registry{Factory: func(ext, root string) (completion.Provider, error) {
		return nil, nil
	}}
	eng := completion.New(reg, completion.WithDebounce(time.Millisecond))
	s := tcell.NewSimulationScreen("")
	_ = s.Init()
	s.SetSize(80, 24)
	b := buffer.New([]string{"a"})
	b.MoveEnd()
	e := NewWithEngine(s, b, "main.go", eng)
	go e.Run()
	s.InjectKey(tcell.KeyRune, 'b', tcell.ModNone)
	time.Sleep(50 * time.Millisecond)
	// Editor still responsive and no popup.
	if e.popupOpenForTest() {
		t.Fatal("no provider -> no popup")
	}
	s.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone)
}
```

- [ ] **Step 9: Run the full editor + repo suite**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Step 10: Commit**

```bash
git add internal/editor/editor.go internal/editor/complete.go internal/editor/editor_test.go internal/completion/engine.go
git commit -m "feat(editor): wire completion engine, popup, and key handling"
```

---

### Task 10: Real-gopls integration smoke test

**Files:**
- Create: `internal/completion/gopls_integration_test.go`

**Interfaces:**
- Consumes: `lsp.Start`, `LSPRegistry`, `GoplsSpecs`, `Engine`.

- [ ] **Step 1: Write the build-tagged test**

```go
//go:build lsp_integration

package completion

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestGoplsRealCompletion(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not on PATH")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	src := "package main\nimport \"fmt\"\nfunc main() {\n\tfmt.\n}\n"
	if err := os.WriteFile(file, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	eng := New(LSPRegistry(GoplsSpecs()), WithDebounce(10*time.Millisecond))
	defer eng.Close()

	uri := "file:///" + filepath.ToSlash(file)
	doc := Document{URI: uri, LangID: "go", Text: src, Version: 1}
	eng.Open(doc)
	// Cursor right after "fmt." on line index 3, character 5.
	eng.Update(doc, Position{Line: 3, Character: 5})

	select {
	case r := <-eng.Results():
		if r.Err != nil {
			t.Fatalf("result error: %v", r.Err)
		}
		found := false
		for _, it := range r.Items {
			if it.Label == "Println" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected Println among %d items", len(r.Items))
		}
	case <-time.After(30 * time.Second):
		t.Fatal("no completion result from gopls in time")
	}
}
```

- [ ] **Step 2: Verify it is skipped by default**

Run: `go test ./internal/completion/ -v`
Expected: the integration test does not appear (excluded by the build tag).

- [ ] **Step 3: Run it explicitly (requires gopls installed)**

Run: `go test -tags lsp_integration ./internal/completion/ -run TestGoplsRealCompletion -v`
Expected: PASS when gopls is installed; SKIP if not on PATH.

- [ ] **Step 4: Commit**

```bash
git add internal/completion/gopls_integration_test.go
git commit -m "test(completion): real-gopls integration smoke test"
```

---

### Task 11: Docs and cleanup

**Files:**
- Modify: `README.md` (Features + Keys)
- Modify: `AGENTS.md` (architecture table + a completion note)

- [ ] **Step 1: Update `README.md`**

Add to Features: "Automatic code completion via LSP (Go/gopls in this build); a popup appears as you type and Enter/Tab accepts, Esc dismisses." Add the popup keys (Up/Down/Enter/Tab/Esc) to the Keys section. Add `internal/lsp` and `internal/completion` rows to the project layout table.

- [ ] **Step 2: Update `AGENTS.md`**

Add `internal/lsp` and `internal/completion` to the package table, note the new dependency edges (`completion → lsp`, `render → completion` for `Item`/`Popup`), and add an invariant: "completion is best-effort; the buffer is never accessed off the UI thread — the engine gets immutable `Document` snapshots."

- [ ] **Step 3: Verify build and tests**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: no `gofmt` output, clean vet, all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document LSP autocomplete feature and packages"
```

---

## Self-review

**Spec coverage:**
- Extensible provider engine → Tasks 4, 5. LSP provider → Task 6. Registry/filetype → Task 6 (`LSPRegistry`, `GoplsSpecs`). Async goroutine+PostEvent → Task 9 (`bridge`, `completionEvent`). Automatic trigger/debounce/cancel/staleness → Tasks 5, 9. didChange-before-completion ordering → Task 5 (`fire` branch). Editor-owned versioning incl. undo/redo → Task 9 Step 6. Rendering popup + keys → Tasks 7, 9. Error handling non-fatal → Task 6 (nil provider) + Task 9 (`TestCompletionMissingServerIsNonFatal`). positionEncoding negotiation → Task 2 (`Initialize` reads back). Unsaved/[No Name] → Task 9 (`requestCompletion` extension guard). Testing (fake + smoke) → Tasks 4, 5, 6, 9, 10. All covered.
- LSP lifecycle `didClose` on file switch: the engine exposes `CloseDoc` (Task 5); wiring it into `browseEnter` is noted here as a **follow-up polish** (open the new file via `eng.Open`, close the old via `eng.CloseDoc`) — small and additive, folded into Task 9's file-switch path if the implementer reaches it, otherwise a P3 follow-up. Flagged so it is not silently dropped.

**Placeholder scan:** No TBD/TODO; every code step shows complete code. The only prose-only steps (Task 11) are doc edits with explicit content.

**Type consistency:** `completion.Document`, `Position`, `Item`, `Result`, `Provider`, `DocSink`, `Registry`, `Engine` methods (`Open`/`Update`/`Cancel`/`CloseDoc`/`SyncOnly`/`Results`/`Close`) are used consistently across Tasks 4–10. `lspConn` matches `*lsp.Client`'s method set from Tasks 2–3 (`DidOpen`/`DidChange`/`DidClose`/`Completion`/`Shutdown`). `render.Popup`/`DrawCompletion` consistent between Tasks 7 and 9. Renamed `DidChangeNudge`→`SyncOnly` fixed inline in Task 9 Step 5.

**Scope:** One coherent feature; 11 tasks each with an independently testable deliverable. Good for a single plan.
