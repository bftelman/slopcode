package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/buffer"
	"github.com/bftelman/slopcode/internal/fileio"
	"github.com/bftelman/slopcode/internal/render"
)

// TestRunTypesEditsAndSaves drives the real event loop through a simulation
// screen: type "hi", newline, "there", Ctrl+S to save, Ctrl+Q to quit.
func TestRunTypesEditsAndSaves(t *testing.T) {
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatalf("init sim screen: %v", err)
	}
	defer s.Fini()
	s.SetSize(80, 24)

	path := filepath.Join(t.TempDir(), "out.txt")
	b := buffer.New(nil)
	e := New(s, b, path)

	for _, r := range "hi" {
		s.InjectKey(tcell.KeyRune, r, tcell.ModNone)
	}
	s.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	for _, r := range "there" {
		s.InjectKey(tcell.KeyRune, r, tcell.ModNone)
	}
	s.InjectKey(tcell.KeyCtrlS, 0, tcell.ModNone)
	s.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone)

	e.Run() // returns when Ctrl+Q is handled

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(got) != "hi\nthere\n" {
		t.Fatalf("saved content = %q, want %q", string(got), "hi\nthere\n")
	}

	// Buffer should also reflect the edits and clear the modified flag on save.
	lines, _ := fileio.Load(path)
	if len(lines) != 2 || lines[0] != "hi" || lines[1] != "there" {
		t.Fatalf("reloaded lines = %#v", lines)
	}
	if e.modified {
		t.Fatalf("expected modified=false after save")
	}
}

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
	s.InjectKey(tcell.KeyCtrlS, 0, tcell.ModNone) // clear modified so quit is allowed
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

// TestBrowserRendersSideBySide confirms the sidebar and editor draw together:
// the file entry appears in the left panel and the editor statusbar in the
// region to the right of the sidebar.
func TestBrowserRendersSideBySide(t *testing.T) {
	dir := t.TempDir()
	start := filepath.Join(dir, "start.txt")
	if err := os.WriteFile(start, []byte("package main\n"), 0644); err != nil {
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
	e.handleKey(keyEvent(tcell.KeyCtrlB))
	e.draw()

	cells, width, height := s.GetContents()
	rowText := func(y, x0, x1 int) string {
		var out []rune
		for x := x0; x < x1; x++ {
			r := cells[y*width+x].Runes
			if len(r) == 1 {
				out = append(out, r[0])
			} else {
				out = append(out, ' ')
			}
		}
		return string(out)
	}

	// Left panel lists the file somewhere.
	leftHas := false
	for y := 0; y < height; y++ {
		if strContains(rowText(y, 0, render.SidebarWidth-1), "start.txt") {
			leftHas = true
			break
		}
	}
	if !leftHas {
		t.Fatal("sidebar (left region) should list start.txt")
	}

	// Editor statusbar renders in the region right of the sidebar (cursor indicator
	// is always present regardless of how long the path is).
	if !strContains(rowText(0, render.SidebarWidth, width), "Ln 1, Col 1") {
		t.Fatalf("editor statusbar not drawn in right region: %q", rowText(0, render.SidebarWidth, width))
	}
}

func strContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func keyEvent(k tcell.Key, ch ...rune) *tcell.EventKey {
	r := rune(0)
	if len(ch) > 0 {
		r = ch[0]
	}
	return tcell.NewEventKey(k, r, tcell.ModNone)
}

func TestSplashShownUntilFirstKey(t *testing.T) {
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(80, 24)

	e := New(s, buffer.New(nil), "") // no filename

	hasBlocks := func() bool {
		cells, width, height := s.GetContents()
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				r := cells[y*width+x].Runes
				if len(r) == 1 && r[0] == '█' {
					return true
				}
			}
		}
		return false
	}

	e.draw()
	if !hasBlocks() {
		t.Fatal("splash banner should show before any typing")
	}

	e.handleKey(keyEvent(tcell.KeyRune, 'h')) // fills buffer, sets modified
	e.draw()
	if hasBlocks() {
		t.Fatal("splash should disappear once typing begins")
	}
	if e.b.Lines()[0] != "h" {
		t.Fatalf("buffer should hold typed text, got %q", e.b.Lines()[0])
	}
}

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

// TestRunBackspaceJoinsLines verifies Backspace at column 0 joins lines.
func TestRunBackspaceJoinsLines(t *testing.T) {
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatalf("init sim screen: %v", err)
	}
	defer s.Fini()
	s.SetSize(80, 24)

	b := buffer.New([]string{"ab", "cd"})
	e := New(s, b, filepath.Join(t.TempDir(), "x.txt"))

	// Move down to row 1 col 0, then backspace to join.
	s.InjectKey(tcell.KeyDown, 0, tcell.ModNone)
	s.InjectKey(tcell.KeyBackspace2, 0, tcell.ModNone)
	s.InjectKey(tcell.KeyCtrlS, 0, tcell.ModNone) // clear modified so quit is allowed
	s.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone)

	e.Run()

	if lines := b.Lines(); len(lines) != 1 || lines[0] != "abcd" {
		t.Fatalf("lines = %#v, want [abcd]", b.Lines())
	}
}
