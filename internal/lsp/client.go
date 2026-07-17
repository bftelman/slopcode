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
