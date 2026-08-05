# Editor Enhancements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a save notification, soft tabs, and chroma-based syntax highlighting to the namlet editor.

**Architecture:** Keep `buffer`/`fileio` pure. Add `internal/highlight` (chroma → tcell styles). Extend `render` to draw styled runes with tab expansion and a notice; extend `editor` with Tab→spaces and a clear-on-keypress notice.

**Tech Stack:** Go 1.25.4, `github.com/gdamore/tcell/v2`, `github.com/alecthomas/chroma/v2` (already added).

## Global Constraints

- `buffer` and `fileio` MUST NOT import tcell or chroma.
- File content is never rewritten to convert tabs↔spaces (Save unchanged).
- `TabWidth = 4`, `StyleName = "monokai"` (constants in `render`).
- chroma color → tcell: `tcell.NewRGBColor(int32(c.Red()), int32(c.Green()), int32(c.Blue()))`, only when `c.IsSet()`.

## File Structure

- `internal/buffer/buffer.go` — add `InsertTab`.
- `internal/highlight/highlight.go` — new: `StyledRune`, `Highlight`.
- `internal/highlight/highlight_test.go` — new.
- `internal/render/render.go` — styled drawing, tab expansion, notice param, `screenCol`.
- `internal/render/render_test.go` — new: `screenCol` tests.
- `internal/editor/editor.go` — Tab→`InsertTab`, `notice` field, save message, clear-on-key.
- `internal/editor/editor_test.go` — extend.

---

### Task 1: `buffer.InsertTab`

**Files:**
- Modify: `internal/buffer/buffer.go`
- Test: `internal/buffer/buffer_test.go`

**Interfaces:**
- Produces: `func (b *Buffer) InsertTab(width int)` — inserts `width - (col % width)` spaces at the cursor, advancing the column.

- [ ] **Step 1: Write failing tests**

```go
func TestInsertTabFromColZero(t *testing.T) {
	b := New([]string{""})
	b.InsertTab(4)
	if b.Lines()[0] != "    " {
		t.Fatalf("want 4 spaces, got %q", b.Lines()[0])
	}
	if _, c := b.Cursor(); c != 4 {
		t.Fatalf("want col 4 got %d", c)
	}
}

func TestInsertTabToNextStop(t *testing.T) {
	b := New([]string{"ab"})
	b.MoveEnd() // col 2
	b.InsertTab(4)
	if b.Lines()[0] != "ab  " { // 2 spaces to reach stop 4
		t.Fatalf("want 'ab  ', got %q", b.Lines()[0])
	}
	if _, c := b.Cursor(); c != 4 {
		t.Fatalf("want col 4 got %d", c)
	}
}

func TestInsertTabOnStopInsertsFullWidth(t *testing.T) {
	b := New([]string{"abcd"})
	b.MoveEnd() // col 4
	b.InsertTab(4)
	if b.Lines()[0] != "abcd    " {
		t.Fatalf("want full tab, got %q", b.Lines()[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/buffer/`
Expected: FAIL (undefined InsertTab).

- [ ] **Step 3: Implement**

Append to `buffer.go`:

```go
// InsertTab inserts spaces to advance the cursor to the next tab stop.
func (b *Buffer) InsertTab(width int) {
	if width < 1 {
		width = 1
	}
	n := width - (b.col % width)
	for i := 0; i < n; i++ {
		b.InsertRune(' ')
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/buffer/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/buffer/
git commit -m "feat(buffer): InsertTab inserts spaces to next tab stop"
```

---

### Task 2: `highlight` package

**Files:**
- Create: `internal/highlight/highlight.go`
- Test: `internal/highlight/highlight_test.go`

**Interfaces:**
- Produces:
  - `type StyledRune struct { R rune; Style tcell.Style }`
  - `func Highlight(text, filename, styleName string) [][]StyledRune` — per-line styled runes, 1:1 with source characters. Never returns nil (empty text → `[][]StyledRune{{}}`).

- [ ] **Step 1: Write failing tests**

```go
package highlight

import (
	"strings"
	"testing"
)

func TestHighlightRoundTripsCharacters(t *testing.T) {
	src := "package main\n\nfunc main() {}\n"
	lines := Highlight(src, "main.go", "monokai")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		for _, sr := range line {
			b.WriteRune(sr.R)
		}
	}
	if b.String() != src {
		t.Fatalf("round trip mismatch:\n got %q\nwant %q", b.String(), src)
	}
}

func TestHighlightSplitsLines(t *testing.T) {
	lines := Highlight("a\nb\nc", "x.go", "monokai")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d", len(lines))
	}
}

func TestHighlightEmpty(t *testing.T) {
	lines := Highlight("", "x.txt", "monokai")
	if len(lines) != 1 {
		t.Fatalf("want 1 line for empty input, got %d", len(lines))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/highlight/`
Expected: FAIL (undefined Highlight).

- [ ] **Step 3: Implement**

```go
// Package highlight turns source text into per-line styled runes using chroma.
package highlight

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/gdamore/tcell/v2"
)

// StyledRune is a single rune paired with its display style.
type StyledRune struct {
	R     rune
	Style tcell.Style
}

// Highlight tokenises text (language chosen by filename) and returns per-line
// styled runes. The concatenation of all runes equals the input exactly.
func Highlight(text, filename, styleName string) [][]StyledRune {
	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	style := styles.Get(styleName)
	if style == nil {
		style = styles.Fallback
	}

	tokens, err := chroma.Tokenise(lexer, nil, text)
	if err != nil {
		// Fall back to unstyled single-run.
		return splitPlain(text)
	}

	lines := [][]StyledRune{{}}
	for _, tok := range tokens {
		st := toTcellStyle(style.Get(tok.Type))
		for _, r := range tok.Value {
			if r == '\n' {
				lines = append(lines, []StyledRune{})
				continue
			}
			last := len(lines) - 1
			lines[last] = append(lines[last], StyledRune{R: r, Style: st})
		}
	}
	return lines
}

func splitPlain(text string) [][]StyledRune {
	lines := [][]StyledRune{{}}
	for _, r := range text {
		if r == '\n' {
			lines = append(lines, []StyledRune{})
			continue
		}
		last := len(lines) - 1
		lines[last] = append(lines[last], StyledRune{R: r, Style: tcell.StyleDefault})
	}
	return lines
}

func toTcellStyle(e chroma.StyleEntry) tcell.Style {
	s := tcell.StyleDefault
	if e.Colour.IsSet() {
		s = s.Foreground(tcell.NewRGBColor(
			int32(e.Colour.Red()), int32(e.Colour.Green()), int32(e.Colour.Blue())))
	}
	if e.Bold == chroma.Yes {
		s = s.Bold(true)
	}
	if e.Italic == chroma.Yes {
		s = s.Italic(true)
	}
	if e.Underline == chroma.Yes {
		s = s.Underline(true)
	}
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/highlight/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/highlight/
git commit -m "feat(highlight): chroma-based per-line styled runes"
```

---

### Task 3: `render` — screenCol helper + tests

**Files:**
- Modify: `internal/render/render.go`
- Test: `internal/render/render_test.go`

**Interfaces:**
- Produces: `func screenCol(line string, byteCol, tabWidth int) int` — screen x-offset of a byte column, expanding tabs to the next tab stop.

- [ ] **Step 1: Write failing tests**

```go
package render

import "testing"

func TestScreenColNoTabs(t *testing.T) {
	if got := screenCol("abc", 2, 4); got != 2 {
		t.Fatalf("want 2 got %d", got)
	}
}

func TestScreenColLeadingTab(t *testing.T) {
	if got := screenCol("\tx", 1, 4); got != 4 {
		t.Fatalf("want 4 got %d", got)
	}
}

func TestScreenColTabAfterTwoChars(t *testing.T) {
	// "ab\t": cols a(0->1) b(1->2) tab(2->4)
	if got := screenCol("ab\t", 3, 4); got != 4 {
		t.Fatalf("want 4 got %d", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/render/`
Expected: FAIL (undefined screenCol).

- [ ] **Step 3: Implement**

Add to `render.go` (constants + helper):

```go
// TabWidth is the number of columns a tab stop spans.
const TabWidth = 4

// StyleName is the chroma style used for highlighting.
const StyleName = "monokai"

// screenCol returns the screen x-offset of byteCol in line, expanding tabs.
func screenCol(line string, byteCol, tabWidth int) int {
	x := 0
	for i, r := range line {
		if i >= byteCol {
			break
		}
		if r == '\t' {
			x += tabWidth - (x % tabWidth)
		} else {
			x++
		}
	}
	return x
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/render/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/render/
git commit -m "feat(render): tab-aware screenCol helper and constants"
```

---

### Task 4: `render.Draw` — styled drawing, tab expansion, notice

**Files:**
- Modify: `internal/render/render.go`

**Interfaces:**
- Consumes: `highlight.Highlight`, `buffer.Buffer`.
- Produces (signature change):
  `func Draw(s tcell.Screen, b *buffer.Buffer, filename, notice string, modified bool, scroll int)`

- [ ] **Step 1: Rewrite Draw**

Replace the existing `Draw` function body with:

```go
// Draw renders the full editor frame and positions the cursor.
func Draw(s tcell.Screen, b *buffer.Buffer, filename, notice string, modified bool, scroll int) {
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
	rightStart := width - len(right)
	if rightStart > 0 {
		drawText(s, rightStart, 0, right, bar)
	}
	if notice != "" {
		noticeStyle := bar.Foreground(tcell.ColorGreen)
		nx := rightStart - len(notice) - 2
		if nx > len(left) {
			drawText(s, nx, 0, notice, noticeStyle)
		}
	}

	// Text area.
	gw := GutterWidth(b.LineCount())
	numStyle := tcell.StyleDefault.Foreground(tcell.ColorGray)
	styled := highlight.Highlight(strings.Join(b.Lines(), "\n"), filename, StyleName)
	textRows := height - 1
	for i := 0; i < textRows; i++ {
		lineIdx := scroll + i
		if lineIdx >= len(styled) {
			break
		}
		y := i + 1
		num := strconv.Itoa(lineIdx + 1)
		drawText(s, gw-2-len(num), y, num, numStyle)
		s.SetContent(gw-1, y, '│', nil, numStyle)

		x := 0
		for _, sr := range styled[lineIdx] {
			if gw+x >= width {
				break
			}
			if sr.R == '\t' {
				stop := TabWidth - (x % TabWidth)
				for k := 0; k < stop && gw+x < width; k++ {
					s.SetContent(gw+x, y, ' ', nil, tcell.StyleDefault)
					x++
				}
				continue
			}
			s.SetContent(gw+x, y, sr.R, nil, sr.Style)
			x++
		}
	}

	// Cursor position on screen (tab-aware).
	lines := b.Lines()
	cy := row - scroll + 1
	cx := gw
	if row >= 0 && row < len(lines) {
		cx = gw + screenCol(lines[row], col, TabWidth)
	}
	if cy >= 1 && cy < height {
		s.ShowCursor(cx, cy)
	} else {
		s.HideCursor()
	}
	s.Show()
}
```

- [ ] **Step 2: Add imports**

Ensure `render.go` imports include `strings` and the highlight package:

```go
import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/namlet/internal/buffer"
	"github.com/bftelman/namlet/internal/highlight"
)
```

- [ ] **Step 3: Verify build + render tests still pass**

Run: `go build ./internal/render/ && go test ./internal/render/`
Expected: build ok; PASS. (Will not fully build repo yet — editor still calls old Draw signature; that's fixed in Task 5.)

- [ ] **Step 4: Commit**

```bash
git add internal/render/
git commit -m "feat(render): syntax highlighting, tab expansion, save notice"
```

---

### Task 5: `editor` — Tab→spaces, notice field, clear-on-key

**Files:**
- Modify: `internal/editor/editor.go`
- Test: `internal/editor/editor_test.go`

**Interfaces:**
- Consumes: `render.TabWidth`, `render.Draw` (new signature).
- Changes: rename `status` field to `notice`; Tab inserts spaces; notice cleared each key.

- [ ] **Step 1: Extend the simulation test**

Add to `internal/editor/editor_test.go`:

```go
func TestRunTabInsertsSpaces(t *testing.T) {
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	defer s.Fini()
	s.SetSize(80, 24)

	b := buffer.New(nil)
	e := New(s, b, filepath.Join(t.TempDir(), "x.txt"))
	s.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	s.InjectKey(tcell.KeyRune, 'x', tcell.ModNone)
	s.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone)
	e.Run()

	if got := b.Lines()[0]; got != "    x" {
		t.Fatalf("want 4 spaces then x, got %q", got)
	}
}

func TestRunNoticeSetOnSaveClearedOnNextKey(t *testing.T) {
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	defer s.Fini()
	s.SetSize(80, 24)

	path := filepath.Join(t.TempDir(), "n.txt")
	b := buffer.New([]string{"hi"})
	e := New(s, b, path)

	// Save, then check notice via a stepwise approach: inject save + quit,
	// but assert notice by calling handleKey directly for determinism.
	if e.handleKey(keyEvent(tcell.KeyCtrlS)) {
		t.Fatal("Ctrl+S should not quit")
	}
	if e.notice == "" {
		t.Fatal("expected notice after save")
	}
	e.handleKey(keyEvent(tcell.KeyRight))
	if e.notice != "" {
		t.Fatalf("notice should clear on next key, got %q", e.notice)
	}
}

func keyEvent(k tcell.Key) *tcell.EventKey {
	return tcell.NewEventKey(k, 0, tcell.ModNone)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/editor/`
Expected: FAIL (notice field undefined / build error against old code).

- [ ] **Step 3: Update `editor.go`**

Replace the struct field `status` with `notice`, update `New`, `handleKey`, and `draw`:

```go
type Editor struct {
	s        tcell.Screen
	b        *buffer.Buffer
	path     string
	scroll   int
	modified bool
	notice   string // transient message; cleared on next key
}

func New(s tcell.Screen, b *buffer.Buffer, path string) *Editor {
	return &Editor{s: s, b: b, path: path}
}
```

In `handleKey`, clear the notice first, and update Ctrl+S and Tab:

```go
func (e *Editor) handleKey(ev *tcell.EventKey) bool {
	e.notice = "" // any key clears the transient notice
	switch ev.Key() {
	case tcell.KeyCtrlQ:
		return true
	case tcell.KeyCtrlS:
		if err := fileio.Save(e.path, e.b.Lines()); err != nil {
			e.notice = "SAVE ERROR: " + err.Error()
		} else {
			e.modified = false
			e.notice = e.path + " saved"
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
		e.b.InsertTab(render.TabWidth)
		e.modified = true
	case tcell.KeyRune:
		e.b.InsertRune(ev.Rune())
		e.modified = true
	}
	return false
}
```

Update the `render.Draw` call in `draw()`:

```go
	render.Draw(e.s, e.b, e.path, e.notice, e.modified, e.scroll)
```

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./...`
Expected: build ok; all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/editor/
git commit -m "feat(editor): soft-tab key, save notice cleared on next key"
```

---

### Task 6: Full verification

- [ ] **Step 1: Build, test, vet**

Run: `go build ./... && go test ./... && go vet ./...`
Expected: all clean.

- [ ] **Step 2: Manual smoke (highlighting)**

Run: `go run . main.go` — confirm Go keywords/strings/comments are colored, tabs align,
Tab key inserts spaces, Ctrl+S shows `main.go saved` which clears on the next key, Ctrl+Q quits.
(Note: interactive TTY required; automated coverage via simulation screen already in place.)

- [ ] **Step 3: Commit any remaining changes (go.mod/go.sum)**

```bash
git add -A
git commit -m "chore: enhancements verification" || true
```

---

## Self-Review

**Spec coverage:** notification (Tasks 4,5) ✓; soft tabs (Tasks 1,5) ✓; tab render expansion (Tasks 3,4) ✓; highlighting (Tasks 2,4) ✓.
**Placeholder scan:** none.
**Type consistency:** `Draw(s, b, filename, notice, modified, scroll)` — defined Task 4, called Task 5 ✓. `InsertTab(width)` — Task 1, used Task 5 ✓. `Highlight(text, filename, styleName)` — Task 2, used Task 4 ✓. `StyledRune{R, Style}` consistent ✓. `screenCol` — Task 3, used Task 4 ✓.
