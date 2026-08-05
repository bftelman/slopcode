# namlet — MVP Text Editor Design

**Date:** 2026-07-15
**Module:** `github.com/bftelman/namlet`

## Overview

A fullscreen terminal text editor written in Go using [`tcell`](https://github.com/gdamore/tcell).
You launch it with a filename, type directly into the buffer, move with arrow keys,
save with Ctrl+S, and quit with Ctrl+Q. Line numbers are shown on the left in a gutter,
and a statusbar across the top shows the filename, line count, cursor position, and a
modified marker.

This is "vim inspired" in look and feel only — it is **not** modal. Typing inserts text
directly; there are no normal/insert modes and no `:` command line.

## Requirements

- Launch: `namlet <filename>` (filename is **required** for the MVP).
- If the file exists, load its contents; if not, start with an empty buffer.
- The file is created on disk only on first save.
- Edit text: insert printable characters, Enter to split lines, Backspace to delete.
- Move: arrow keys (clamped to valid positions), Home/End for line start/end.
- Save: Ctrl+S. Quit: Ctrl+Q.
- Handle terminal resize (re-render).
- Vertical scrolling when the file is taller than the screen.
- Long lines **clip** at the right edge (no wrapping) for the MVP.

## Layout

```
 notes.txt — 12 lines            Ln 3, Col 5  [modified]   <- statusbar (row 0, inverted)
  1 │ package main                                          <- gutter + text
  2 │
  3 │ func main() {
  ...
```

- Row 0: statusbar (inverted colors). Left: filename + line count. Right: cursor
  position and `[modified]` marker when there are unsaved changes.
- Rows 1..N-1: line-number gutter + text content.

## Components

Each component has one purpose, a clear interface, and can be understood independently.

### 1. `buffer` (pure logic, no tcell — unit tested)
The text model.

- State: `lines []string`, cursor `row, col int`.
- Operations:
  - `InsertRune(r rune)` — insert at cursor, advance column.
  - `InsertNewline()` — split current line at cursor; move to start of new line.
  - `Backspace()` — delete char before cursor; at column 0, join with previous line.
  - `MoveLeft/Right/Up/Down()` — move cursor, clamped to line lengths.
  - `MoveHome/MoveEnd()` — start/end of current line.
  - Accessors: `Lines()`, `Cursor()`, line count.
- Invariants: `lines` always has ≥ 1 line; cursor always within bounds.

### 2. `fileio` (pure logic — unit tested)
- `Load(path string) ([]string, error)` — read file into lines; missing file → empty
  buffer (one empty line), not an error.
- `Save(path string, lines []string) error` — write lines joined by `\n`.

### 3. `render` (draws to a tcell screen)
Given a screen, a buffer, and a scroll offset, draws:
- the statusbar (filename, line count, cursor pos, modified marker),
- the gutter (right-aligned line numbers + separator),
- visible text lines (clipped horizontally).
Computes gutter width from the number of lines.

### 4. `editor` (event loop)
Owns a buffer, a tcell screen, the file path, a scroll offset, and a `modified` flag.
Loop: poll event → dispatch (key → buffer op / save / quit; resize → redraw) → adjust
scroll so the cursor stays visible → render.

### 5. `main`
Parse the filename argument, load the file, construct and run the editor.

## Data Flow

```
main → fileio.Load → buffer
                       │
        ┌──────────────┴───────────────┐
        │            editor loop        │
        │  tcell event → buffer mutate  │
        │  → scroll adjust → render      │
        └──────────────┬───────────────┘
                        │
         Ctrl+S → fileio.Save(path, buffer.Lines())
```

## Error Handling

- Missing filename arg → print usage to stderr, exit non-zero.
- Load error other than "not found" (e.g. permission) → print error, exit non-zero
  before entering fullscreen.
- Save error → show the error message in the statusbar; do not crash or quit.
- tcell init failure → print error, exit non-zero.

## Testing

- `buffer`: real unit tests covering insert, newline splitting, backspace (including
  line joins at column 0), and cursor movement/clamping edge cases (start/end of buffer,
  short/long adjacent lines).
- `fileio`: unit tests for load (existing file, missing file) and save round-trip using
  a temp directory.
- `render` and `editor`: validated primarily by running the app; light or no automated
  coverage for the tcell-dependent parts.

## Out of Scope (YAGNI for MVP)

- Modal editing, `:` commands, macros.
- Line wrapping, horizontal scrollbar.
- Undo/redo, copy/paste, search/replace.
- Syntax highlighting (despite the gutter looking code-friendly).
- Multiple buffers / tabs / splits.
- Mouse support.
