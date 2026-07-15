# slopcode — Auto-pairs & File Browser Design

**Date:** 2026-07-15
**Module:** `github.com/bftelman/slopcode`
**Builds on:** the MVP and enhancements specs (same folder).

## Overview

Two additions:

1. **Smart auto-pairs** — typing a bracket/quote inserts its match; typing a closer over an
   existing one steps past it; backspacing an empty pair deletes both.
2. **Side-by-side file browser** — Ctrl+B toggles a left sidebar listing the current
   directory; Up/Down move the selection, Enter descends into folders or opens files.

Pure logic stays free of tcell: new packages `autopair` and `filebrowser` are unit-tested.
`render` and `editor` remain the UI layer.

## Feature A: Smart Auto-Pairs

New package `internal/autopair` (imports only `buffer`).

- Pair table: `(`→`)`, `[`→`]`, `{`→`}`, `"`→`"`, `'`→`'`, `` ` ``→`` ` ``.
- `func InsertRune(b *buffer.Buffer, r rune)`:
  1. **Skip-over:** if `r` is a closing char and the rune at the cursor equals `r`,
     `b.MoveRight()` and return. (Also lets a re-typed quote step out of a pair.)
  2. **Auto-close:** if `r` is an opener, insert `r` then its close, then `b.MoveLeft()`
     so the cursor sits between them.
  3. Otherwise `b.InsertRune(r)`.
- `func Backspace(b *buffer.Buffer)`: if the rune before the cursor is an opener and the
  rune at the cursor is its matching closer (empty pair), delete both
  (`MoveRight`, `Backspace`, `Backspace`); otherwise `b.Backspace()`.

Supporting buffer addition:
- `func (b *Buffer) RuneAt(offset int) (rune, bool)` — rune at `col+offset` in the current
  line (byte-indexed; adequate for ASCII pairs). Returns false when out of range.

Editor wiring: `KeyRune → autopair.InsertRune(b, r)`, `Backspace → autopair.Backspace(b)`.

## Feature B: Side-by-Side File Browser

New package `internal/filebrowser` (uses `os.ReadDir`; no tcell).

- `type Entry struct { Name string; IsDir bool }`
- `type Browser struct { ... }`
- `func Open(dir string) (*Browser, error)` — builds entries: `..` first, then directories
  (sorted), then files (sorted). Selection starts at 0. `dir` is cleaned to absolute.
- `func (b *Browser) Entries() []Entry`
- `func (b *Browser) Dir() string`
- `func (b *Browser) SelIndex() int`
- `func (b *Browser) Selected() Entry`
- `func (b *Browser) MoveUp()` / `MoveDown()` — clamped to `[0, len-1]`.
- `func (b *Browser) Enter() (path string, isDir bool, err error)`:
  - selected is a directory (incl. `..`) → set dir to `filepath.Clean(filepath.Join(dir, name))`,
    reload entries, reset selection, return `("", true, err)`.
  - selected is a file → return `(filepath.Join(dir, name), false, nil)`.

### Editor integration

`Editor` gains `browser *filebrowser.Browser` (nil = editing, non-nil = browsing) and a
`pendingOpen string` (unsaved-guard latch).

Key dispatch splits on mode at the top of `handleKey`:

- **Editing mode** (browser nil): existing behavior, plus **Ctrl+B** → open browser at
  `filepath.Dir(e.path)` (or `.` if empty).
- **Browsing mode** (browser non-nil):
  - **Ctrl+B** or **Esc** → close browser (`browser = nil`, clear `pendingOpen`).
  - **Up/Down** → `MoveUp/MoveDown`; clears `pendingOpen`.
  - **Ctrl+S** → save current buffer to `e.path` (so the user can save before switching).
  - **Enter**:
    - dir → `Enter()` navigates in; clears `pendingOpen`.
    - file → if `e.modified` and `pendingOpen != path`: set `pendingOpen = path`,
      notice `"unsaved changes — Ctrl+S, or Enter again to discard"`, return.
      Otherwise open it: `fileio.Load(path)` → new buffer, `e.path = path`,
      `scroll = 0`, `modified = false`, `browser = nil`, `pendingOpen = ""`,
      notice `"<path> opened"`. Load error → notice, stay in browser.
  - Other keys ignored.

### Rendering (side-by-side)

- `const SidebarWidth = 30`
- `render.Draw` signature becomes:
  `func Draw(s tcell.Screen, b *buffer.Buffer, filename, notice string, modified bool, scroll, originX int, showCursor bool)`
  - Draws the editor into columns `[originX, width)`: statusbar fills that span; gutter at
    `originX`; text at `originX+gutter`; clip at `width`. Cursor at
    `originX + gutter + screenCol(...)`, hidden when `showCursor` is false.
- `render.DrawSidebar(s tcell.Screen, br *filebrowser.Browser)`
  - Fills columns `[0, SidebarWidth)`. Row 0: a header showing the dir basename (inverted).
    Rows 1..: entries, dirs suffixed `/`, names truncated to fit; the selected row is
    inverted. Auto-scrolls so the selection stays visible. A `│` separator sits at column
    `SidebarWidth-1`.
- `editor.draw()`:
  - browsing → `Draw(..., originX = SidebarWidth, showCursor = false)` then
    `DrawSidebar(...)` (drawn second so it overlays the left region).
  - editing → `Draw(..., originX = 0, showCursor = true)`.
  - `Draw` and `DrawSidebar` each clear/show as today; drawing the sidebar last keeps it on top.

## Testing

- `buffer.RuneAt`: at cursor, before cursor, out-of-range both ends.
- `autopair`: `(`→`()` cursor between; `)` skip-over; `"`→`""`; `"` skip-over; backspace
  empty pair deletes both; backspace non-pair deletes one; plain char unaffected.
- `filebrowser` (temp dir): `Open` ordering (`..`, dirs, files, sorted); `MoveDown/Up`
  clamp; `Enter` into a subdir changes `Dir()` and reloads; `Enter` on a file returns its
  full path; `..` navigates to parent.
- `editor` (simulation screen): Ctrl+B opens browser; Down + Enter opens a file into the
  buffer; unsaved guard — after editing, Enter on a file sets the notice and does NOT switch,
  a second Enter switches; Ctrl+B closes.
- `render`: update existing `Draw` calls for the new signature; add a `DrawSidebar` sim test
  asserting the selected entry and directory names appear in the left columns.

## Out of Scope (YAGNI)

- Resizable / hideable-width sidebar, mouse selection.
- Creating/renaming/deleting files from the browser.
- Fuzzy find / filtering, hidden-file toggle (dotfiles are listed as normal entries).
- Multiple open buffers or tabs.
- Auto-pair awareness of strings/comments/escapes (always pairs).
