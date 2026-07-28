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
	KindConstant
	KindType   // class/struct/interface
	KindModule // package/namespace
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
