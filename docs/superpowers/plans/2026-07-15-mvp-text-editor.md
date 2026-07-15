# MVP Text Editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a fullscreen, non-modal, vim-inspired terminal text editor in Go that opens/creates a file, edits text with line numbers and a top statusbar, and saves.

**Architecture:** Pure-logic `buffer` and `fileio` packages (fully unit-tested), a `render` package that draws to a tcell screen, and an `editor` package that runs the event loop. `main` wires them together. tcell handles cross-platform (incl. Windows) terminal I/O.

**Tech Stack:** Go 1.25.4, `github.com/gdamore/tcell/v2`.

## Global Constraints

- Module path: `github.com/bftelman/slopcode`.
- Go version: 1.25.4.
- Not modal: typing inserts directly. Ctrl+S saves, Ctrl+Q quits.
- Filename argument is required.
- Missing file on load → empty buffer (one empty line), not an error.
- `buffer` and `fileio` packages MUST NOT import tcell (keeps them unit-testable).
- `buffer.lines` always has ≥ 1 element; cursor always in bounds.
- Long lines clip horizontally; vertical scrolling only.

## File Structure

- `internal/buffer/buffer.go` — text model + cursor ops (no tcell).
- `internal/buffer/buffer_test.go` — unit tests.
- `internal/fileio/fileio.go` — load/save (no tcell).
- `internal/fileio/fileio_test.go` — unit tests.
- `internal/render/render.go` — draw statusbar + gutter + text to a tcell screen.
- `internal/editor/editor.go` — event loop, scroll, key dispatch.
- `main.go` — arg parsing + wiring.

> **Note:** This is a brand-new project with no git repo. Task 1 initializes git so the per-task commit steps work.

---

### Task 1: Project setup + git + tcell dependency

**Files:**
- Modify: `go.mod`
- Create: `.gitignore`

**Interfaces:**
- Produces: a buildable module with `tcell/v2` available for import.

- [ ] **Step 1: Initialize git**

```bash
cd /c/Users/telman.babayev/source/slopcode
git init
```

- [ ] **Step 2: Create `.gitignore`**

```
/slopcode
/slopcode.exe
```

- [ ] **Step 3: Add tcell dependency**

```bash
go get github.com/gdamore/tcell/v2@latest
go mod tidy
```

- [ ] **Step 4: Verify it builds**

Run: `go build ./...`
Expected: exit 0, no output.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore: init project with tcell dependency"
```

---

### Task 2: `buffer` package — construction and accessors

**Files:**
- Create: `internal/buffer/buffer.go`
- Test: `internal/buffer/buffer_test.go`

**Interfaces:**
- Produces:
  - `type Buffer struct { ... }`
  - `func New(lines []string) *Buffer` — if `lines` is empty, initializes to `[]string{""}`. Cursor at row 0, col 0.
  - `func (b *Buffer) Lines() []string`
  - `func (b *Buffer) Cursor() (row, col int)`
  - `func (b *Buffer) LineCount() int`

- [ ] **Step 1: Write the failing tests**

```go
package buffer

import "testing"

func TestNewEmptyGivesOneBlankLine(t *testing.T) {
	b := New(nil)
	if got := b.Lines(); len(got) != 1 || got[0] != "" {
		t.Fatalf("want one blank line, got %#v", got)
	}
	if r, c := b.Cursor(); r != 0 || c != 0 {
		t.Fatalf("want cursor 0,0 got %d,%d", r, c)
	}
	if b.LineCount() != 1 {
		t.Fatalf("want LineCount 1, got %d", b.LineCount())
	}
}

func TestNewWithLines(t *testing.T) {
	b := New([]string{"abc", "de"})
	if b.LineCount() != 2 {
		t.Fatalf("want 2 lines, got %d", b.LineCount())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/buffer/`
Expected: FAIL (undefined: New).

- [ ] **Step 3: Write minimal implementation**

```go
package buffer

// Buffer is a pure text model with a cursor. It does not depend on any UI.
type Buffer struct {
	lines []string
	row   int
	col   int
}

// New creates a Buffer from lines. Empty input becomes a single blank line.
func New(lines []string) *Buffer {
	if len(lines) == 0 {
		lines = []string{""}
	}
	return &Buffer{lines: lines}
}

func (b *Buffer) Lines() []string { return b.lines }

func (b *Buffer) Cursor() (row, col int) { return b.row, b.col }

func (b *Buffer) LineCount() int { return len(b.lines) }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/buffer/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/buffer/
git commit -m "feat(buffer): construction and accessors"
```

---

### Task 3: `buffer` — cursor movement (clamped)

**Files:**
- Modify: `internal/buffer/buffer.go`
- Test: `internal/buffer/buffer_test.go`

**Interfaces:**
- Produces:
  - `func (b *Buffer) MoveLeft()`
  - `func (b *Buffer) MoveRight()`
  - `func (b *Buffer) MoveUp()`
  - `func (b *Buffer) MoveDown()`
  - `func (b *Buffer) MoveHome()`
  - `func (b *Buffer) MoveEnd()`
- Behavior: `col` is clamped to `[0, len(currentLine)]` (col may equal line length = position after last char). `MoveLeft` at col 0 goes to end of previous line; `MoveRight` at end of line goes to col 0 of next line. Up/Down clamp col to the destination line length. Movement never leaves bounds.

- [ ] **Step 1: Write the failing tests**

```go
func TestMoveRightAndLeftWithinLine(t *testing.T) {
	b := New([]string{"abc"})
	b.MoveRight()
	if r, c := b.Cursor(); r != 0 || c != 1 {
		t.Fatalf("want 0,1 got %d,%d", r, c)
	}
	b.MoveLeft()
	if r, c := b.Cursor(); r != 0 || c != 0 {
		t.Fatalf("want 0,0 got %d,%d", r, c)
	}
}

func TestMoveRightWrapsToNextLine(t *testing.T) {
	b := New([]string{"ab", "cd"})
	b.MoveEnd() // col 2 (after 'b')
	b.MoveRight()
	if r, c := b.Cursor(); r != 1 || c != 0 {
		t.Fatalf("want 1,0 got %d,%d", r, c)
	}
}

func TestMoveLeftWrapsToPrevLineEnd(t *testing.T) {
	b := New([]string{"ab", "cd"})
	b.MoveDown() // row 1, col clamped to 0? col was 0 -> stays 0
	b.MoveLeft()
	if r, c := b.Cursor(); r != 0 || c != 2 {
		t.Fatalf("want 0,2 got %d,%d", r, c)
	}
}

func TestMoveDownClampsColumn(t *testing.T) {
	b := New([]string{"abcd", "e"})
	b.MoveEnd() // col 4
	b.MoveDown()
	if r, c := b.Cursor(); r != 1 || c != 1 {
		t.Fatalf("want 1,1 got %d,%d", r, c)
	}
}

func TestMoveBoundsAreSafe(t *testing.T) {
	b := New([]string{"a"})
	b.MoveLeft()  // at 0,0 already
	b.MoveUp()
	if r, c := b.Cursor(); r != 0 || c != 0 {
		t.Fatalf("want 0,0 got %d,%d", r, c)
	}
	b.MoveEnd()
	b.MoveRight() // no next line
	b.MoveDown()  // no next line
	if r, c := b.Cursor(); r != 0 || c != 1 {
		t.Fatalf("want 0,1 got %d,%d", r, c)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/buffer/`
Expected: FAIL (undefined methods).

- [ ] **Step 3: Write minimal implementation**

Append to `buffer.go`:

```go
func (b *Buffer) curLen() int { return len(b.lines[b.row]) }

func (b *Buffer) clampCol() {
	if b.col > b.curLen() {
		b.col = b.curLen()
	}
	if b.col < 0 {
		b.col = 0
	}
}

func (b *Buffer) MoveLeft() {
	if b.col > 0 {
		b.col--
		return
	}
	if b.row > 0 {
		b.row--
		b.col = b.curLen()
	}
}

func (b *Buffer) MoveRight() {
	if b.col < b.curLen() {
		b.col++
		return
	}
	if b.row < len(b.lines)-1 {
		b.row++
		b.col = 0
	}
}

func (b *Buffer) MoveUp() {
	if b.row > 0 {
		b.row--
		b.clampCol()
	}
}

func (b *Buffer) MoveDown() {
	if b.row < len(b.lines)-1 {
		b.row++
		b.clampCol()
	}
}

func (b *Buffer) MoveHome() { b.col = 0 }

func (b *Buffer) MoveEnd() { b.col = b.curLen() }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/buffer/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/buffer/
git commit -m "feat(buffer): clamped cursor movement"
```

---

### Task 4: `buffer` — editing (insert rune, newline, backspace)

**Files:**
- Modify: `internal/buffer/buffer.go`
- Test: `internal/buffer/buffer_test.go`

**Interfaces:**
- Produces:
  - `func (b *Buffer) InsertRune(r rune)` — insert at cursor, advance col.
  - `func (b *Buffer) InsertNewline()` — split current line at col; move to row+1, col 0.
  - `func (b *Buffer) Backspace()` — delete char before cursor; at col 0 with row>0, join current line onto previous (cursor to join point); at 0,0 no-op.

- [ ] **Step 1: Write the failing tests**

```go
func TestInsertRune(t *testing.T) {
	b := New([]string{"ac"})
	b.MoveRight() // col 1
	b.InsertRune('b')
	if b.Lines()[0] != "abc" {
		t.Fatalf("want abc got %q", b.Lines()[0])
	}
	if _, c := b.Cursor(); c != 2 {
		t.Fatalf("want col 2 got %d", c)
	}
}

func TestInsertNewlineSplits(t *testing.T) {
	b := New([]string{"abcd"})
	b.MoveRight()
	b.MoveRight() // col 2
	b.InsertNewline()
	got := b.Lines()
	if len(got) != 2 || got[0] != "ab" || got[1] != "cd" {
		t.Fatalf("bad split: %#v", got)
	}
	if r, c := b.Cursor(); r != 1 || c != 0 {
		t.Fatalf("want 1,0 got %d,%d", r, c)
	}
}

func TestBackspaceWithinLine(t *testing.T) {
	b := New([]string{"abc"})
	b.MoveEnd() // col 3
	b.Backspace()
	if b.Lines()[0] != "ab" {
		t.Fatalf("want ab got %q", b.Lines()[0])
	}
	if _, c := b.Cursor(); c != 2 {
		t.Fatalf("want col 2 got %d", c)
	}
}

func TestBackspaceJoinsLines(t *testing.T) {
	b := New([]string{"ab", "cd"})
	b.MoveDown() // row 1, col 0
	b.Backspace()
	got := b.Lines()
	if len(got) != 1 || got[0] != "abcd" {
		t.Fatalf("want [abcd] got %#v", got)
	}
	if r, c := b.Cursor(); r != 0 || c != 2 {
		t.Fatalf("want 0,2 got %d,%d", r, c)
	}
}

func TestBackspaceAtStartNoop(t *testing.T) {
	b := New([]string{"ab"})
	b.Backspace()
	if b.Lines()[0] != "ab" {
		t.Fatalf("want ab got %q", b.Lines()[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/buffer/`
Expected: FAIL (undefined methods).

- [ ] **Step 3: Write minimal implementation**

Append to `buffer.go`:

```go
func (b *Buffer) InsertRune(r rune) {
	line := b.lines[b.row]
	b.lines[b.row] = line[:b.col] + string(r) + line[b.col:]
	b.col++
}

func (b *Buffer) InsertNewline() {
	line := b.lines[b.row]
	head, tail := line[:b.col], line[b.col:]
	b.lines[b.row] = head
	// insert tail as a new line after current row
	b.lines = append(b.lines, "")
	copy(b.lines[b.row+2:], b.lines[b.row+1:])
	b.lines[b.row+1] = tail
	b.row++
	b.col = 0
}

func (b *Buffer) Backspace() {
	if b.col > 0 {
		line := b.lines[b.row]
		b.lines[b.row] = line[:b.col-1] + line[b.col:]
		b.col--
		return
	}
	if b.row == 0 {
		return
	}
	prev := b.lines[b.row-1]
	joinCol := len(prev)
	b.lines[b.row-1] = prev + b.lines[b.row]
	// remove current row
	b.lines = append(b.lines[:b.row], b.lines[b.row+1:]...)
	b.row--
	b.col = joinCol
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/buffer/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/buffer/
git commit -m "feat(buffer): insert, newline, backspace"
```

---

### Task 5: `fileio` package — load and save

**Files:**
- Create: `internal/fileio/fileio.go`
- Test: `internal/fileio/fileio_test.go`

**Interfaces:**
- Produces:
  - `func Load(path string) ([]string, error)` — reads file, splits on `\n`, strips a trailing `\r` per line (Windows). Missing file → `[]string{""}, nil`. Empty file → `[]string{""}`.
  - `func Save(path string, lines []string) error` — writes `strings.Join(lines, "\n")` with a trailing newline, perm 0644.

- [ ] **Step 1: Write the failing tests**

```go
package fileio

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFile(t *testing.T) {
	lines, err := Load(filepath.Join(t.TempDir(), "nope.txt"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(lines) != 1 || lines[0] != "" {
		t.Fatalf("want one blank line, got %#v", lines)
	}
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := Save(p, []string{"hello", "world"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	lines, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(lines) != 2 || lines[0] != "hello" || lines[1] != "world" {
		t.Fatalf("round trip bad: %#v", lines)
	}
}

func TestLoadStripsCarriageReturn(t *testing.T) {
	p := filepath.Join(t.TempDir(), "crlf.txt")
	if err := os.WriteFile(p, []byte("a\r\nb\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	lines, _ := Load(p)
	if len(lines) < 2 || lines[0] != "a" || lines[1] != "b" {
		t.Fatalf("want a,b got %#v", lines)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/fileio/`
Expected: FAIL (undefined: Load).

- [ ] **Step 3: Write minimal implementation**

```go
package fileio

import (
	"os"
	"strings"
)

// Load reads a file into lines. A missing file yields a single blank line.
func Load(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{""}, nil
		}
		return nil, err
	}
	text := strings.TrimSuffix(string(data), "\n")
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	if len(lines) == 0 {
		return []string{""}, nil
	}
	return lines, nil
}

// Save writes lines joined by newlines, with a trailing newline.
func Save(path string, lines []string) error {
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/fileio/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/fileio/
git commit -m "feat(fileio): load and save"
```

---

### Task 6: `render` package — draw statusbar, gutter, text

**Files:**
- Create: `internal/render/render.go`

**Interfaces:**
- Consumes: `*buffer.Buffer` (for `Lines()`, `Cursor()`, `LineCount()`).
- Produces:
  - `func GutterWidth(lineCount int) int` — digits of lineCount + 2 (padding + separator space). Minimum 4.
  - `func Draw(s tcell.Screen, b *buffer.Buffer, filename string, modified bool, scroll int)` — clears screen, draws statusbar on row 0, then visible lines starting at buffer row `scroll`, and positions the tcell cursor.
- Behavior: statusbar shows `"<filename> — <n> lines"` left-aligned and `"Ln <r>, Col <c>  [modified]"` right-aligned (1-based Ln/Col; `[modified]` only when modified). Gutter shows right-aligned 1-based line numbers + `│`. Text clipped to screen width. tcell cursor placed at the caret's screen position.

**Note:** No automated test (tcell-dependent). Verified by running in Task 8. `GutterWidth` is pure but small; covered implicitly.

- [ ] **Step 1: Write the implementation**

```go
package render

import (
	"fmt"
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/bftelman/slopcode/internal/buffer"
)

// GutterWidth returns the column width reserved for line numbers + separator.
func GutterWidth(lineCount int) int {
	digits := len(strconv.Itoa(lineCount))
	w := digits + 2 // one leading space, one space before separator
	if w < 4 {
		w = 4
	}
	return w
}

func drawText(s tcell.Screen, x, y int, text string, style tcell.Style) {
	for i, r := range text {
		s.SetContent(x+i, y, r, nil, style)
	}
}

// Draw renders the full editor frame.
func Draw(s tcell.Screen, b *buffer.Buffer, filename string, modified bool, scroll int) {
	s.Clear()
	width, height := s.Size()

	// Statusbar (row 0, inverted).
	bar := tcell.StyleDefault.Reverse(true)
	for x := 0; x < width; x++ {
		s.SetContent(x, 0, ' ', nil, bar)
	}
	left := fmt.Sprintf(" %s — %d lines", filename, b.LineCount())
	drawText(s, 0, 0, clip(left, width), bar)
	row, col := b.Cursor()
	right := fmt.Sprintf("Ln %d, Col %d", row+1, col+1)
	if modified {
		right += "  [modified]"
	}
	right += " "
	if len(right) < width {
		drawText(s, width-len(right), 0, right, bar)
	}

	// Text area.
	gw := GutterWidth(b.LineCount())
	numStyle := tcell.StyleDefault.Foreground(tcell.ColorGray)
	lines := b.Lines()
	textRows := height - 1
	for i := 0; i < textRows; i++ {
		lineIdx := scroll + i
		if lineIdx >= len(lines) {
			break
		}
		y := i + 1
		num := strconv.Itoa(lineIdx + 1)
		drawText(s, gw-2-len(num), y, num, numStyle)
		s.SetContent(gw-1, y, '│', nil, numStyle)
		drawText(s, gw, y, clip(lines[lineIdx], width-gw), tcell.StyleDefault)
	}

	// Cursor position on screen.
	cy := row - scroll + 1
	cx := gw + col
	if cy >= 1 && cy < height {
		s.ShowCursor(cx, cy)
	} else {
		s.HideCursor()
	}
	s.Show()
}

func clip(s string, max int) string {
	if max < 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return s[:max]
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./...`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/render/
git commit -m "feat(render): statusbar, gutter, and text drawing"
```

---

### Task 7: `editor` package — event loop, scrolling, key dispatch

**Files:**
- Create: `internal/editor/editor.go`

**Interfaces:**
- Consumes: `buffer`, `fileio`, `render`, `tcell`.
- Produces:
  - `type Editor struct { ... }`
  - `func New(s tcell.Screen, b *buffer.Buffer, path string) *Editor`
  - `func (e *Editor) Run()` — poll events until quit.
- Behavior:
  - Key dispatch: printable rune → `InsertRune`; Enter → `InsertNewline`; Backspace/Backspace2 → `Backspace`; arrows → Move*; Home/End → MoveHome/MoveEnd; Ctrl+S → save (set/clear modified, show error in status on failure); Ctrl+Q → quit. Any buffer-mutating key sets `modified = true`.
  - After each event: adjust `scroll` so the cursor row is visible (`scroll <= row < scroll + textRows`), then `render.Draw`.
  - Resize event → `s.Sync()` then redraw.
  - Save error is stored and shown via a modified filename suffix in the statusbar is out of scope; instead print via status — for MVP, on save error set `modified` true and overwrite the filename shown with the error. (Kept simple: store `statusMsg`, pass into Draw as part of filename.)

**Note:** No automated test (tcell-dependent). Verified by running in Task 8.

- [ ] **Step 1: Write the implementation**

```go
package editor

import (
	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/buffer"
	"github.com/bftelman/slopcode/internal/fileio"
	"github.com/bftelman/slopcode/internal/render"
)

type Editor struct {
	s        tcell.Screen
	b        *buffer.Buffer
	path     string
	scroll   int
	modified bool
	status   string // transient message shown in place of filename
}

func New(s tcell.Screen, b *buffer.Buffer, path string) *Editor {
	return &Editor{s: s, b: b, path: path, status: path}
}

func (e *Editor) Run() {
	e.draw()
	for {
		ev := e.s.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventResize:
			e.s.Sync()
		case *tcell.EventKey:
			if e.handleKey(ev) {
				return
			}
		}
		e.draw()
	}
}

// handleKey returns true when the editor should quit.
func (e *Editor) handleKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyCtrlQ:
		return true
	case tcell.KeyCtrlS:
		if err := fileio.Save(e.path, e.b.Lines()); err != nil {
			e.status = "SAVE ERROR: " + err.Error()
		} else {
			e.modified = false
			e.status = e.path
		}
	case tcell.KeyEnter:
		e.b.InsertNewline()
		e.modified = true
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		e.b.Backspace()
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
		e.b.InsertRune('\t')
		e.modified = true
	case tcell.KeyRune:
		e.b.InsertRune(ev.Rune())
		e.modified = true
	}
	return false
}

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
	render.Draw(e.s, e.b, e.status, e.modified, e.scroll)
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./...`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/editor/
git commit -m "feat(editor): event loop, scrolling, key dispatch"
```

---

### Task 8: `main.go` — wire everything, run, manual verify

**Files:**
- Create: `main.go`

**Interfaces:**
- Consumes: `buffer`, `fileio`, `editor`, `tcell`.

- [ ] **Step 1: Write the implementation**

```go
package main

import (
	"fmt"
	"os"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/buffer"
	"github.com/bftelman/slopcode/internal/editor"
	"github.com/bftelman/slopcode/internal/fileio"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: slopcode <filename>")
		os.Exit(2)
	}
	path := os.Args[1]

	lines, err := fileio.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot open %s: %v\n", path, err)
		os.Exit(1)
	}

	s, err := tcell.NewScreen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot init screen: %v\n", err)
		os.Exit(1)
	}
	if err := s.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "cannot init screen: %v\n", err)
		os.Exit(1)
	}
	defer s.Fini()

	b := buffer.New(lines)
	editor.New(s, b, path).Run()
}
```

- [ ] **Step 2: Verify it builds and all tests pass**

Run: `go build ./... && go test ./...`
Expected: build exit 0; tests PASS.

- [ ] **Step 3: Manual smoke test**

Run: `go run . scratch.txt`
Verify: fullscreen opens; statusbar shows `scratch.txt — 1 lines`; typing inserts text; Enter splits lines; arrows move; line numbers increment; Ctrl+S saves (marker clears); Ctrl+Q quits. Confirm `scratch.txt` exists with the typed content afterwards.

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat: wire up main entrypoint"
```

---

## Self-Review

**Spec coverage:**
- Fullscreen + tcell → Tasks 1, 6, 7, 8. ✓
- Open/create file, create-on-save → Tasks 5, 8 (Load missing → blank; Save writes). ✓
- Edit text (insert/newline/backspace) → Task 4. ✓
- Cursor movement + Home/End → Task 3, dispatch in Task 7. ✓
- Ctrl+S save / Ctrl+Q quit → Task 7. ✓
- Line numbers gutter → Task 6. ✓
- Statusbar (filename, line count, cursor pos, modified) → Task 6/7. ✓
- Resize handling → Task 7. ✓
- Vertical scrolling, horizontal clip → Tasks 6 (clip) + 7 (scroll). ✓
- Required filename arg + error handling → Task 8. ✓
- `buffer`/`fileio` no tcell import → Tasks 2–5 (imports are stdlib only). ✓

**Placeholder scan:** No TBD/TODO. Every code step has full code. (Task 7 note text clarified: save errors are shown by replacing the status string.) ✓

**Type consistency:** `New`, `Lines`, `Cursor`, `LineCount`, `Move*`, `InsertRune`, `InsertNewline`, `Backspace`, `GutterWidth`, `Draw`, `Load`, `Save` used consistently across tasks. `render.Draw` signature `(s, b, filename, modified, scroll)` matches its call in `editor.draw()`. ✓
