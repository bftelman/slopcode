# Spec: Search & replace, and telescope-style fuzzy finding

Date: 2026-08-05
Status: Design approved.

## Goal

Two related features:

1. **Literal search & replace** in the current buffer, driven by an incremental
   prompt line at the bottom of the screen. Type and matches highlight as you
   go; step through them; replace one or all.
2. **A telescope-style fuzzy picker** with two sources: files by path across the
   project, and lines within the current buffer.

Both are non-modal, in keeping with the editor: there is no search mode, only a
prompt that is either open or closed.

## Decisions (locked during brainstorming)

| Question | Decision |
| --- | --- |
| Picker sources (v1) | Files by path (recursive) + lines in the current buffer |
| Search UX | Prompt line at the bottom, incremental, all matches highlighted |
| Replace UX | Per-match `Ctrl+R` and `Ctrl+A` replace-all; no confirm sub-mode |
| Match options | Smart case only — no regex, no whole-word, no case toggle |
| Candidate listing | `rg --files` -> `fd --type f` -> `filepath.WalkDir` fallback |
| Fuzzy ranking | `github.com/sahilm/fuzzy`, in-process (not the `fzf` binary) |
| Substring search | Go's `strings.Index` — **not** a hand-rolled Boyer-Moore |
| Picker root | Nearest ancestor containing `.git`, else the current file's directory |

## Non-goals (v1)

- No regular expressions, whole-word matching, or an explicit case-sensitivity
  toggle. Smart case covers the common need without a persistent option flag.
- No live grep across files. The picker never searches file *contents*; only
  paths, and lines of the already-open buffer.
- No multi-buffer picker — slopcode holds one buffer at a time, so an "open
  buffers" source would first require multi-buffer support.
- No preview pane in the picker. Telescope's is iconic but needs per-selection
  file loading and a second highlighted pane; deferred.
- No search history, no incremental "search in selection" (there is no
  selection model), no replace across files.

## The Boyer-Moore question

**Decision: use `strings.Index`. Do not implement Boyer-Moore.**

Measured on a 5,250,689-byte / 187,367-line Go corpus (Intel i5-1235U,
`go1.25.4`, windows/amd64), doing a full find-all pass. All three
implementations were cross-checked to return identical hit counts before timing:

| Needle | `strings.Index` | BM-Horspool | Full Boyer-Moore |
| --- | --- | --- | --- |
| 3 B `col` (5312 hits) | **1.24 ms** | 6.53 ms | 6.92 ms |
| 9 B `Highlight` (507 hits) | **0.14 ms** | 2.29 ms | 2.38 ms |
| 28 B function signature (40 hits) | **1.18 ms** | 1.21 ms | 1.22 ms |
| 12 B absent needle (0 hits) | **0.23 ms** | 2.56 ms | 2.29 ms |

Boyer-Moore never wins; at best it ties, on the longest needle. The reason:
`strings.Index` dispatches to `internal/bytealg.IndexString`, which is
hand-written SIMD assembly on amd64/arm64 for needles up to 32 bytes, and for
longer needles uses `IndexByte`-accelerated candidate scanning with a Rabin-Karp
fallback that bounds worst-case degradation. Comparing 16-32 bytes per
instruction beats Boyer-Moore's scalar skip-table bookkeeping until needles grow
far longer than anything a user types into a find bar.

Searching **per line** — which is what the editor wants, since a line-relative
offset maps directly to a `(row, col)` cursor position — costs 2.2-4.1 ms for
that same 5 MB. For a typical source file under 100 KB that is roughly 40-80 us
for a complete find-all, per keystroke. Substring search is therefore *not* a
place this feature needs to spend design effort.

A `BenchmarkFindAll` ships in `internal/textsearch` so this conclusion stays
measurable rather than remembered. The Boyer-Moore implementations were
throwaway benchmark scaffolding and are deliberately **not** committed.

## Fuzzy ranking cost, and why it must be async

`fuzzy.Find` cost for one keystroke, by candidate count (same machine):

| Candidates | Query hits | Query misses |
| --- | --- | --- |
| 1,000 | 0.73 ms | 0.51 ms |
| 10,000 | 8.1 ms | 5.2 ms |
| 50,000 | 54 ms | 57 ms |
| 200,000 | 199 ms | 105 ms |

A buffer-lines picker over a large file, or a file picker in a large monorepo,
would visibly stall the UI if ranked on the event-loop goroutine. **Ranking runs
off the UI thread**, reusing the actor pattern already established by
`completion.Engine`: one owner goroutine, commands over a channel, debounce,
supersede-on-newer-query, generation-tagged results, and a bridge goroutine that
turns each result into a `screen.PostEvent`.

### Incremental narrowing

When the new query extends the previous one, only the previous round's survivors
are re-ranked, via `fuzzy.FindFrom` over a `Source` that projects a slice of
indexes. Measured over typing `renderbuf` against diverse generated repo paths:

| Candidates | Full rescan each keystroke | Narrowed |
| --- | --- | --- |
| 10,000 | 42.3 ms total | **17.9 ms total** |
| 200,000 | 852.9 ms total | **455.3 ms total** |

Roughly 2x overall, and the tail of a query becomes nearly free once the
survivor set is small (0.00 ms once it empties). The first keystroke is
unavoidably a full scan.

Two caveats measured directly from the library, both of which the engine must
handle explicitly:

- **An empty pattern returns zero matches**, not everything. The engine
  special-cases the empty query to emit all candidates in natural order.
- **Matching is always case-insensitive.** `fuzzy.Find("IHL", ...)` and
  `fuzzy.Find("ihl", ...)` return identical scores and offsets. Smart case
  therefore applies to `textsearch` only, never to the picker.
- Scores may be negative and are sorted descending. `MatchedIndexes` are offsets
  into the candidate string, suitable for highlighting.

### Why not the `fzf` binary

Worth recording, because "just shell out to fzf and rg" is the obvious
suggestion. Telescope itself uses `rg`/`fd` as *finders* but does its fuzzy
*sorting* in-process via a compiled native sorter; it does not spawn `fzf`.
`fzf.vim` is the one that shells out, and it does so by handing the entire
terminal over.

- **Interactive `fzf`** needs the tty. slopcode owns the terminal through tcell,
  so this means `Suspend()`/`Resume()` around the child. That costs a visible
  flash on every open, makes the picker untestable through
  `tcell.SimulationScreen`, prevents sharing the chroma theme, and puts the
  flakiest path (Windows conpty handoff) on a core interaction.
- **Non-interactive `fzf --filter=QUERY`** avoids the tty but costs a process
  spawn per keystroke (Windows process creation is 15-40 ms) and returns only
  matched strings on stdout — no character offsets, so no match highlighting.

`rg`/`fd` for *listing* is adopted, because `.gitignore` handling for free is a
real win and it is genuinely what Telescope does.

`rg` is deliberately **not** used for the in-buffer find/replace core: the buffer
is in memory and usually unsaved, so `rg` would search stale on-disk text.

## Architecture

New packages are UI-free, honoring the dependency rules in `AGENTS.md`.

```
internal/textsearch/  Literal substring find over []string lines, and the
                      match-stepping arithmetic. stdlib only, no UI.
internal/picker/      Candidate sources (rg/fd/WalkDir file listing, buffer
                      lines) + the async ranking Engine. Imports sahilm/fuzzy.
                      No UI, no buffer.
internal/buffer/      + ReplaceRange, SetCursor.
internal/render/      + findbar.go (prompt line, match highlighting)
                      + picker.go (centered overlay list)
internal/editor/      + find.go (prompt state machine and keys)
                      + picker.go (picker state and keys)
```

Dependency directions (must hold):

- `textsearch` imports stdlib only.
- `picker` imports `sahilm/fuzzy` and stdlib. It must **not** import `tcell`,
  `buffer`, or `render`. Buffer lines reach it as a plain `[]string` copy.
- `render` gains `textsearch` (for the `Match` value type it highlights) and
  `picker` (for the `Row` value type it lists), consistent with how it already
  imports `completion` purely for `Item`.
- `editor` wires everything, owns both prompt states, and owns the bridge
  goroutine.

`AGENTS.md`'s package table and the README's layout table and key tables are
updated as part of this work.

## internal/textsearch

```go
// Match is one occurrence: a byte span on a single line.
type Match struct {
    Row int // 0-based line index
    Col int // 0-based byte offset within the line
    Len int // byte length of the matched text
}

// FindAll returns every occurrence of query across lines, in document order.
// An empty query returns nil. Overlapping occurrences are all reported
// (searching "aa" in "aaa" yields offsets 0 and 1).
func FindAll(lines []string, query string) []Match

// NearestFrom returns the index of the first match at or after (row, col),
// wrapping to 0. Used to pick the initial selection when the bar opens, so a
// cursor already sitting on a match selects that match rather than skipping it.
// It returns -1 when ms is empty.
func NearestFrom(ms []Match, row, col int) int

// NextFrom returns the index of the first match strictly after (row, col),
// wrapping to 0. Used for Ctrl+N, which must always advance. Returns -1 when
// ms is empty.
func NextFrom(ms []Match, row, col int) int

// PrevFrom returns the index of the last match strictly before (row, col),
// wrapping to the end. Returns -1 when ms is empty.
func PrevFrom(ms []Match, row, col int) int
```

`NearestFrom` and `NextFrom` are deliberately separate: reusing one for both
jobs would either skip a match the cursor already sits on when opening the bar,
or make `Ctrl+N` stick in place.

### Smart case, and the byte-length trap

An all-lowercase query matches case-insensitively; any uppercase rune in the
query makes the whole query case-sensitive. This is ripgrep's and Telescope's
behavior and needs no toggle key or persistent option state.

The case-insensitive path folds with an **ASCII-only, byte-length-preserving**
fold, not `strings.ToLower`:

```go
// foldASCII lowercases A-Z and leaves every other byte untouched. It is
// byte-length preserving, so an offset found in the folded string is a valid
// offset into the original.
func foldASCII(s string) string
```

`strings.ToLower` is wrong here: Unicode lowering can change byte length
(U+0130 `İ` lowers to a 3-byte, 2-rune sequence), which would desynchronize
match offsets from the original line and corrupt every subsequent `Col`.
Consequence, documented as a known limitation: **case-insensitive matching is
ASCII-only**; non-ASCII letters always compare case-sensitively. This sits
alongside the existing UTF-16 position-encoding limitation in
`2026-07-17-autocomplete-lsp-design.md`.

## internal/buffer additions

```go
// ReplaceRange replaces the byte span [col, col+length) on row with text, and
// leaves the cursor immediately after the inserted text. Out-of-range spans
// are clamped.
func (b *Buffer) ReplaceRange(row, col, length int, text string)

// SetCursor moves the cursor to (row, col), clamped to the buffer's bounds.
func (b *Buffer) SetCursor(row, col int)
```

Neither takes a checkpoint; the caller decides undo granularity. This is what
makes single-undo replace-all possible.

## internal/picker

```go
// Candidate is one selectable row. Exactly one of the two destinations is
// meaningful, decided by the Source that produced it.
type Candidate struct {
    Text string // matched against the query, and shown in the list
    Path string // file to open (file source); "" for line candidates
    Row  int    // line to jump to (line source); ignored for file candidates
}

// Source produces a picker's candidate list, once per open.
type Source interface {
    Title() string                    // shown in the overlay header
    Candidates() ([]Candidate, error)
}

// Files lists files under root. root is resolved by GitRoot.
func Files(root string) Source

// Lister returns repo-relative file paths under root. It is the seam Files
// uses internally, exported so tests can supply canned output instead of
// depending on which binaries exist on the test machine.
type Lister func(root string) ([]string, error)

// FilesWith is Files with an injected Lister.
func FilesWith(root string, l Lister) Source

// Lines wraps a snapshot of the current buffer's lines.
func Lines(lines []string) Source

// GitRoot walks up from start looking for a .git entry and returns that
// directory. It falls back to start when there is no repository above it.
func GitRoot(start string) string

// Row is one ranked candidate plus the offsets that matched.
type Row struct {
    Cand    Candidate
    Matched []int // offsets into Cand.Text; nil when the query is empty
    Score   int
}

// Result is a completed ranking pass, tagged for staleness.
type Result struct {
    Gen   int    // generation this pass ran against
    Query string
    Rows  []Row  // truncated to MaxRows
    Total int    // total matches before truncation, for the counter
    Err   error  // non-nil disables the list for this pass; never fatal
}

// Engine ranks candidates on its own goroutine: debounced, cancellable,
// generation-tagged. Every method returns immediately.
func NewEngine() *Engine
func (e *Engine) Open(src Source)       // loads candidates, then ranks ""
func (e *Engine) Query(q string)        // debounced re-rank; supersedes pending
func (e *Engine) Results() <-chan Result
func (e *Engine) Close() error
```

`MaxRows` is a package constant (200) capping how many rows cross the channel
and reach the renderer — the overlay only ever shows a screenful. `Total` still
reports the true match count, so the `[n/m]` counter stays accurate.

### File listing, with fallbacks

`Files` resolves candidates by trying, in order:

1. `rg --files --hidden --glob !.git` — respects `.gitignore` for free.
2. `fd --type f --hidden --exclude .git`.
3. `filepath.WalkDir` with a built-in skip list: `.git`, `node_modules`,
   `vendor`, `dist`, `build`, `target`, `.venv`, `__pycache__`.

Each subprocess runs under an `exec.CommandContext` with a few seconds' timeout;
a missing binary, a non-zero exit, or a timeout falls through to the next
option rather than failing. This mirrors the existing best-effort posture for
`gopls`: absence of a tool is not an error. The walk is capped at 200,000
entries so a picker opened at `/` cannot hang.

Paths are reported relative to root, with forward slashes, so ranking and
display are stable across platforms.

The listing step is injected as a function value on the file source so tests can
exercise `rg` stdout parsing from canned output and the `WalkDir` fallback
directly, without depending on which binaries exist on the test machine.

## Rendering

### render.Frame

`render.Draw` already takes eight positional parameters, and this work needs two
more (the highlighted match list, and the number of rows reserved at the bottom
for the prompt). Rather than grow it to ten, the parameters move into a struct:

```go
// Frame is everything Draw needs for one repaint.
type Frame struct {
    Buf        *buffer.Buffer
    Filename   string
    Notice     string
    Modified   bool
    Scroll     int
    OriginX    int
    ShowCursor bool
    BottomRows int                // rows reserved at the bottom (find bar)
    Matches    []textsearch.Match // highlighted; nil when not searching
    Current    int                // index into Matches drawn as the active match
}

func Draw(s tcell.Screen, f Frame)
```

This is a contained refactor of code the feature already has to touch.

Match highlighting uses **attributes, not hardcoded colors**, honoring the
`AGENTS.md` theming invariant: the current match renders `Reverse(true)`, other
matches `Underline(true)`. Screen columns come from the existing
`render.ScreenCol`, so highlights land correctly on lines containing tabs.

### render.DrawFindBar

Draws the reserved bottom row: the query field, the replace field once revealed,
an `[n/m]` match counter, and a key hint. The focused field shows a cursor.

### render.DrawPicker

A centered overlay: header (the source title), a query line, and the ranked
rows with matched characters accented. Colors come from
`highlight.BackgroundStyle(StyleName)` and `highlight.Style(...)`, exactly as
`render/completion.go` does, so the picker re-themes with the single `StyleName`
constant.

## Editor wiring

New state on `Editor`:

```go
find   *findState   // non-nil while the find/replace bar is open
pick   *pickState   // non-nil while a picker overlay is open
pk     *picker.Engine
```

`findState` holds the two field values, which field has focus, the match list,
the current match index, and the cursor position to restore on cancel.

Key dispatch precedence in `handleKey`: picker, then find bar, then browser,
then completion popup, then normal editing. Opening a picker or the find bar
dismisses the completion popup; opening a picker closes the browser.

### Keys

`Ctrl+H` is **not** used for replace, and this is a portability trap worth
recording. tcell's Unix input parser maps the incoming byte before it ever
reaches the `KeyCtrl*` range:

```go
case '\t':          -> KeyTab        // Ctrl+I unreachable
case '\b', '\x7F':  -> KeyBackspace  // Ctrl+H unreachable
case '\r':          -> KeyEnter      // Ctrl+M unreachable
default: if r < ' ' -> KeyCtrlSpace + Key(r)   // Ctrl+A..Z arrive normally
```

On Windows the console path reports the virtual key plus a Ctrl modifier
separately, so `Ctrl+H` *does* arrive as `KeyCtrlH` there. A `Ctrl+H` binding
would therefore work on Windows and silently break on Linux and macOS — and the
simulation-screen tests inject `Key` values directly, so they would **not**
catch it. `Ctrl+H`, `Ctrl+I`, `Ctrl+J`, and `Ctrl+M` are all avoided.

Global:

| Key | Action |
| --- | --- |
| `Ctrl+F` | Open the find bar |
| `Ctrl+P` | Open the file picker |
| `Ctrl+G` | Open the buffer-lines picker |

Find bar open:

| Key | Action |
| --- | --- |
| Printable / Backspace | Edit the focused field; re-run `FindAll`; reselect the nearest match |
| `Tab` | Reveal and focus the replace field; again toggles focus back |
| `Ctrl+N` / `Ctrl+P` | Next / previous match (bar stays open) |
| `Ctrl+R` | Replace the current match and advance |
| `Ctrl+A` | Replace all matches |
| `Enter` | Accept: close the bar, leaving the cursor on the current match |
| `Esc` | Cancel: close the bar, restoring the cursor to where it opened |
| `Ctrl+S` | Save |

Picker open:

| Key | Action |
| --- | --- |
| Printable / Backspace | Edit the query (re-ranked asynchronously) |
| `Up` / `Ctrl+P` | Move the selection up |
| `Down` / `Ctrl+N` | Move the selection down |
| `Enter` | Accept: open the file, or jump to the line |
| `Esc` | Cancel |

`Ctrl+P` means "file picker" globally and "previous" inside a prompt. Context
disambiguates it, the same way the completion popup already reinterprets
`Up`/`Down`/`Enter`/`Tab`.

### Replace mechanics

- `Ctrl+R`: one `b.Checkpoint()`, one `b.ReplaceRange`, then re-run `FindAll`
  (the replacement may itself contain the query) and advance.
- `Ctrl+A`: one `b.Checkpoint()`, then apply every match in **reverse document
  order** so the offsets of not-yet-applied matches stay valid. The whole sweep
  is a single undo step.

Both bump `e.docVersion` and re-sync the completion engine, or gopls completes
against stale text.

### Shared file-open path

`browseEnter`'s file-open sequence — `dismissPopup`, `CloseDoc` on the old URI,
swap the buffer, `Open` the new document — is extracted into
`(*Editor).openPath(path string)` and reused by the picker. `AGENTS.md` records
that mishandling this sequence has already broken gopls symbol resolution once,
so the picker reuses it rather than re-implementing it.

### Scrolling to a match

Setting the cursor is sufficient: `draw()` already adjusts `scroll` to keep the
cursor visible. No centering logic (YAGNI).

### Unsaved and [No Name] buffers

The find bar works on any buffer, saved or not. The file picker needs a root, so
for a `[No Name]` buffer it falls back to the process working directory. The
lines picker always works.

## Error handling

Neither feature may crash or block the editor.

- No `rg` or `fd`: silently falls through to `WalkDir`. Not a notice; not an
  error.
- Listing fails outright (unreadable root): the `Result` carries `Err`, the
  overlay shows the message, and `Esc` closes it. The buffer is untouched.
- A query with no matches: the counter shows `[0/0]`, `Ctrl+N`/`Ctrl+P`/`Ctrl+R`
  /`Ctrl+A` are no-ops, and nothing is highlighted.
- Ranking a huge candidate set: results simply arrive late. Debounce plus
  generation-tagging means superseded passes are dropped, so the UI stays
  responsive and never renders a list for a query the user has moved past.

## Testing

**`textsearch`** (unit): smart case both ways; overlapping matches; empty query;
`NextFrom`/`PrevFrom` wraparound and the empty-list `-1`; and specifically that
a line containing non-ASCII text yields offsets that still slice the original
line correctly — the regression that `foldASCII` exists to prevent.
Plus `BenchmarkFindAll` to keep the stdlib decision measurable.

**`buffer`** (unit): `ReplaceRange` where the replacement is shorter, longer,
and equal length; spanning multi-byte runes; clamping out-of-range spans;
`SetCursor` clamping past end-of-line and past end-of-buffer.

**`picker`** (unit): `GitRoot` finds an ancestor `.git` and falls back when there
is none; `rg` stdout parsing from canned output; the `WalkDir` fallback finds
files and honors the skip list; paths come back relative with forward slashes.
Engine: a burst of `Query` calls collapses to one ranking pass; a newer query
supersedes an older one; a stale-generation result is dropped; an empty query
returns every candidate. And a **property test that narrowed ranking returns the
same rows as a full rescan** — the guard on the incremental-narrowing
optimization.

**`render`** (simulated screen): the find bar shows both fields and the counter;
matches highlight at the correct screen columns on a line containing tabs; the
picker overlay draws ranked rows with matched characters accented.

**`editor`** (simulated screen, driving the real event loop): `Ctrl+F`, type,
`Ctrl+N` cycles with wraparound, `Enter` leaves the cursor on the match; `Esc`
restores the original cursor; `Ctrl+R` replaces one occurrence and bumps
`docVersion`; **`Ctrl+A` replace-all is reverted by a single `Ctrl+Z`**; opening
a file through the picker sends `CloseDoc` for the old URI and `Open` for the
new one; and normal editing keys are unchanged when neither prompt is open.

## Suggested implementation phasing

1. `internal/textsearch`: `FindAll`, smart case, `foldASCII`, `NextFrom`/
   `PrevFrom`, unit tests, benchmark.
2. `internal/buffer`: `ReplaceRange`, `SetCursor`, unit tests.
3. `render.Frame` refactor (behavior-preserving; existing tests keep passing).
4. `render.DrawFindBar` + match highlighting + render tests.
5. `editor/find.go`: prompt state, keys, replace mechanics, integration tests.
6. `internal/picker`: sources, `GitRoot`, file listing with fallbacks, unit tests.
7. `internal/picker`: `Engine` with debounce/cancel/generation/narrowing, tests.
8. `render.DrawPicker` + render tests.
9. `editor/picker.go`: overlay state, keys, `openPath` extraction, integration
   tests.
10. `AGENTS.md` and `README.md` updates.

## Success criteria

- `Ctrl+F` finds and steps through matches incrementally, with every match
  highlighted and the active one distinguished, on files containing tabs.
- Replace-all across many matches is a single undo step.
- `Ctrl+P` opens files from anywhere in the repository, ranked usefully, with
  matched characters highlighted; `Ctrl+G` jumps to a line in the open file.
- The UI never blocks: ranking 200k candidates leaves typing responsive, and
  results for superseded queries are never rendered.
- The editor works with neither `rg` nor `fd` installed.
- `textsearch` and `picker` remain free of `tcell`, `buffer`, and `render`; the
  buffer is never touched off the UI thread.
- `go test ./...` passes without `rg`, `fd`, or `gopls` installed.
