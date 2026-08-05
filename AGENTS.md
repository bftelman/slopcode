# AGENTS.md

Guidance for agentic coding tools working in this repository.

## Package layout and dependency rules

| Path | Responsibility | May import |
| --- | --- | --- |
| `internal/buffer` | Text model, cursor movement, editing, undo/redo. UI-free. | stdlib only |
| `internal/fileio` | Reading and writing files. | stdlib only |
| `internal/autopair` | Bracket/quote completion on top of the buffer. | `buffer` |
| `internal/filebrowser` | Directory listing and navigation. UI-free. | stdlib only |
| `internal/textsearch` | Literal substring find + match stepping over `[]string`. UI-free. | stdlib only |
| `internal/picker` | Fuzzy-picker candidate sources (rg/fd/walk file listing, buffer lines) + async ranking engine. UI-free. | `sahilm/fuzzy`, stdlib |
| `internal/highlight` | Source text -> colored runes via chroma; exposes theme colors for reuse. | chroma, tcell |
| `internal/lsp` | Minimal JSON-RPC client for LSP servers (subprocess, doc sync, completion). UI-free. | stdlib only |
| `internal/completion` | UI-free completion engine: actor loop, debounce, provider dispatch, gopls provider. | `lsp` |
| `internal/render` | Draws the status bar, gutter, text, sidebar, splash, completion popup, find bar, and picker overlay. | tcell, chroma, `buffer`, `filebrowser`, `highlight`, `completion`, `textsearch`, `picker` |
| `internal/editor` | Event loop tying input, buffer, completion, search, pickers, and rendering together. | everything above |
| `main.go` | Argument parsing and startup. | `editor` |

**`lsp`, `completion`, `textsearch`, and `picker` must never import `tcell`, `buffer`,
or `render`.** They are UI-free by design so they can be tested and reasoned about
without a terminal — buffer contents reach `textsearch` and `picker` as a plain
`[]string`. Only `internal/editor` is allowed to wire every package together.

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
- Syntax highlighting, the completion popup, and the picker overlay share one theme:
  `render.StyleName` picks the chroma style (`internal/highlight.Style`/`BackgroundStyle`)
  they all draw from. Change that one constant to re-theme them together; don't hardcode
  colors in `render/completion.go` or `render/picker.go`. Search-match highlighting in
  `render/render.go` uses **attributes** (`Reverse`/`Underline`) rather than colors for
  the same reason — it has to compose with whatever foreground the theme gave the glyph.
- Picker generations are **editor-owned** (`Editor.pickGen`), like `docVersion`: the
  editor passes a generation to `picker.Engine.Open` and every `Result` echoes it, so a
  late ranking pass for a picker the user already closed is dropped. The engine does not
  keep its own counter — only the editor knows which picker is on screen.
- **Never bind `Ctrl+H`, `Ctrl+I`, `Ctrl+J`, or `Ctrl+M`.** tcell's Unix input parser
  maps those bytes to `KeyBackspace`/`KeyTab`/`KeyLF`/`KeyEnter` *before* the `KeyCtrl*`
  range, while the Windows console path reports them as real `KeyCtrl*` events. A binding
  on any of them works on Windows and silently breaks on Linux and macOS — and the
  simulation-screen tests inject `Key` values directly, so they cannot catch it.
- Case-insensitive search folds with `textsearch.foldASCII`, **not** `strings.ToLower`.
  Unicode lowering can change a string's byte length (U+0130 lowers to 3 bytes / 2 runes),
  which would desynchronize match offsets from the original line. The documented cost is
  that non-ASCII letters always compare case-sensitively.
- Replace-all applies matches in **reverse document order** under a single
  `buffer.Checkpoint`, so unapplied offsets stay valid and one undo reverts the sweep.
  `buffer.ReplaceRange` deliberately takes no checkpoint; callers own undo granularity.
- Search uses `strings.Index`, not a hand-rolled Boyer-Moore. That is measured, not
  assumed: stdlib wins or ties at every needle length because it dispatches to SIMD
  assembly in `internal/bytealg`. See the design spec and `textsearch.BenchmarkFindAll`.

## Commits

Conventional Commits with a package scope (`feat(lsp):`, `feat(completion):`,
`feat(render):`, `feat(editor):`, `fix(...)`, `test(...)`, `docs:`).
