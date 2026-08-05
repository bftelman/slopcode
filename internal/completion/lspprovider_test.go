package completion

import (
	"context"
	"runtime"
	"testing"

	"github.com/bftelman/namlet/internal/lsp"
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

func TestPathToFileURI(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{"unix absolute", "/mnt/second_drive/src/namlet/test_lsp.go", "file:///mnt/second_drive/src/namlet/test_lsp.go"},
		{"unix root", "/", "file:///"},
		// filepath.ToSlash only rewrites '\' on GOOS=windows (it's a no-op
		// elsewhere), so a drive path is given already slash-form here to
		// test PathToFileURI's own leading-slash logic independent of that
		// OS-dependent conversion; TestPathToFileURIWindowsBackslashes below
		// covers the real backslash-normalizing behavior on Windows itself.
		{"windows drive, slash-form", "C:/Users/x/test_lsp.go", "file:///C:/Users/x/test_lsp.go"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PathToFileURI(c.path); got != c.want {
				t.Errorf("PathToFileURI(%q) = %q, want %q", c.path, got, c.want)
			}
		})
	}
}

func TestPathToFileURIWindowsBackslashes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("filepath.ToSlash only rewrites '\\' on GOOS=windows")
	}
	got := PathToFileURI(`C:\Users\x\test_lsp.go`)
	want := "file:///C:/Users/x/test_lsp.go"
	if got != want {
		t.Errorf("PathToFileURI = %q, want %q", got, want)
	}
}

func TestStripFileSchemeHandlesMultipleLeadingSlashes(t *testing.T) {
	cases := []struct {
		name string
		uri  string
		want string
	}{
		{"three slashes (well-formed)", "file:///mnt/x/y.go", "/mnt/x/y.go"},
		{"four slashes (malformed, must still recover)", "file:////mnt/x/y.go", "/mnt/x/y.go"},
		{"windows drive, three slashes", "file:///C:/Users/x", "C:/Users/x"},
		{"windows drive, four slashes", "file:////C:/Users/x", "C:/Users/x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := stripFileScheme(c.uri)
			if !ok {
				t.Fatalf("stripFileScheme(%q): ok=false", c.uri)
			}
			if got != c.want {
				t.Errorf("stripFileScheme(%q) = %q, want %q", c.uri, got, c.want)
			}
		})
	}
}

func TestMapKind(t *testing.T) {
	cases := []struct {
		lspKind int
		want    Kind
	}{
		{2, KindFunction},  // Method
		{3, KindFunction},  // Function
		{4, KindFunction},  // Constructor
		{5, KindField},     // Field
		{6, KindVariable},  // Variable
		{7, KindType},      // Class
		{8, KindType},      // Interface
		{9, KindModule},    // Module
		{14, KindKeyword},  // Keyword
		{21, KindConstant}, // Constant
		{22, KindType},     // Struct
		{999, KindText},    // unmapped -> fallback
	}
	for _, c := range cases {
		if got := mapKind(c.lspKind); got != c.want {
			t.Errorf("mapKind(%d) = %v, want %v", c.lspKind, got, c.want)
		}
	}
}

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
