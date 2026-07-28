package completion

import (
	"context"
	"path/filepath"
	"sync"
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
	cmdSync
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

	ctx       context.Context
	cancelAll context.CancelFunc
	wg        sync.WaitGroup
}

// cmdBuffer sized so per-keystroke Update/Cancel sends (see below) rarely
// need their non-blocking fallback even while the loop is briefly busy
// (e.g. starting a provider).
const cmdBuffer = 8

// New builds and starts an Engine.
func New(reg Registry, opts ...Option) *Engine {
	e := &Engine{
		reg:      reg,
		debounce: defaultDebounce,
		cmds:     make(chan command, cmdBuffer),
		results:  make(chan Result, 1),
	}
	for _, o := range opts {
		o(e)
	}
	e.ctx, e.cancelAll = context.WithCancel(context.Background())
	go e.loop()
	return e
}

// Results delivers completed requests. Each carries the document version it ran
// against; consumers drop results whose version is stale.
func (e *Engine) Results() <-chan Result { return e.results }

// Open registers a document (LSP didOpen for providers that implement DocSink).
func (e *Engine) Open(doc Document) { e.cmds <- command{kind: cmdOpen, doc: doc} }

// Update records an edit; the engine debounces then requests completions.
// Non-blocking: if the loop is momentarily busy (e.g. starting a provider)
// and the command buffer is full, this drops the update rather than
// blocking the UI thread — safe, since a later keystroke's Update
// supersedes it anyway, and the loop is never busy for long (provider
// creation is itself bounded, e.g. LSPRegistry's initializeTimeout).
func (e *Engine) Update(doc Document, pos Position) {
	select {
	case e.cmds <- command{kind: cmdUpdate, doc: doc, pos: pos}:
	default:
	}
}

// Cancel abandons any pending request without issuing a new one.
// Non-blocking for the same reason as Update.
func (e *Engine) Cancel() {
	select {
	case e.cmds <- command{kind: cmdCancel}:
	default:
	}
}

// CloseDoc registers that a document is gone (LSP didClose).
func (e *Engine) CloseDoc(uri string) { e.cmds <- command{kind: cmdCloseDoc, uri: uri} }

// SyncOnly records a document change without requesting completions (e.g. after
// an accept), keeping the provider's view current.
func (e *Engine) SyncOnly(doc Document) { e.cmds <- command{kind: cmdSync, doc: doc} }

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
		timer  *time.Timer
		fire   = make(chan struct{}, 1)
		curDoc Document
		curPos Position
		cancel context.CancelFunc
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
			case cmdSync:
				if p := provFor(c.doc.URI); p != nil {
					if s, ok := p.(DocSink); ok {
						_ = s.DidChange(c.doc)
					}
				}
			case cmdClose:
				if timer != nil {
					timer.Stop()
				}
				e.cancelAll() // cancels every in-flight request's derived ctx
				e.wg.Wait()   // wait for all dispatch goroutines to finish
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
			ctx, cf := context.WithCancel(e.ctx)
			cancel = cf
			doc, pos := curDoc, curPos
			e.wg.Add(1)
			go func() {
				defer e.wg.Done()
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
