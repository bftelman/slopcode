# Auto-pairs & File Browser Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Add smart bracket/quote auto-completion and a side-by-side Ctrl+B file browser.

**Architecture:** New pure packages `autopair` (operates on `*buffer.Buffer`) and `filebrowser` (`os.ReadDir`). `render` grows a sidebar + an `originX/showCursor`-aware `Draw`. `editor` gains a browse mode.

**Tech Stack:** Go 1.25.4, tcell/v2, chroma/v2 (existing). No new deps.

## Global Constraints

- `buffer`, `autopair`, `filebrowser` MUST NOT import tcell/chroma.
- `SidebarWidth = 30` (const in `render`).
- Auto-pairs: `()` `[]` `{}` `""` `''` `` `` ``.
- Unsaved edits are never discarded without a second explicit Enter (or a save).

## File Structure

- `internal/buffer/buffer.go` — add `RuneAt`.
- `internal/autopair/autopair.go` (+ test) — new.
- `internal/filebrowser/filebrowser.go` (+ test) — new.
- `internal/render/render.go` — `Draw` signature change, `DrawSidebar`, `SidebarWidth`.
- `internal/render/render_test.go` — update calls, add sidebar test.
- `internal/editor/editor.go` (+ test) — browse mode, open, guard.

---

### Task 1: `buffer.RuneAt`

**Files:** Modify `internal/buffer/buffer.go`; Test `internal/buffer/buffer_test.go`.

**Interfaces:** `func (b *Buffer) RuneAt(offset int) (rune, bool)` — rune at `col+offset` in the current line; false if out of range.

- [ ] **Step 1: Failing tests**

```go
func TestRuneAt(t *testing.T) {
	b := New([]string{"abc"})
	b.MoveRight() // col 1
	if r, ok := b.RuneAt(0); !ok || r != 'b' {
		t.Fatalf("at cursor want b got %q ok=%v", r, ok)
	}
	if r, ok := b.RuneAt(-1); !ok || r != 'a' {
		t.Fatalf("before cursor want a got %q ok=%v", r, ok)
	}
	if _, ok := b.RuneAt(-2); ok {
		t.Fatal("expected out-of-range before start")
	}
	b.MoveEnd() // col 3
	if _, ok := b.RuneAt(0); ok {
		t.Fatal("expected out-of-range at end")
	}
}
```

- [ ] **Step 2: Run — expect FAIL** (`go test ./internal/buffer/`, undefined RuneAt).

- [ ] **Step 3: Implement** (append to buffer.go)

```go
// RuneAt returns the rune at col+offset in the current line, or false if out of range.
func (b *Buffer) RuneAt(offset int) (rune, bool) {
	line := b.lines[b.row]
	i := b.col + offset
	if i < 0 || i >= len(line) {
		return 0, false
	}
	return rune(line[i]), true
}
```

- [ ] **Step 4: Run — expect PASS** (`go test ./internal/buffer/`).

- [ ] **Step 5: Commit** — `git add internal/buffer/ && git commit -m "feat(buffer): RuneAt accessor"`

---

### Task 2: `autopair` package

**Files:** Create `internal/autopair/autopair.go`, `internal/autopair/autopair_test.go`.

**Interfaces:**
- `func InsertRune(b *buffer.Buffer, r rune)`
- `func Backspace(b *buffer.Buffer)`

- [ ] **Step 1: Failing tests**

```go
package autopair

import (
	"testing"

	"github.com/bftelman/slopcode/internal/buffer"
)

func lineCol(b *buffer.Buffer) (string, int) {
	_, c := b.Cursor()
	return b.Lines()[0], c
}

func TestInsertOpenerAutoCloses(t *testing.T) {
	b := buffer.New([]string{""})
	InsertRune(b, '(')
	if l, c := lineCol(b); l != "()" || c != 1 {
		t.Fatalf("want () col1, got %q col%d", l, c)
	}
}

func TestInsertClosingSkipsOver(t *testing.T) {
	b := buffer.New([]string{""})
	InsertRune(b, '(') // "(|)"
	InsertRune(b, ')') // should step over -> "()|"
	if l, c := lineCol(b); l != "()" || c != 2 {
		t.Fatalf("want () col2, got %q col%d", l, c)
	}
}

func TestInsertQuotePair(t *testing.T) {
	b := buffer.New([]string{""})
	InsertRune(b, '"')
	if l, c := lineCol(b); l != "\"\"" || c != 1 {
		t.Fatalf("want two quotes col1, got %q col%d", l, c)
	}
	InsertRune(b, '"') // step over
	if l, c := lineCol(b); l != "\"\"" || c != 2 {
		t.Fatalf("want two quotes col2, got %q col%d", l, c)
	}
}

func TestBackspaceDeletesEmptyPair(t *testing.T) {
	b := buffer.New([]string{""})
	InsertRune(b, '{') // "{|}"
	Backspace(b)       // delete both -> ""
	if l, c := lineCol(b); l != "" || c != 0 {
		t.Fatalf("want empty col0, got %q col%d", l, c)
	}
}

func TestBackspacePlain(t *testing.T) {
	b := buffer.New([]string{"ab"})
	b.MoveEnd()
	Backspace(b)
	if l := b.Lines()[0]; l != "a" {
		t.Fatalf("want a, got %q", l)
	}
}

func TestInsertPlainRune(t *testing.T) {
	b := buffer.New([]string{""})
	InsertRune(b, 'x')
	if l, c := lineCol(b); l != "x" || c != 1 {
		t.Fatalf("want x col1, got %q col%d", l, c)
	}
}
```

- [ ] **Step 2: Run — expect FAIL** (`go test ./internal/autopair/`).

- [ ] **Step 3: Implement**

```go
// Package autopair adds bracket/quote auto-completion on top of a buffer.
package autopair

import "github.com/bftelman/slopcode/internal/buffer"

var openers = map[rune]rune{
	'(': ')', '[': ']', '{': '}',
	'"': '"', '\'': '\'', '`': '`',
}

var closers = map[rune]bool{
	')': true, ']': true, '}': true,
	'"': true, '\'': true, '`': true,
}

// InsertRune inserts r with auto-close / skip-over behavior.
func InsertRune(b *buffer.Buffer, r rune) {
	if closers[r] {
		if next, ok := b.RuneAt(0); ok && next == r {
			b.MoveRight() // step over existing closer
			return
		}
	}
	if close, ok := openers[r]; ok {
		b.InsertRune(r)
		b.InsertRune(close)
		b.MoveLeft()
		return
	}
	b.InsertRune(r)
}

// Backspace deletes an empty pair as a unit, else deletes one rune.
func Backspace(b *buffer.Buffer) {
	prev, hasPrev := b.RuneAt(-1)
	next, hasNext := b.RuneAt(0)
	if hasPrev && hasNext {
		if close, ok := openers[prev]; ok && close == next {
			b.MoveRight()
			b.Backspace()
			b.Backspace()
			return
		}
	}
	b.Backspace()
}
```

Note: `closers[r]` is checked before `openers[r]`, so a quote steps over an adjacent
matching quote and otherwise opens a new pair.

- [ ] **Step 4: Run — expect PASS**.

- [ ] **Step 5: Commit** — `git add internal/autopair/ && git commit -m "feat(autopair): smart bracket/quote completion"`

---

### Task 3: `filebrowser` package

**Files:** Create `internal/filebrowser/filebrowser.go`, `internal/filebrowser/filebrowser_test.go`.

**Interfaces:**
- `type Entry struct { Name string; IsDir bool }`
- `type Browser struct { ... }`
- `func Open(dir string) (*Browser, error)`
- `func (b *Browser) Entries() []Entry`, `Dir() string`, `SelIndex() int`, `Selected() Entry`
- `func (b *Browser) MoveUp()`, `MoveDown()`
- `func (b *Browser) Enter() (path string, isDir bool, err error)`

- [ ] **Step 1: Failing tests**

```go
package filebrowser

import (
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"b.txt", "a.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestOpenOrdersEntries(t *testing.T) {
	dir := setup(t)
	br, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := br.Entries()
	// Expect: .., sub/(dir), a.txt, b.txt
	if len(got) != 4 {
		t.Fatalf("want 4 entries, got %d: %#v", len(got), got)
	}
	if got[0].Name != ".." || !got[0].IsDir {
		t.Fatalf("first entry should be .. dir, got %#v", got[0])
	}
	if got[1].Name != "sub" || !got[1].IsDir {
		t.Fatalf("second should be sub dir, got %#v", got[1])
	}
	if got[2].Name != "a.txt" || got[3].Name != "b.txt" {
		t.Fatalf("files out of order: %#v", got[2:])
	}
}

func TestMoveClamps(t *testing.T) {
	br, _ := Open(setup(t))
	br.MoveUp() // already at 0
	if br.SelIndex() != 0 {
		t.Fatalf("want 0 got %d", br.SelIndex())
	}
	for i := 0; i < 10; i++ {
		br.MoveDown()
	}
	if br.SelIndex() != len(br.Entries())-1 {
		t.Fatalf("want last got %d", br.SelIndex())
	}
}

func TestEnterDirAndFile(t *testing.T) {
	dir := setup(t)
	br, _ := Open(dir)
	br.MoveDown() // select "sub"
	path, isDir, err := br.Enter()
	if err != nil || !isDir {
		t.Fatalf("want dir nav, isDir=%v err=%v", isDir, err)
	}
	if filepath.Base(br.Dir()) != "sub" {
		t.Fatalf("want dir sub, got %q", br.Dir())
	}
	// Go back up.
	br.MoveUp() // ".." is index 0 already; ensure selected is ".."
	for br.Selected().Name != ".." {
		br.MoveUp()
	}
	_, isDir, _ = br.Enter()
	if !isDir || filepath.Clean(br.Dir()) != filepath.Clean(dir) {
		t.Fatalf("want back to %q, got %q", dir, br.Dir())
	}
	// Select a file and open it.
	for br.Selected().Name != "a.txt" {
		br.MoveDown()
	}
	path, isDir, err = br.Enter()
	if err != nil || isDir {
		t.Fatalf("want file, isDir=%v err=%v", isDir, err)
	}
	if filepath.Base(path) != "a.txt" {
		t.Fatalf("want a.txt path, got %q", path)
	}
}
```

- [ ] **Step 2: Run — expect FAIL**.

- [ ] **Step 3: Implement**

```go
// Package filebrowser lists a directory for navigation. No UI dependencies.
package filebrowser

import (
	"os"
	"path/filepath"
	"sort"
)

// Entry is one item in the listing.
type Entry struct {
	Name  string
	IsDir bool
}

// Browser holds a directory listing and the current selection.
type Browser struct {
	dir     string
	entries []Entry
	sel     int
}

// Open reads dir and returns a Browser positioned at the first entry.
func Open(dir string) (*Browser, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	b := &Browser{dir: filepath.Clean(abs)}
	if err := b.load(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Browser) load() error {
	items, err := os.ReadDir(b.dir)
	if err != nil {
		return err
	}
	var dirs, files []Entry
	for _, it := range items {
		if it.IsDir() {
			dirs = append(dirs, Entry{Name: it.Name(), IsDir: true})
		} else {
			files = append(files, Entry{Name: it.Name(), IsDir: false})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	entries := []Entry{{Name: "..", IsDir: true}}
	entries = append(entries, dirs...)
	entries = append(entries, files...)
	b.entries = entries
	b.sel = 0
	return nil
}

func (b *Browser) Entries() []Entry { return b.entries }
func (b *Browser) Dir() string      { return b.dir }
func (b *Browser) SelIndex() int    { return b.sel }
func (b *Browser) Selected() Entry  { return b.entries[b.sel] }

// MoveUp moves the selection up, clamped.
func (b *Browser) MoveUp() {
	if b.sel > 0 {
		b.sel--
	}
}

// MoveDown moves the selection down, clamped.
func (b *Browser) MoveDown() {
	if b.sel < len(b.entries)-1 {
		b.sel++
	}
}

// Enter descends into the selected directory (returns isDir=true) or returns a
// file's full path (isDir=false).
func (b *Browser) Enter() (string, bool, error) {
	e := b.entries[b.sel]
	if e.IsDir {
		b.dir = filepath.Clean(filepath.Join(b.dir, e.Name))
		return "", true, b.load()
	}
	return filepath.Join(b.dir, e.Name), false, nil
}
```

- [ ] **Step 4: Run — expect PASS**.

- [ ] **Step 5: Commit** — `git add internal/filebrowser/ && git commit -m "feat(filebrowser): navigable directory listing"`

---

### Task 4: `render` — Draw region + sidebar

**Files:** Modify `internal/render/render.go`, `internal/render/render_test.go`.

**Interfaces:**
- `const SidebarWidth = 30`
- `func Draw(s tcell.Screen, b *buffer.Buffer, filename, notice string, modified bool, scroll, originX int, showCursor bool)`
- `func DrawSidebar(s tcell.Screen, br *filebrowser.Browser)`

- [ ] **Step 1: Update existing render tests for the new Draw signature**

In `render_test.go`, change the three `Draw(s, b, ..., false, 0)` calls to
`Draw(s, b, ..., false, 0, 0, true)`, and update the highlight/tab tests' cell scan
(gutter origin is still 0 since `originX=0`).

- [ ] **Step 2: Add a sidebar test**

```go
func TestDrawSidebarShowsEntries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	br, err := filebrowser.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := newSimScreen(t, 80, 24)
	defer s.Fini()
	DrawSidebar(s, br)

	cells, width, height := s.GetContents()
	found := false
	for y := 0; y < height; y++ {
		var row []rune
		for x := 0; x < SidebarWidth-1; x++ {
			r := cellAt(cells, width, x, y).Runes
			if len(r) == 1 {
				row = append(row, r[0])
			} else {
				row = append(row, ' ')
			}
		}
		if contains(string(row), "hello.txt") {
			found = true
		}
	}
	if !found {
		t.Fatal("sidebar should list hello.txt")
	}
}
```

Add imports `os`, `path/filepath`, and `github.com/bftelman/slopcode/internal/filebrowser`
to `render_test.go`.

- [ ] **Step 3: Run — expect FAIL** (undefined DrawSidebar / signature mismatch).

- [ ] **Step 4: Implement in render.go**

Add the const and change `Draw`'s signature/body to honor `originX` and `showCursor`.
Replace the `Draw` function header and the statusbar/gutter/text/cursor offsets so every
screen x is shifted by `originX` (statusbar spans `[originX, width)`; gutter at `originX`;
text at `originX+gw`; cursor `originX+gw+screenCol`; hide cursor when `!showCursor`). Then
add `DrawSidebar`:

```go
// SidebarWidth is the column width of the file browser panel.
const SidebarWidth = 30

// DrawSidebar renders the file browser into columns [0, SidebarWidth).
func DrawSidebar(s tcell.Screen, br *filebrowser.Browser) {
	width, height := s.Size()
	_ = width
	sep := SidebarWidth - 1
	normal := tcell.StyleDefault
	dirStyle := tcell.StyleDefault.Foreground(tcell.ColorAqua)
	selStyle := tcell.StyleDefault.Reverse(true)
	header := tcell.StyleDefault.Reverse(true)

	// Clear panel + draw separator.
	for y := 0; y < height; y++ {
		for x := 0; x < sep; x++ {
			s.SetContent(x, y, ' ', nil, normal)
		}
		s.SetContent(sep, y, '│', nil, normal)
	}

	// Header (row 0): directory basename.
	title := " " + filepath.Base(br.Dir())
	drawText(s, 0, 0, clipPad(title, sep), header)

	entries := br.Entries()
	sel := br.SelIndex()
	rows := height - 1
	start := 0
	if rows > 0 && sel >= rows {
		start = sel - rows + 1
	}
	for i := 0; i < rows; i++ {
		idx := start + i
		if idx >= len(entries) {
			break
		}
		y := i + 1
		e := entries[idx]
		label := e.Name
		if e.IsDir {
			label += "/"
		}
		st := normal
		if e.IsDir {
			st = dirStyle
		}
		if idx == sel {
			st = selStyle
		}
		drawText(s, 0, y, clipPad(" "+label, sep), st)
	}
	s.Show()
}

// clipPad truncates or right-pads text to exactly width columns.
func clipPad(text string, width int) string {
	if width < 0 {
		return ""
	}
	if len(text) > width {
		return text[:width]
	}
	for len(text) < width {
		text += " "
	}
	return text
}
```

Also add `"path/filepath"` and the `filebrowser` import to render.go.

- [ ] **Step 5: Run — expect PASS** (`go test ./internal/render/`). Build may still fail in
  `editor` (old Draw call) — fixed in Task 5.

- [ ] **Step 6: Commit** — `git add internal/render/ && git commit -m "feat(render): region-aware Draw and file browser sidebar"`

---

### Task 5: `editor` — browse mode, open, unsaved guard

**Files:** Modify `internal/editor/editor.go`, `internal/editor/editor_test.go`.

**Interfaces:** `Editor` gains `browser *filebrowser.Browser` and `pendingOpen string`.

- [ ] **Step 1: Failing tests** (append to editor_test.go)

```go
func TestBrowserOpensFileOnEnter(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(target, []byte("loaded\n"), 0644); err != nil {
		t.Fatal(err)
	}
	start := filepath.Join(dir, "start.txt")
	if err := os.WriteFile(start, []byte("start\n"), 0644); err != nil {
		t.Fatal(err)
	}

	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(80, 24)

	lines, _ := fileio.Load(start)
	e := New(s, buffer.New(lines), start)

	e.handleKey(keyEvent(tcell.KeyCtrlB)) // open browser
	if e.browser == nil {
		t.Fatal("browser should be open")
	}
	// Move to "other.txt" and open it.
	for e.browser.Selected().Name != "other.txt" {
		e.handleKey(keyEvent(tcell.KeyDown))
	}
	e.handleKey(keyEvent(tcell.KeyEnter))
	if e.browser != nil {
		t.Fatal("browser should close after opening a file")
	}
	if e.path != target {
		t.Fatalf("path = %q, want %q", e.path, target)
	}
	if e.b.Lines()[0] != "loaded" {
		t.Fatalf("buffer not loaded: %#v", e.b.Lines())
	}
}

func TestBrowserUnsavedGuard(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(target, []byte("loaded\n"), 0644); err != nil {
		t.Fatal(err)
	}
	start := filepath.Join(dir, "start.txt")
	if err := os.WriteFile(start, []byte("start\n"), 0644); err != nil {
		t.Fatal(err)
	}

	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(80, 24)

	lines, _ := fileio.Load(start)
	e := New(s, buffer.New(lines), start)
	e.handleKey(keyEvent(tcell.KeyRune, 'z')) // modify
	e.handleKey(keyEvent(tcell.KeyCtrlB))     // open browser
	for e.browser.Selected().Name != "other.txt" {
		e.handleKey(keyEvent(tcell.KeyDown))
	}
	e.handleKey(keyEvent(tcell.KeyEnter)) // first Enter: guard
	if e.browser == nil {
		t.Fatal("should NOT switch on first Enter with unsaved changes")
	}
	if e.notice == "" {
		t.Fatal("expected unsaved-changes notice")
	}
	e.handleKey(keyEvent(tcell.KeyEnter)) // second Enter: switch
	if e.browser != nil {
		t.Fatal("should switch on second Enter")
	}
	if e.path != target {
		t.Fatalf("path = %q, want %q", e.path, target)
	}
}
```

Update the existing `keyEvent` helper to accept an optional rune:

```go
func keyEvent(k tcell.Key, ch ...rune) *tcell.EventKey {
	r := rune(0)
	if len(ch) > 0 {
		r = ch[0]
	}
	return tcell.NewEventKey(k, r, tcell.ModNone)
}
```

(Existing callers `keyEvent(tcell.KeyCtrlS)` / `keyEvent(tcell.KeyRight)` still compile.)

- [ ] **Step 2: Run — expect FAIL**.

- [ ] **Step 3: Implement editor.go**

Add imports `path/filepath`, `github.com/bftelman/slopcode/internal/autopair`,
`github.com/bftelman/slopcode/internal/filebrowser`. Add fields:

```go
	browser     *filebrowser.Browser
	pendingOpen string
```

Split `handleKey` by mode at the top:

```go
func (e *Editor) handleKey(ev *tcell.EventKey) bool {
	if e.browser != nil {
		return e.handleBrowseKey(ev)
	}
	e.notice = "" // any key clears the transient notice
	switch ev.Key() {
	case tcell.KeyCtrlQ:
		return true
	case tcell.KeyCtrlB:
		e.openBrowser()
	case tcell.KeyCtrlS:
		e.save()
	case tcell.KeyEnter:
		e.b.InsertNewline()
		e.modified = true
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		autopair.Backspace(e.b)
		e.modified = true
	case tcell.KeyLeft:
		e.b.MoveLeft()
	case tcell.KeyRight:
		e.b.MoveRight()
	case tcell.KeyUp:
		e.b.MoveUp()
	case tcell.KeyDown:
		e.b.MoveDown()
	case tcell.KeyHome:
		e.b.MoveHome()
	case tcell.KeyEnd:
		e.b.MoveEnd()
	case tcell.KeyTab:
		e.b.InsertTab(render.TabWidth)
		e.modified = true
	case tcell.KeyRune:
		autopair.InsertRune(e.b, ev.Rune())
		e.modified = true
	}
	return false
}

func (e *Editor) save() {
	if err := fileio.Save(e.path, e.b.Lines()); err != nil {
		e.notice = "SAVE ERROR: " + err.Error()
		return
	}
	e.modified = false
	e.notice = e.path + " saved"
}

func (e *Editor) openBrowser() {
	dir := filepath.Dir(e.path)
	if dir == "" {
		dir = "."
	}
	br, err := filebrowser.Open(dir)
	if err != nil {
		e.notice = "BROWSE ERROR: " + err.Error()
		return
	}
	e.browser = br
	e.pendingOpen = ""
}

func (e *Editor) handleBrowseKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyCtrlQ:
		return true
	case tcell.KeyCtrlB, tcell.KeyEscape:
		e.browser = nil
		e.pendingOpen = ""
	case tcell.KeyCtrlS:
		e.save()
	case tcell.KeyUp:
		e.browser.MoveUp()
		e.pendingOpen = ""
	case tcell.KeyDown:
		e.browser.MoveDown()
		e.pendingOpen = ""
	case tcell.KeyEnter:
		e.browseEnter()
	}
	return false
}

func (e *Editor) browseEnter() {
	path, isDir, err := e.browser.Enter()
	if err != nil {
		e.notice = "BROWSE ERROR: " + err.Error()
		return
	}
	if isDir {
		e.pendingOpen = ""
		return
	}
	if e.modified && e.pendingOpen != path {
		e.pendingOpen = path
		e.notice = "unsaved changes — Ctrl+S, or Enter again to discard"
		return
	}
	lines, err := fileio.Load(path)
	if err != nil {
		e.notice = "OPEN ERROR: " + err.Error()
		return
	}
	e.b = buffer.New(lines)
	e.path = path
	e.scroll = 0
	e.modified = false
	e.browser = nil
	e.pendingOpen = ""
	e.notice = path + " opened"
}
```

Update `draw()`:

```go
func (e *Editor) draw() {
	_, height := e.s.Size()
	textRows := height - 1
	if textRows < 1 {
		textRows = 1
	}
	row, _ := e.b.Cursor()
	if row < e.scroll {
		e.scroll = row
	} else if row >= e.scroll+textRows {
		e.scroll = row - textRows + 1
	}
	if e.browser != nil {
		render.Draw(e.s, e.b, e.path, e.notice, e.modified, e.scroll, render.SidebarWidth, false)
		render.DrawSidebar(e.s, e.browser)
	} else {
		render.Draw(e.s, e.b, e.path, e.notice, e.modified, e.scroll, 0, true)
	}
}
```

- [ ] **Step 4: Run — expect PASS** (`go build ./... && go test ./...`).

- [ ] **Step 5: Commit** — `git add internal/editor/ && git commit -m "feat(editor): Ctrl+B file browser mode with unsaved guard; autopair wiring"`

---

### Task 6: Full verification

- [ ] **Step 1:** `go build ./... && go test ./... && go vet ./...` — all clean.
- [ ] **Step 2: Manual smoke** — `go run . main.go`: type `(` `[` `{` `"` and confirm pairs
  auto-close with the cursor between; type the closer to step over; backspace an empty pair
  removes both; Ctrl+B shows the sidebar; Up/Down move the highlight; Enter on a folder
  descends, on a file opens it; editing then Enter on a file warns once, opens on the second.
- [ ] **Step 3:** Commit any leftover changes.

---

## Self-Review

- **Coverage:** auto-pairs (Tasks 1,2,5) ✓; skip-over & delete-pair (Task 2) ✓; navigable
  browser (Task 3) ✓; Ctrl+B toggle + Up/Down + Enter open (Task 5) ✓; side-by-side render
  (Task 4) ✓; unsaved guard (Task 5) ✓; starting dir = file's dir (Task 5 `openBrowser`) ✓.
- **Placeholders:** none.
- **Type consistency:** `Draw(...scroll, originX, showCursor)` defined Task 4, called Task 5 ✓;
  `DrawSidebar(s, br)` Task 4 / Task 5 ✓; `RuneAt` Task 1 / Task 2 ✓;
  `Enter() (string,bool,error)` Task 3 / Task 5 ✓; `keyEvent` variadic rune Task 5 ✓.
