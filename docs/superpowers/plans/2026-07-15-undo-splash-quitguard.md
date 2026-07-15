# Undo/Redo, Splash & Quit Guard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Add per-change undo/redo, a no-filename splash banner, an unnamed-save fallback, and a hard unsaved-changes quit guard.

**Architecture:** Undo/redo is pure state in `buffer`. `render` gains `DrawSplash` + a rune-correct `drawText`. `editor` wires undo/redo keys + checkpoints, splash gating, `save()` fallback, and a shared `tryQuit`. `main` accepts 0 or 1 args.

**Tech Stack:** Go 1.25.4, tcell/v2, chroma/v2 (existing). No new deps.

## Global Constraints

- `buffer` stays tcell/chroma-free.
- Undo/redo: per-change snapshots; Ctrl+Z / Ctrl+Y; cap 500.
- No filename → splash while pristine; unnamed save → `untitled.txt` (working dir).
- Ctrl+Q hard-blocks while modified (notice, no quit).
- `[modified]` marker already renders — do not remove it.

## File Structure

- `internal/buffer/buffer.go` — undo/redo state + methods.
- `internal/render/render.go` — `drawText` rune fix, `DrawSplash`, banner var.
- `internal/editor/editor.go` — checkpoints, undo/redo keys, splash gating, save fallback, quit guard.
- `main.go` — 0/1 args.

---

### Task 1: `buffer` undo/redo

**Files:** Modify `internal/buffer/buffer.go`; Test `internal/buffer/buffer_test.go`.

**Interfaces:** `func (b *Buffer) Checkpoint()`, `func (b *Buffer) Undo() bool`, `func (b *Buffer) Redo() bool`.

- [ ] **Step 1: Failing tests**

```go
func TestUndoRedo(t *testing.T) {
	b := New([]string{"ab"})
	b.MoveEnd()
	b.Checkpoint()
	b.InsertRune('c') // "abc"
	if b.Lines()[0] != "abc" {
		t.Fatalf("setup: %q", b.Lines()[0])
	}
	if !b.Undo() {
		t.Fatal("undo should succeed")
	}
	if b.Lines()[0] != "ab" {
		t.Fatalf("after undo want ab, got %q", b.Lines()[0])
	}
	if _, c := b.Cursor(); c != 2 {
		t.Fatalf("cursor should restore to 2, got %d", c)
	}
	if !b.Redo() {
		t.Fatal("redo should succeed")
	}
	if b.Lines()[0] != "abc" {
		t.Fatalf("after redo want abc, got %q", b.Lines()[0])
	}
}

func TestUndoEmptyReturnsFalse(t *testing.T) {
	b := New([]string{"x"})
	if b.Undo() {
		t.Fatal("undo on empty stack should return false")
	}
}

func TestCheckpointClearsRedo(t *testing.T) {
	b := New([]string{""})
	b.Checkpoint()
	b.InsertRune('a')
	b.Undo() // redo now has one entry
	b.Checkpoint()
	b.InsertRune('b')
	if b.Redo() {
		t.Fatal("redo should be cleared after a new checkpoint")
	}
}
```

- [ ] **Step 2: Run — expect FAIL** (`go test ./internal/buffer/`).

- [ ] **Step 3: Implement** — add state type, fields, and methods to buffer.go.

Add fields to the `Buffer` struct:

```go
	undo []state
	redo []state
```

Add near the top (after the struct):

```go
const maxUndo = 500

type state struct {
	lines []string
	row   int
	col   int
}

func (b *Buffer) snapshot() state {
	cp := make([]string, len(b.lines))
	copy(cp, b.lines)
	return state{lines: cp, row: b.row, col: b.col}
}

func (b *Buffer) restore(s state) {
	cp := make([]string, len(s.lines))
	copy(cp, s.lines)
	b.lines = cp
	b.row = s.row
	b.col = s.col
}

// Checkpoint records the current state for undo and clears the redo stack.
func (b *Buffer) Checkpoint() {
	b.undo = append(b.undo, b.snapshot())
	if len(b.undo) > maxUndo {
		b.undo = b.undo[len(b.undo)-maxUndo:]
	}
	b.redo = nil
}

// Undo reverts to the previous checkpoint. Returns false if none.
func (b *Buffer) Undo() bool {
	if len(b.undo) == 0 {
		return false
	}
	b.redo = append(b.redo, b.snapshot())
	last := b.undo[len(b.undo)-1]
	b.undo = b.undo[:len(b.undo)-1]
	b.restore(last)
	return true
}

// Redo reapplies the most recently undone state. Returns false if none.
func (b *Buffer) Redo() bool {
	if len(b.redo) == 0 {
		return false
	}
	b.undo = append(b.undo, b.snapshot())
	last := b.redo[len(b.redo)-1]
	b.redo = b.redo[:len(b.redo)-1]
	b.restore(last)
	return true
}
```

- [ ] **Step 4: Run — expect PASS**.

- [ ] **Step 5: Commit** — `git add internal/buffer/ && git commit -m "feat(buffer): per-change undo/redo"`

---

### Task 2: `render` — drawText rune fix + DrawSplash

**Files:** Modify `internal/render/render.go`, `internal/render/render_test.go`.

**Interfaces:** `func DrawSplash(s tcell.Screen, filename, notice string)`.

- [ ] **Step 1: Failing test** (append to render_test.go)

```go
func TestDrawSplashShowsBanner(t *testing.T) {
	s := newSimScreen(t, 80, 24)
	defer s.Fini()
	DrawSplash(s, "[No Name]", "")

	cells, width, height := s.GetContents()
	blocks, subtitle := false, false
	for y := 0; y < height; y++ {
		var row []rune
		for x := 0; x < width; x++ {
			r := cellAt(cells, width, x, y).Runes
			if len(r) == 1 {
				row = append(row, r[0])
				if r[0] == '█' {
					blocks = true
				}
			} else {
				row = append(row, ' ')
			}
		}
		if contains(string(row), "bftelman") {
			subtitle = true
		}
	}
	if !blocks {
		t.Fatal("splash should render block banner")
	}
	if !subtitle {
		t.Fatal("splash should show subtitle by @bftelman")
	}
}

func TestDrawTextMultibyteColumns(t *testing.T) {
	s := newSimScreen(t, 20, 3)
	defer s.Fini()
	drawText(s, 0, 0, "█x", tcell.StyleDefault)
	s.Show()
	cells, width, _ := s.GetContents()
	if r := cellAt(cells, width, 0, 0).Runes; len(r) != 1 || r[0] != '█' {
		t.Fatalf("cell 0 = %q, want block", r)
	}
	if r := cellAt(cells, width, 1, 0).Runes; len(r) != 1 || r[0] != 'x' {
		t.Fatalf("cell 1 = %q, want x at column 1", r)
	}
}
```

- [ ] **Step 2: Run — expect FAIL** (`go test ./internal/render/`, undefined DrawSplash / column mismatch).

- [ ] **Step 3: Fix drawText** — replace its body:

```go
func drawText(s tcell.Screen, x, y int, text string, style tcell.Style) {
	col := 0
	for _, r := range text {
		s.SetContent(x+col, y, r, nil, style)
		col++
	}
}
```

- [ ] **Step 4: Add banner var + DrawSplash + import**

Add `"unicode/utf8"` to the imports. Add:

```go
var splashArt = []string{
	"█████ █     █████ █████ █████ █████ ████  █████",
	"█     █     █   █ █   █ █     █   █ █   █ █    ",
	"█████ █     █   █ █████ █     █   █ █   █ █████",
	"    █ █     █   █ █     █     █   █ █   █ █    ",
	"█████ █████ █████ █     █████ █████ ████  █████",
}

const splashSubtitle = "by @bftelman"

// DrawSplash renders the no-file welcome screen: statusbar + centered banner.
func DrawSplash(s tcell.Screen, filename, notice string) {
	s.Clear()
	width, height := s.Size()

	bar := tcell.StyleDefault.Reverse(true)
	for x := 0; x < width; x++ {
		s.SetContent(x, 0, ' ', nil, bar)
	}
	drawText(s, 0, 0, clip(" "+filename, width), bar)
	if notice != "" {
		noticeStyle := bar.Foreground(tcell.ColorGreen)
		nx := width - utf8.RuneCountInString(notice) - 1
		if nx > utf8.RuneCountInString(filename)+2 {
			drawText(s, nx, 0, notice, noticeStyle)
		}
	}

	artStyle := tcell.StyleDefault.Foreground(tcell.ColorAqua)
	block := len(splashArt) + 2 // banner + blank + subtitle
	top := 1 + (height-1-block)/2
	if top < 1 {
		top = 1
	}
	for i, line := range splashArt {
		x := (width - utf8.RuneCountInString(line)) / 2
		if x < 0 {
			x = 0
		}
		drawText(s, x, top+i, line, artStyle)
	}
	sx := (width - utf8.RuneCountInString(splashSubtitle)) / 2
	if sx < 0 {
		sx = 0
	}
	drawText(s, sx, top+len(splashArt)+1, splashSubtitle, tcell.StyleDefault)

	s.HideCursor()
	s.Show()
}
```

- [ ] **Step 5: Run — expect PASS** (`go test ./internal/render/`).

- [ ] **Step 6: Commit** — `git add internal/render/ && git commit -m "feat(render): splash banner; fix drawText multibyte columns"`

---

### Task 3: `editor` — checkpoints, undo/redo, splash, save fallback, quit guard

**Files:** Modify `internal/editor/editor.go`, `internal/editor/editor_test.go`.

- [ ] **Step 1: Failing tests** (append to editor_test.go)

```go
func TestUndoRedoKeys(t *testing.T) {
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(80, 24)

	e := New(s, buffer.New([]string{""}), filepath.Join(t.TempDir(), "x.txt"))
	e.handleKey(keyEvent(tcell.KeyRune, 'a'))
	e.handleKey(keyEvent(tcell.KeyRune, 'b'))
	if e.b.Lines()[0] != "ab" {
		t.Fatalf("setup: %q", e.b.Lines()[0])
	}
	e.handleKey(keyEvent(tcell.KeyCtrlZ))
	if e.b.Lines()[0] != "a" {
		t.Fatalf("after undo want a, got %q", e.b.Lines()[0])
	}
	e.handleKey(keyEvent(tcell.KeyCtrlY))
	if e.b.Lines()[0] != "ab" {
		t.Fatalf("after redo want ab, got %q", e.b.Lines()[0])
	}
}

func TestQuitGuardBlocksWhenModified(t *testing.T) {
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(80, 24)

	e := New(s, buffer.New([]string{""}), filepath.Join(t.TempDir(), "x.txt"))
	e.handleKey(keyEvent(tcell.KeyRune, 'z')) // modify
	if e.handleKey(keyEvent(tcell.KeyCtrlQ)) {
		t.Fatal("should NOT quit while modified")
	}
	if e.notice == "" {
		t.Fatal("expected unsaved-changes notice")
	}
	e.handleKey(keyEvent(tcell.KeyCtrlS)) // save clears modified
	if !e.handleKey(keyEvent(tcell.KeyCtrlQ)) {
		t.Fatal("should quit after saving")
	}
}

func TestUnnamedSaveWritesUntitled(t *testing.T) {
	t.Chdir(t.TempDir())
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(80, 24)

	e := New(s, buffer.New(nil), "") // no filename
	e.handleKey(keyEvent(tcell.KeyRune, 'h'))
	e.handleKey(keyEvent(tcell.KeyCtrlS))
	if e.path != "untitled.txt" {
		t.Fatalf("path = %q, want untitled.txt", e.path)
	}
	if _, err := os.Stat("untitled.txt"); err != nil {
		t.Fatalf("untitled.txt not written: %v", err)
	}
}
```

- [ ] **Step 2: Run — expect FAIL**.

- [ ] **Step 3: Implement editor.go changes**

Add a `displayName` helper and route Ctrl+Q through `tryQuit`; add checkpoints and
undo/redo; splash gating in `draw`; save fallback. Replace the relevant parts:

`handleKey` — Ctrl+Q, Ctrl+Z, Ctrl+Y, and checkpoint-before-mutation:

```go
func (e *Editor) handleKey(ev *tcell.EventKey) bool {
	if e.browser != nil {
		return e.handleBrowseKey(ev)
	}
	e.notice = "" // any key clears the transient notice
	switch ev.Key() {
	case tcell.KeyCtrlQ:
		return e.tryQuit()
	case tcell.KeyCtrlB:
		e.openBrowser()
	case tcell.KeyCtrlS:
		e.save()
	case tcell.KeyCtrlZ:
		if e.b.Undo() {
			e.modified = true
		}
	case tcell.KeyCtrlY:
		if e.b.Redo() {
			e.modified = true
		}
	case tcell.KeyEnter:
		e.b.Checkpoint()
		e.b.InsertNewline()
		e.modified = true
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		e.b.Checkpoint()
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
		e.b.Checkpoint()
		e.b.InsertTab(render.TabWidth)
		e.modified = true
	case tcell.KeyRune:
		e.b.Checkpoint()
		autopair.InsertRune(e.b, ev.Rune())
		e.modified = true
	}
	return false
}

// tryQuit returns true only when it is safe to quit (no unsaved changes).
func (e *Editor) tryQuit() bool {
	if e.modified {
		e.notice = "unsaved changes — Ctrl+S to save before quitting"
		return false
	}
	return true
}

func (e *Editor) displayName() string {
	if e.path == "" {
		return "[No Name]"
	}
	return e.path
}
```

Update `save()` for the unnamed fallback:

```go
func (e *Editor) save() {
	if e.path == "" {
		e.path = "untitled.txt"
	}
	if err := fileio.Save(e.path, e.b.Lines()); err != nil {
		e.notice = "SAVE ERROR: " + err.Error()
		return
	}
	e.modified = false
	e.notice = e.path + " saved"
}
```

Update `handleBrowseKey` Ctrl+Q to use the guard:

```go
	case tcell.KeyCtrlQ:
		return e.tryQuit()
```

Update `draw()` to gate splash and use `displayName()`:

```go
func (e *Editor) draw() {
	if e.browser == nil && e.path == "" && !e.modified {
		render.DrawSplash(e.s, e.displayName(), e.notice)
		return
	}
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
		render.Draw(e.s, e.b, e.displayName(), e.notice, e.modified, e.scroll, render.SidebarWidth, false)
		render.DrawSidebar(e.s, e.browser)
	} else {
		render.Draw(e.s, e.b, e.displayName(), e.notice, e.modified, e.scroll, 0, true)
	}
}
```

- [ ] **Step 4: Run — expect PASS** (`go build ./... && go test ./...`).

- [ ] **Step 5: Commit** — `git add internal/editor/ && git commit -m "feat(editor): undo/redo keys, splash gating, untitled save, hard quit guard"`

---

### Task 4: `main.go` — accept 0 or 1 args

**Files:** Modify `main.go`.

- [ ] **Step 1: Implement**

```go
func main() {
	if len(os.Args) > 2 {
		fmt.Fprintln(os.Stderr, "usage: slopcode [filename]")
		os.Exit(2)
	}
	path := ""
	if len(os.Args) == 2 {
		path = os.Args[1]
	}

	var lines []string
	if path != "" {
		var err error
		lines, err = fileio.Load(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cannot open %s: %v\n", path, err)
			os.Exit(1)
		}
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

- [ ] **Step 2: Verify** — `go build ./... && ./slopcode a b c; echo $?` (expect usage + exit 2); `go vet ./...`.

- [ ] **Step 3: Commit** — `git add main.go && git commit -m "feat: launch with no filename (splash mode)"`

---

### Task 5: Full verification

- [ ] **Step 1:** `go build ./... && go test ./... && go vet ./...` — all clean.
- [ ] **Step 2: Manual smoke** — `go run .` (no arg): SLOPCODE banner shows; typing replaces
  it with the buffer and `[No Name]` + `[modified]` appear; Ctrl+Z/Ctrl+Y undo/redo;
  Ctrl+Q is blocked with a notice; Ctrl+S writes `untitled.txt`; Ctrl+Q then quits.
- [ ] **Step 3:** Commit any leftovers.

---

## Self-Review

- **Coverage:** undo/redo (Tasks 1,3) ✓; Ctrl+Z/Y keys (Task 3) ✓; splash no-arg (Tasks 2,3,4) ✓;
  fill-on-typing = splash gate `!modified` (Task 3 `draw`) ✓; `[modified]` anytime (existing, retained) ✓;
  quit guard hard-block (Task 3 `tryQuit`, both modes) ✓; unnamed save → untitled.txt (Task 3 `save`) ✓.
- **Placeholders:** none.
- **Type consistency:** `Checkpoint/Undo/Redo` (Task 1 → Task 3) ✓; `DrawSplash(s, filename, notice)`
  (Task 2 → Task 3) ✓; `displayName()`/`tryQuit()` defined and used in Task 3 ✓; `Draw` signature unchanged
  from prior plan ✓; `main` passes `path=""` → `New(s, buffer.New(nil), "")` matches splash gate ✓.
