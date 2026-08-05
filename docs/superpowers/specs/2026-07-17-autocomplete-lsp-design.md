# Spec: Extensible autocomplete engine with LSP

Date: 2026-07-17
Status: Design approved; iterating on integration detail (rev. 2).

## Goal

Add code autocompletion to namlet through an **extensible engine** that
accepts pluggable completion providers. The first-class provider connects to
**Language Server Protocol (LSP)** servers. The MVP targets Go via `gopls`;
completion server selection is driven by file type so more languages are added
as data, not code.

Completion is **best-effort and non-fatal**: if a server is missing or fails,
the editor keeps working unchanged.

## Decisions (locked during brainstorming)

| Question | Decision |
| --- | --- |
| Async integration | Background goroutine + `screen.PostEvent` into the loop |
| Engine shape | `Provider` interface; LSP is one provider, fake is another |
| Trigger | Automatic on typing (debounced, with trigger characters) |
| Testing | Fake provider (unit) + build-tagged real-`gopls` smoke test |

## Non-goals (MVP)

- No LSP features other than completion (no diagnostics, hover, go-to-def,
  formatting, rename).
- No incremental document sync (full-text sync only).
- No buffer-word or snippet provider yet (the fake provider proves the seam;
  these are documented future providers).
- No multi-server-per-file or completion-source merging/ranking across
  providers; one active provider per file type.
- No completion-item resolve, documentation popups, or signature help.

## Architecture

Three new packages; small additions to `render` and `editor`. All new logic is
UI-free except the editor wiring, honoring the dependency rule in `AGENTS.md`.

```
internal/lsp/          JSON-RPC 2.0 client over subprocess stdio + protocol types.
                       No UI, no completion deps.
internal/completion/   Provider interface, Item/Result/Document types, the Engine
                       (debounce/dispatch/cancel/staleness), the LSP-backed
                       provider, and the filetype->server registry. No UI.
internal/render/       + completion.go: DrawCompletion popup (also advances the
                       R2 render file-split from the conventions spec).
internal/editor/       Wiring: didOpen/didChange, PostEvent bridge, popup state,
                       key interception.
```

**Layering choice:** `completion` must not import tcell. The engine emits
results on a `<-chan Result`; the editor runs a small bridge goroutine that
converts each result into a `screen.PostEvent`. Async plumbing stays out of the
UI-free packages.

Dependency directions (must hold):

- `lsp` depends on nothing in this repo (stdlib + JSON only).
- `completion` depends on `lsp`; it does **not** import tcell, `buffer`, or
  `render`.
- `render` gains a popup drawer; it depends on `completion` only for the
  `Item`/popup value types it renders.
- `editor` wires everything: owns the buffer, the engine, and the bridge.

## The extensibility seam

```go
// Document is an immutable snapshot handed to a provider. No buffer aliasing.
type Document struct {
    URI     string   // file:// URI (or synthetic for [No Name])
    Text    string   // full text at Version
    Version int      // monotonic; matches the buffer version at snapshot time
}

// Position is a zero-based line/character location (see position-encoding note).
type Position struct{ Line, Character int }

// Item is one completion candidate.
type Item struct {
    Label  string // shown in the popup
    Insert string // text inserted on accept
    Detail string // e.g. type signature (optional)
    Kind   Kind   // function, variable, keyword, ... (optional)
}

// Result is a completed request, tagged for staleness checks.
type Result struct {
    Version int    // document version the request was issued against
    Items   []Item
    Err     error  // non-nil disables the popup for this request, never fatal
}

// Provider is one source of completions.
type Provider interface {
    Complete(ctx context.Context, doc Document, pos Position) ([]Item, error)
    Close() error
}
```

Two providers exist at MVP: the **LSP provider** (real) and the **fake
provider** (tests). This is the proof that the engine is not coupled to LSP.

### Engine API

The editor drives the engine through a small, non-blocking surface. Every call
returns immediately; work happens on the engine's owner goroutine.

```go
// New builds an Engine that selects a provider per file type from the registry.
func New(reg Registry) *Engine

// Open registers a document with the engine (and, for LSP, sends didOpen).
func (e *Engine) Open(doc Document)

// Update records an edit. The engine debounces, then syncs the document to the
// provider and requests completions for pos. Supersedes any pending request.
func (e *Engine) Update(doc Document, pos Position)

// Cancel abandons the pending request (e.g. popup dismissed) without a new one.
func (e *Engine) Cancel()

// CloseDoc registers that a document is gone (LSP didClose); used on file switch.
func (e *Engine) CloseDoc(uri string)

// Results delivers completed requests. The editor bridges these to PostEvent.
func (e *Engine) Results() <-chan Result

// Close shuts down the engine, cancels in-flight work, and stops all providers.
func (e *Engine) Close() error
```

`Registry` maps a file extension to a `ServerSpec`; the engine lazily starts and
reuses one provider per language.

## Concurrency model (actor, not mutexes)

The buffer is **UI-thread-only**. Nothing off the UI thread touches it.

1. On each edit the editor builds a `Document` snapshot (immutable copy of
   `b.Lines()` joined, plus version and cursor `Position`) and sends it to the
   engine. Pure value; no aliasing of buffer state.
2. The `Engine` runs **one owner goroutine** consuming commands (`OnEdit`,
   `Cancel`, `Shutdown`) from a channel. It dispatches provider calls with a
   `context.Context` it cancels when a newer edit arrives. Results go out on
   `Results() <-chan Result`. No shared mutable state, therefore no locks.
3. The `lsp.Client` runs its own reader goroutine that frames JSON-RPC messages
   and matches responses to requests by id.
4. The editor bridge goroutine:
   `for r := range engine.Results() { screen.PostEvent(completionEvent{r}) }`.

Shutdown: closing the engine cancels in-flight contexts, closes providers
(`gopls` `shutdown`/`exit`), and closes `Results()`, ending the bridge
goroutine cleanly.

## Trigger, debounce, staleness (automatic-on-typing)

- **Debounce**: wait ~150 ms after the last keystroke before dispatching.
- **Trigger gate**: dispatch when the char just typed is an *identifier char* or
  a trigger char (`.`). Suppress on whitespace, on backspace that empties the
  prefix, and while the file browser is open.
- **Definitions**: an *identifier char* is `[A-Za-z0-9_]`. The *current word*
  (the completion prefix, and the span replaced on accept) runs from the cursor
  back to the first preceding non-identifier char on the line. An empty prefix
  after a trigger char (e.g. just after `.`) is allowed and requests member
  completions.
- **Cancellation**: a new edit cancels the in-flight request's context.
- **Staleness**: each `Result` carries the document `Version`; the editor drops
  any result whose version does not equal the current buffer version, so the
  popup never renders against text the user has moved past.
- **Live filtering**: while typing within the same word, the popup re-filters
  against the current prefix and re-requests on the next debounce tick.

## Document versioning & sync ordering

The `buffer` stays pure (no version field — YAGNI). The **editor** owns a
monotonically increasing `docVersion`, incremented on every buffer-mutating
action — inserts, deletes, tab, newline, **and undo/redo** (cursor-only moves do
not bump it). That version flows into the `Document` snapshot, into LSP
`didChange`, and back on `Result` for the staleness check — one source of truth.

On each debounce tick the engine performs the provider interaction **in order**:

1. Sync the document to the provider first (LSP `didChange` with the snapshot
   text and version), so the server's view matches the text the request refers
   to.
2. Then issue `textDocument/completion` at `pos` for that same version.

Skipping step 1, or reordering it, would let `gopls` complete against stale
text. Cancelling a superseded request is done by **dropping its late response**
via the version check; the MVP does **not** send LSP `$/cancelRequest` (a
possible later optimization, noted so its absence is intentional, not an
oversight).

## Rendering & key handling

`render.DrawCompletion(s, popup)` draws a bordered list anchored at the cursor:
below it by default, flipping above when it would clip the bottom row; the
selected row is highlighted; the list scrolls when longer than a max height.
The popup is drawn after the text frame so it overlays.

Editor input gains a popup-open state. When the popup is open, keys are
intercepted before normal handling:

| Key | Popup open | Popup closed (today's behavior) |
| --- | --- | --- |
| Up / Down | Move selection | Move cursor |
| Enter | Accept selected item | Split line |
| Tab | Accept selected item | Insert spaces to tab stop |
| Esc | Dismiss popup | (no-op / browser close) |
| Any edit key | Update prefix, keep popup | Normal edit |

Accepting replaces the current word (from the word start to the cursor) with the
item's `Insert` text. Popup state lives in a `completion` field on `Editor`,
mirroring how `browser`/`pendingOpen` already gate input.

## LSP scope & lifecycle (MVP)

Protocol subset only:

- `initialize` / `initialized`. `rootUri` is the directory of the current file
  (or cwd), which puts `gopls` in single-file/dir mode. Advertise the
  completion capability and a preferred `positionEncoding` of `utf-8` via
  `general.positionEncodings`. **Read back** the server's chosen encoding from
  the `initialize` result's `capabilities.positionEncoding`; if absent, assume
  `utf-16` (the LSP default) — see the known-limitation note.
- `textDocument/didOpen` when a file is loaded/opened.
- `textDocument/didChange` with **full-text sync** (`TextDocumentSyncKind.Full`)
  on edits.
- `textDocument/completion` for requests.
- `textDocument/didClose` when switching away from a file (browser open of a new
  file sends `didClose` for the old, `didOpen` for the new).
- `shutdown` / `exit` on quit.

**Unsaved / `[No Name]` buffers**: completion requires an on-disk path with a
registered extension. An unsaved `[No Name]` buffer gets no completions until
saved (it has no URI and no extension to route on). This is consistent with the
non-goals and keeps the MVP from inventing temp-file plumbing.

**Filetype -> server registry**: a `map[ext]ServerSpec`, where `ServerSpec` is
`{Cmd string, Args []string, LanguageID string}`. MVP entry:
`.go -> {Cmd:"gopls", LanguageID:"go"}`. Files with no registered server get no
completions (engine yields empty, editor shows no popup). This is the
"detect LSP based on file type" requirement, staged as gopls-first now,
registry-driven next.

Lifecycle: launch `gopls` lazily on the first `.go` file opened; one server
instance per language; `shutdown` on editor quit.

## Error handling

Completion never crashes or blocks the editor.

- `gopls` not found / fails to start: status notice
  "gopls unavailable — completion disabled"; editor continues.
- JSON-RPC or protocol error on a request: that `Result` carries `Err`; the
  popup stays closed for it; the editor is unaffected.
- Subprocess exits unexpectedly: the provider is marked dead and disabled
  (no automatic restart in MVP); a notice is shown once.

## Testing

- **Engine** (unit, fake provider): debounce collapses a burst of edits into one
  dispatch; a newer edit cancels the prior request's context; a stale-version
  result is dropped.
- **lsp.Client** (unit, in-memory pipe + scripted fake server): `Content-Length`
  framing round-trips; requests and responses match by id; an `initialize`
  handshake and a `completion` response decode into `Item`s. No binary needed.
- **Editor / popup** (simulated tcell screen): popup opens on canned results;
  Up/Down/Enter/Tab/Esc behave per the table; accept inserts and replaces the
  current word correctly; normal key behavior is unchanged when the popup is
  closed.
- **Non-fatal degradation** (unit): a provider whose start fails (bad command)
  or whose `Complete` returns an error produces a `Result` with `Err` set and no
  popup; the editor keeps handling keys normally and shows the disabled notice
  once. Proves the best-effort guarantee without needing a real failure.
- **Integration smoke test** (`//go:build lsp_integration`, and `t.Skip` when
  `gopls` is not on `PATH`): start real `gopls` on a temp `.go` file and assert a
  known completion (e.g. a `fmt` member) appears. CI stays green without
  `gopls`.

## Known limitation

LSP character positions are **UTF-16 code units** by default. The MVP advertises
a utf-8 `positionEncoding` preference; if `gopls` declines, positions are correct
for ASCII lines only. This is the same rune/width gap tracked as **R4** in
`2026-07-17-conventions-and-refactor-spec.md`; it is cross-referenced here, not
solved in this feature.

## Suggested implementation phasing

1. `lsp.Client`: JSON-RPC framing + protocol types + client unit tests.
2. `completion`: `Provider`/`Item`/`Document`/`Result` types, `Engine`
   (debounce/cancel/staleness) with the fake provider + engine unit tests.
3. `completion` LSP provider + filetype registry, backed by `lsp.Client`.
4. `render.DrawCompletion` popup + render tests.
5. `editor` wiring: didOpen/didChange, PostEvent bridge, popup state and keys,
   simulated-screen tests.
6. Integration smoke test against real `gopls`.

## Success criteria

- Typing in a `.go` file surfaces a completion popup driven by real `gopls`,
  and accepting inserts the selection.
- The UI never blocks or crashes when `gopls` is slow, missing, or failing.
- The engine works against the fake provider with no subprocess, and all unit
  tests pass without `gopls` installed.
- Adding a second language server is a one-line registry entry, no engine
  changes.
- `buffer`, `fileio`, `filebrowser`, `theme`, and `completion` remain free of
  tcell; the buffer is never accessed off the UI thread.
