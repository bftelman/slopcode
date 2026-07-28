# AGENTS.md

Guidance for agentic coding tools working in this repository.

## Package layout and dependency rules

| Path | Responsibility | May import |
| --- | --- | --- |
| `internal/buffer` | Text model, cursor movement, editing, undo/redo. UI-free. | stdlib only |
| `internal/fileio` | Reading and writing files. | stdlib only |
| `internal/autopair` | Bracket/quote completion on top of the buffer. | `buffer` |
| `internal/filebrowser` | Directory listing and navigation. UI-free. | stdlib only |
| `internal/highlight` | Source text -> colored runes via chroma; exposes theme colors for reuse. | chroma, tcell |
| `internal/lsp` | Minimal JSON-RPC client for LSP servers (subprocess, doc sync, completion). UI-free. | stdlib only |
| `internal/completion` | UI-free completion engine: actor loop, debounce, provider dispatch, gopls provider. | `lsp` |
| `internal/render` | Draws the status bar, gutter, text, sidebar, splash, and completion popup. | tcell, chroma, `buffer`, `filebrowser`, `highlight`, `completion` |
| `internal/editor` | Event loop tying input, buffer, completion, and rendering together. | everything above |
| `main.go` | Argument parsing and startup. | `editor` |

**`lsp` and `completion` must never import `tcell`, `buffer`, or `render`.** They are
UI-free by design so the completion engine can be tested and reasoned about without a
terminal. Only `internal/editor` is allowed to wire every package together.

## Invariants

- The `buffer` is **UI-thread-only**: never touch it from a goroutine. The completion
  engine only ever receives immutable `completion.Document` snapshots, never a `*buffer.Buffer`.
- Completion is **best-effort and non-fatal**: a missing or failing `gopls` (or any other
  provider) must never crash, block, or panic the editor — see
  `completion.LSPRegistry`'s `(nil, nil)` returns and the non-fatal-degradation tests in
  `internal/editor/editor_test.go`.
- Document versioning is **editor-owned** (`Editor.docVersion`), not tracked in `buffer`.
  It increments on every mutating key, including undo/redo, so stale completion results
  (tagged with the version they ran against) can be dropped safely.
- File URIs must go through `completion.PathToFileURI` (not ad hoc string concatenation).
  A bare `"file:///" + absPath` produces a malformed four-slash URI on Unix, since
  `absPath` already starts with `/` — this broke real gopls symbol resolution once already
  (fixed in the `feat/autocomplete-lsp` branch history).
- Syntax highlighting and the completion popup share one theme: `render.StyleName` picks
  the chroma style (`internal/highlight.Style`/`BackgroundStyle`) both draw from. Change
  that one constant to re-theme both together; don't hardcode colors in `render/completion.go`.

## Commits

Conventional Commits with a package scope (`feat(lsp):`, `feat(completion):`,
`feat(render):`, `feat(editor):`, `fix(...)`, `test(...)`, `docs:`).
