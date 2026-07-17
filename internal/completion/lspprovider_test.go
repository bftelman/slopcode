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
