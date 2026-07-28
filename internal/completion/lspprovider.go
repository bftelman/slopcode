package completion

import (
	"context"
	"path/filepath"
	"strings"

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

// mapKind maps a subset of LSP CompletionItemKind numbers to our Kind. See
// https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#completionItemKind
// for the full numbering; unmapped kinds fall back to KindText.
func mapKind(k int) Kind {
	switch k {
	case 2, 3, 4: // Method, Function, Constructor
		return KindFunction
	case 6: // Variable
		return KindVariable
	case 5: // Field
		return KindField
	case 7, 8, 22: // Class, Interface, Struct
		return KindType
	case 9: // Module
		return KindModule
	case 14: // Keyword
		return KindKeyword
	case 21: // Constant
		return KindConstant
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
		client, err := lsp.Start(resolveCmd(spec.Cmd), spec.Args)
		if err != nil {
			return nil, nil
		}
		rootURI := PathToFileURI(root)
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

// PathToFileURI converts an absolute filesystem path to a file:// URI.
// Unix paths already start with '/', so filepath.ToSlash returns "/abs/path",
// and "file://" + "/abs/path" = "file:///abs/path" (correct, three slashes).
// Windows paths may be "C:\path" which becomes "C:/path" after filepath.ToSlash;
// to get "file:///C:/path" (correct), we add a leading "/" first.
func PathToFileURI(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}
