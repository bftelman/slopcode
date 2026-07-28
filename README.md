# slopcode

A fullscreen terminal text editor written in Go. It opens or creates a file, lets you
type directly, and saves. The look is inspired by vim (fullscreen, line numbers, a status
bar), but it is not modal: you type into the buffer straight away, no insert or normal mode.

## Features

- Open an existing file or start a new one.
- Line numbers in a left gutter and a status bar across the top showing the filename, line
  count, cursor position, and an unsaved marker.
- Syntax highlighting by file type, using the chroma library (monokai theme).
- Soft tabs: the Tab key inserts spaces up to the next tab stop. Literal tabs in a file you
  open are shown expanded and left untouched on disk.
- Bracket and quote auto-completion: typing an opening bracket or quote inserts its closing
  pair, typing the closing character steps over it, and deleting an empty pair removes both
  characters.
- A file browser sidebar (Ctrl+B) for moving between files without leaving the editor.
- Automatic code completion for Go files, backed by `gopls` over LSP. A popup appears as
  you type an identifier or after `.`; each item shows a kind tag (`[F]` function, `[V]`
  variable, `[C]` constant, `[P]` field, `[K]` keyword, `[T]` type, `[M]` module) colored
  from the same chroma theme as syntax highlighting. Up/Down selects, Enter or Tab accepts,
  Esc dismisses. Completion is best-effort: without `gopls` installed, editing is unaffected
  and no popup appears.
- Undo and redo.
- A welcome screen when you start with no filename.
- Save notifications and an unsaved-changes guard on quit.

## Requirements

- Go 1.25 or newer.
- A terminal. Windows, macOS, and Linux are supported through the tcell library.
- Optional: [`gopls`](https://pkg.go.dev/golang.org/x/tools/gopls) on `$PATH`, `$GOBIN`, or
  `$(go env GOPATH)/bin`, for Go code completion. Its absence is not an error — completion
  is simply disabled.

## Build and run

Run directly from source:

```
go run . path/to/file.go
```

Start with no file to see the welcome screen:

```
go run .
```

Build a binary:

```
go build -o slopcode .
./slopcode path/to/file.go
```

If the file does not exist, it is created on the first save. Starting without a filename
and then saving writes to `untitled.txt` in the current directory.

## Keys

Editing:

| Key | Action |
| --- | --- |
| Arrow keys | Move the cursor |
| Home / End | Jump to start or end of the line |
| Enter | Split the line |
| Backspace | Delete the character before the cursor |
| Tab | Insert spaces to the next tab stop |
| Ctrl+S | Save |
| Ctrl+Z | Undo |
| Ctrl+Y | Redo |
| Ctrl+B | Open the file browser |
| Ctrl+Q | Quit (blocked while there are unsaved changes) |

Completion popup (while open):

| Key | Action |
| --- | --- |
| Up / Down | Move the selection |
| Enter or Tab | Accept the selected item |
| Esc | Dismiss the popup |

File browser:

| Key | Action |
| --- | --- |
| Up / Down | Move the selection |
| Enter | Open a file, or step into a folder |
| Ctrl+S | Save the current buffer |
| Ctrl+B or Esc | Close the browser |

## Saving and quitting

The status bar shows a marker while the buffer differs from the version on disk. The marker
clears once the content matches what was saved, so editing and then reverting back to the
original leaves the file marked as unchanged. Ctrl+Q refuses to quit while there are unsaved
changes; save first with Ctrl+S, then quit.

## Project layout

The code is split into small packages, each with one job:

| Path | Responsibility |
| --- | --- |
| `internal/buffer` | Text model, cursor movement, editing, and undo/redo. No terminal code. |
| `internal/fileio` | Reading and writing files. |
| `internal/autopair` | Bracket and quote completion on top of the buffer. |
| `internal/filebrowser` | Directory listing and navigation. |
| `internal/highlight` | Turns source text into colored runes with chroma. |
| `internal/lsp` | Minimal JSON-RPC client for LSP servers (subprocess, doc sync, completion). |
| `internal/completion` | UI-free completion engine: debounce, provider dispatch, gopls provider. |
| `internal/render` | Draws the status bar, gutter, text, sidebar, welcome screen, and completion popup. |
| `internal/editor` | The event loop that ties input, buffer, completion, and rendering together. |
| `main.go` | Argument parsing and startup. |

## Tests

```
go test ./...
```

The `buffer`, `fileio`, `autopair`, `filebrowser`, `lsp`, `completion`, and `highlight`
packages have unit tests. The editor and render packages are checked through a simulated
screen that drives the real event loop and inspects the resulting cells.

A real-`gopls` integration test is excluded from the default run (`//go:build lsp_integration`)
since it needs `gopls` installed. Run it explicitly with:

```
go test -tags lsp_integration ./internal/completion/ -run TestGoplsRealCompletion -v
```
