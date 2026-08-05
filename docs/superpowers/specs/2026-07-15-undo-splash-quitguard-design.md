# namlet — Undo/Redo, Splash Screen, Quit Guard Design

**Date:** 2026-07-15
**Module:** `github.com/bftelman/namlet`
**Builds on:** prior specs in this folder.

## Overview

Three finishing features:

1. **Undo/redo** — per-change snapshots, Ctrl+Z / Ctrl+Y.
2. **Splash screen** — launched with no filename, show an ASCII banner
   ("NAMLET" + "by @bftelman") until the first keystroke.
3. **Unsaved-changes handling** — the `[modified]` marker shows whenever there are unsaved
   changes (already implemented); Ctrl+Q hard-blocks while modified; an unnamed buffer saves
   to `untitled.txt`.

Pure logic (undo/redo) lives in `buffer`; `render` gains the splash; `editor` wires keys,
splash gating, save fallback, and the quit guard.

## Feature 1: Undo/Redo

In `internal/buffer`:

- Internal `type state struct { lines []string; row, col int }`.
- Fields `undo []state`, `redo []state`; `const maxUndo = 500`.
- `func (b *Buffer) Checkpoint()` — push a deep copy of the current state onto `undo`
  (trimming the oldest if over `maxUndo`) and clear `redo`. Called by the editor **once
  before each mutating action**.
- `func (b *Buffer) Undo() bool` — if `undo` empty, return false; push current state to
  `redo`, pop `undo` into the buffer, return true.
- `func (b *Buffer) Redo() bool` — symmetric.
- Deep copy: `cp := make([]string, len(lines)); copy(cp, lines)` (strings are immutable).

Editor wiring:
- Before `InsertRune`/`InsertTab`/`InsertNewline`/`Backspace` (and before an `autopair`
  call, so a pair insertion is one step): `e.b.Checkpoint()`.
- **Ctrl+Z** → `if e.b.Undo() { e.modified = true }`.
- **Ctrl+Y** → `if e.b.Redo() { e.modified = true }`.
- Cursor-only movement does NOT checkpoint.

## Feature 2: Splash Screen

- `main.go` accepts **0 or 1** arguments. With 0 args: `fileio.Load` is skipped, buffer is
  `buffer.New(nil)` (one blank line), and `path == ""`.
- Splash condition: `e.path == "" && !e.modified`. The first mutating key sets `modified`,
  so the splash disappears and normal editing renders.
- `render.DrawSplash(s tcell.Screen, filename, notice string)`:
  - Draws a statusbar (row 0): `filename` on the left, `notice` and `[modified]` handling
    not needed (splash implies unmodified), but the same green notice slot is honored.
  - Draws the banner (see below) centered horizontally, vertically centered in the text area.
  - Hides the cursor (nothing to edit yet) — or places it at the text origin.
- Editor passes the display name: `"[No Name]"` when `path == ""`, else `path`. This display
  name is also used by the normal `Draw` statusbar.

### Banner art (stored in `render`)

```
█████ █     █████ █████ █████ █████ ████  █████
█     █     █   █ █   █ █     █   █ █   █ █
█████ █     █   █ █████ █     █   █ █   █ █████
    █ █     █   █ █     █     █   █ █   █ █
█████ █████ █████ █     █████ █████ ████  █████
```
with the subtitle `by @bftelman` centered below.

## Feature 3: Unsaved-Changes Handling

- **Modified marker:** `render.Draw` already appends `  [modified]` to the statusbar when
  `modified` is true, in both editing and browse views. No change needed; this is the
  "shown anytime there are unsaved changes" behavior.
- **Save fallback:** `editor.save()` — if `e.path == ""`, set `e.path = "untitled.txt"`
  (working dir) before saving; notice reflects the adopted name. The display name updates
  accordingly, so a saved unnamed buffer stops showing `[No Name]`.
- **Hard quit guard:** a shared `func (e *Editor) tryQuit() bool`:
  - if `e.modified`: set notice `"unsaved changes — Ctrl+S to save before quitting"`,
    return false (do not quit).
  - else return true.
  Both `handleKey` and `handleBrowseKey` route **Ctrl+Q** through `tryQuit()`.
  Because an unnamed buffer saves to `untitled.txt`, Ctrl+S always clears `modified`, so
  the guard never dead-ends.

## Rendering helper fix

`render.drawText` currently advances by **byte** index (`for i, r := range text`), which
misplaces multi-byte runes (the banner's `█`, and the statusbar em dash). Change it to
advance by a rune **column** counter so multi-byte glyphs render at consecutive columns.
Centering in `DrawSplash` uses rune counts (`utf8.RuneCountInString`), not byte length.

## Testing

- `buffer` undo/redo: edit → `Undo` restores prior lines+cursor and returns true; `Redo`
  reapplies; `Undo` on empty stack returns false; a new `Checkpoint` after undo clears redo.
- `editor` (simulation screen):
  - Ctrl+Z after typing a char reverts the buffer; Ctrl+Y reapplies.
  - Ctrl+Q while modified returns false and sets the notice; after Ctrl+S it returns true.
  - Unnamed save: with `t.Chdir(tmp)` and `path == ""`, Ctrl+S writes `untitled.txt` and
    sets `e.path`.
- `render`: `DrawSplash` output contains the banner (a cell scan finds `█`) and the subtitle
  text `bftelman`; `drawText` places a multi-byte string at consecutive columns.

## Out of Scope (YAGNI)

- Grouped/coalesced undo (each change is its own step).
- Undo memory beyond the 500-entry cap; persisting history across sessions.
- A text prompt for "save as" (unnamed always → `untitled.txt`).
- Configurable banner / themes.
