# slopcode — Enhancements Design (notifications, soft tabs, syntax highlighting)

**Date:** 2026-07-15
**Module:** `github.com/bftelman/slopcode`
**Builds on:** `2026-07-15-mvp-text-editor-design.md`

## Overview

Three additions to the MVP editor:

1. **Save notification** — a transient statusbar message shown on save, cleared on the next keypress.
2. **Soft tabs** — the Tab key inserts spaces to the next tab stop; literal tabs in loaded files render expanded but are never rewritten.
3. **Syntax highlighting** — per-token colors via the `chroma` library, chosen by file extension.

The existing architecture is preserved: `buffer` and `fileio` stay pure (stdlib only);
`render`, `editor`, and a new `highlight` package are the UI layer (may import tcell/chroma).

## Feature 1: Save Notification

- `editor.Editor` gains a `notice string` field (replacing the ad-hoc `status` field).
- On **Ctrl+S**:
  - success → `notice = "<filename> saved"`.
  - failure → `notice = "SAVE ERROR: <err>"`.
- At the top of `handleKey`, clear `notice` for every key **before** dispatch. Ctrl+S then
  re-sets it, so the message survives its own keypress and disappears on the next one.
- `render.Draw` receives `notice` and draws it in the statusbar, just left of `Ln/Col`,
  in a distinct color (green fg for save, red fg for error is out of scope — one accent color).
  The filename + line count remain on the left.

## Feature 2: Soft Tabs

- Constant `TabWidth = 4` (in `editor`, passed where needed).
- New pure method `func (b *Buffer) InsertTab(width int)` — inserts
  `width - (col % width)` spaces (advance to next tab stop), moving the cursor with them.
- The **Tab** key calls `b.InsertTab(TabWidth)` instead of `InsertRune('\t')`.
- Backspace is unchanged: it deletes one space at a time.
- **Loaded literal tabs are never corrupted.** `fileio` and `Save` are unchanged. The
  buffer may still contain `\t` from a loaded file; rendering expands each `\t` to the next
  tab stop *visually only*.

## Feature 3: Syntax Highlighting

New package `internal/highlight` (UI layer):

- `type StyledRune struct { R rune; Style tcell.Style }`
- `func Highlight(text, filename, styleName string) [][]StyledRune`
  1. Lexer: `lexers.Match(filename)`; fall back to `lexers.Fallback`.
  2. Coalesce with `chroma.Coalesce(lexer)`; tokenise the whole text.
  3. Style: `styles.Get(styleName)` (default `monokai`); for each token get the
     `chroma.StyleEntry` for its type; if `entry.Colour.IsSet()`, convert via
     `tcell.NewRGBColor(r,g,b)` using the colour's R/G/B. Apply bold/italic/underline
     from the entry's `Trilean` flags (`== chroma.Yes`).
  4. Split token values on `\n` into per-line `[]StyledRune`. Output is 1:1 with the source
     characters (including tabs/spaces), so buffer column indices map directly.
- Constant `StyleName = "monokai"` lives in `render` (or `editor`) and is passed through.

### Rendering with styles + tabs

`render.Draw(s, b, filename, notice, modified, scroll)`:
- Calls `highlight.Highlight(strings.Join(b.Lines(), "\n"), filename, StyleName)`.
- For each visible line, walks its `[]StyledRune`, tracking a screen x:
  - `\t` → advance x to the next multiple of `TabWidth` (drawing spaces), styled default.
  - other rune → draw at x with its style, x++.
  - stop when x reaches the screen width (clip).
- Cursor screen-x uses the same tab-aware mapping over the cursor's line up to its column:
  `screenCol(line string, byteCol, tabWidth int) int`.
- Gutter (line numbers) and statusbar are unchanged except for the new notice segment.

Re-highlighting the whole buffer on every redraw is acceptable for MVP-sized files. No caching.

## Interfaces (exact)

- `buffer`: `func (b *Buffer) InsertTab(width int)`
- `highlight`:
  - `type StyledRune struct { R rune; Style tcell.Style }`
  - `func Highlight(text, filename, styleName string) [][]StyledRune`
- `render`:
  - `const TabWidth = 4`
  - `const StyleName = "monokai"`
  - `func GutterWidth(lineCount int) int` (unchanged)
  - `func Draw(s tcell.Screen, b *buffer.Buffer, filename, notice string, modified bool, scroll int)`
  - `func screenCol(line string, byteCol, tabWidth int) int` (unexported helper)
- `editor`:
  - `Editor.notice string`
  - Tab key → `InsertTab(render.TabWidth)`

## Testing

- `buffer.InsertTab`: unit tests — from col 0 (4 spaces), from col 2 (2 spaces to stop 4),
  from col 4 (4 spaces to stop 8).
- `highlight.Highlight`: unit tests — Go source produces >1 line; a keyword token gets a
  non-default color; character stream round-trips (concatenating all `StyledRune.R` across
  lines, rejoined with `\n`, equals the input).
- `render.screenCol`: unit tests — no tabs (== byteCol), leading tab (→ TabWidth), tab after
  two chars (→ next stop).
- `editor`: extend the simulation-screen test — Tab inserts spaces; after Ctrl+S the notice
  is set and a following keypress clears it.

## Out of Scope (YAGNI)

- Configurable tab width / theme via flags or config file (constants only).
- Converting tabs↔spaces in file content (retab).
- Incremental / cached highlighting.
- Per-error notice colors, notice timers.
- Multi-width (CJK) glyph handling beyond tabs.
