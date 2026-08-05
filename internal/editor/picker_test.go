package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/buffer"
	"github.com/bftelman/slopcode/internal/completion"
	"github.com/bftelman/slopcode/internal/fileio"
	"github.com/bftelman/slopcode/internal/picker"
)

// waitPickRows pumps picker events into the editor until the overlay has rows,
// mirroring what Run() does with them. The engine ranks on its own goroutine, so
// a test must drain the bridge rather than assume synchronous results.
func waitPickRows(t *testing.T, e *Editor, want func(*pickState) bool) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if e.pick != nil && want(e.pick) {
			return
		}
		select {
		case <-deadline:
			got := 0
			if e.pick != nil {
				got = len(e.pick.rows)
			}
			t.Fatalf("timed out waiting for picker rows (have %d)", got)
		default:
		}
		ev := e.s.PollEvent()
		if pe, ok := ev.(*pickerEvent); ok {
			e.applyPickResult(pe.res)
		}
	}
}

// newPickerEditor builds an editor rooted in a temp dir holding files.
func newPickerEditor(t *testing.T, files map[string]string, open string) *Editor {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatalf("init sim screen: %v", err)
	}
	t.Cleanup(s.Fini)
	s.SetSize(80, 24)

	openPath := filepath.Join(root, filepath.FromSlash(open))
	lines := []string{""}
	if content, ok := files[open]; ok && content != "" {
		lines = []string{content}
	}
	return New(s, buffer.New(lines), openPath)
}

func TestLinePickerJumpsToLine(t *testing.T) {
	e := newPickerEditor(t, map[string]string{"t.txt": "x"}, "t.txt")
	e.b = buffer.New([]string{"alpha", "beta", "gamma target", "delta"})

	press(e, tcell.KeyCtrlG)
	if e.pick == nil {
		t.Fatal("Ctrl+G should open the line picker")
	}
	waitPickRows(t, e, func(p *pickState) bool { return len(p.rows) > 0 })

	typeText(e, "target")
	waitPickRows(t, e, func(p *pickState) bool {
		return len(p.rows) > 0 && p.rows[0].Cand.Row == 2
	})

	press(e, tcell.KeyEnter)
	if e.pick != nil {
		t.Error("Enter should close the picker")
	}
	if row, _ := e.b.Cursor(); row != 2 {
		t.Errorf("cursor on row %d, want 2", row)
	}
}

func TestLinePickerEscapeLeavesCursor(t *testing.T) {
	e := newPickerEditor(t, map[string]string{"t.txt": "x"}, "t.txt")
	e.b = buffer.New([]string{"alpha", "beta", "gamma"})
	e.b.SetCursor(1, 2)

	press(e, tcell.KeyCtrlG)
	waitPickRows(t, e, func(p *pickState) bool { return len(p.rows) > 0 })
	press(e, tcell.KeyEscape)

	if e.pick != nil {
		t.Error("Esc should close the picker")
	}
	if row, col := e.b.Cursor(); row != 1 || col != 2 {
		t.Errorf("cursor at (%d,%d), want (1,2) - Esc must not move it", row, col)
	}
}

func TestFilePickerOpensSelectedFile(t *testing.T) {
	e := newPickerEditor(t, map[string]string{
		"start.txt":            "start content",
		"deep/nested/other.md": "other content",
	}, "start.txt")

	press(e, tcell.KeyCtrlP)
	if e.pick == nil {
		t.Fatal("Ctrl+P should open the file picker")
	}
	waitPickRows(t, e, func(p *pickState) bool { return len(p.rows) > 0 })

	typeText(e, "other")
	waitPickRows(t, e, func(p *pickState) bool {
		return len(p.rows) > 0 && filepath.Base(p.rows[0].Cand.Path) == "other.md"
	})

	press(e, tcell.KeyEnter)
	if e.pick != nil {
		t.Error("Enter should close the picker")
	}
	if filepath.Base(e.path) != "other.md" {
		t.Errorf("opened %q, want other.md", e.path)
	}
	if got := e.b.Lines()[0]; got != "other content" {
		t.Errorf("buffer holds %q, want the opened file's content", got)
	}
}

// Opening through the picker must go through openPath, so the completion engine
// gets didClose for the old URI and didOpen for the new one.
func TestFilePickerSyncsCompletionDocuments(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) string {
		p := filepath.Join(root, rel)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	start := write("start.go", "package main\n")
	target := write("other.go", "package other\n")

	prov := &recordingProvider{}
	reg := completion.Registry{Factory: func(ext, root string) (completion.Provider, error) {
		return prov, nil
	}}
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Fini)
	s.SetSize(80, 24)
	lines, _ := fileio.Load(start)
	e := NewWithEngine(s, buffer.New(lines), start, completion.New(reg))

	press(e, tcell.KeyCtrlP)
	waitPickRows(t, e, func(p *pickState) bool { return len(p.rows) > 0 })
	typeText(e, "other")
	waitPickRows(t, e, func(p *pickState) bool {
		return len(p.rows) > 0 && filepath.Base(p.rows[0].Cand.Path) == "other.go"
	})
	press(e, tcell.KeyEnter)

	if filepath.Base(e.path) != "other.go" {
		t.Fatalf("opened %q, want other.go", e.path)
	}

	absStart, _ := filepath.Abs(start)
	absTarget, _ := filepath.Abs(target)
	startURI := completion.PathToFileURI(absStart)
	targetURI := completion.PathToFileURI(absTarget)

	// The engine's owner goroutine drains commands asynchronously.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		prov.mu.Lock()
		ok := containsStr(prov.closed, startURI) && containsStr(prov.opened, targetURI)
		prov.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	prov.mu.Lock()
	defer prov.mu.Unlock()
	t.Errorf("engine never saw didClose(%s)+didOpen(%s)\nclosed=%v\nopened=%v",
		startURI, targetURI, prov.closed, prov.opened)
}

// An unsaved buffer is protected by the same two-step latch the browser uses.
func TestFilePickerGuardsUnsavedChanges(t *testing.T) {
	e := newPickerEditor(t, map[string]string{
		"start.txt": "start content",
		"other.txt": "other content",
	}, "start.txt")

	typeText(e, "dirty") // modify the buffer
	if !e.isModified() {
		t.Fatal("expected the buffer to be modified")
	}

	press(e, tcell.KeyCtrlP)
	waitPickRows(t, e, func(p *pickState) bool { return len(p.rows) > 0 })
	typeText(e, "other")
	waitPickRows(t, e, func(p *pickState) bool {
		return len(p.rows) > 0 && filepath.Base(p.rows[0].Cand.Path) == "other.txt"
	})

	press(e, tcell.KeyEnter) // first Enter: guard
	if e.pick == nil {
		t.Fatal("the guard should keep the picker open")
	}
	if filepath.Base(e.path) == "other.txt" {
		t.Fatal("first Enter must not discard unsaved changes")
	}

	press(e, tcell.KeyEnter) // second Enter: confirm
	if filepath.Base(e.path) != "other.txt" {
		t.Errorf("second Enter should open the file, got %q", e.path)
	}
}

func TestPickerSelectionMovesAndClamps(t *testing.T) {
	e := newPickerEditor(t, map[string]string{"t.txt": "x"}, "t.txt")
	e.b = buffer.New([]string{"aa", "ab", "ac", "ad"})

	press(e, tcell.KeyCtrlG)
	waitPickRows(t, e, func(p *pickState) bool { return len(p.rows) == 4 })

	if e.pick.sel != 0 {
		t.Fatalf("initial sel = %d, want 0", e.pick.sel)
	}
	press(e, tcell.KeyCtrlP) // already at the top
	if e.pick.sel != 0 {
		t.Errorf("sel = %d after Ctrl+P at the top, want 0 (clamped)", e.pick.sel)
	}
	press(e, tcell.KeyDown, tcell.KeyDown)
	if e.pick.sel != 2 {
		t.Errorf("sel = %d after two Down, want 2", e.pick.sel)
	}
	for i := 0; i < 10; i++ {
		press(e, tcell.KeyCtrlN)
	}
	if e.pick.sel != 3 {
		t.Errorf("sel = %d past the end, want 3 (clamped)", e.pick.sel)
	}
}

// Editing the query resets the selection: the old row is about to be replaced.
func TestPickerQueryResetsSelection(t *testing.T) {
	e := newPickerEditor(t, map[string]string{"t.txt": "x"}, "t.txt")
	e.b = buffer.New([]string{"aa", "ab", "ac"})

	press(e, tcell.KeyCtrlG)
	waitPickRows(t, e, func(p *pickState) bool { return len(p.rows) == 3 })
	press(e, tcell.KeyDown, tcell.KeyDown)
	if e.pick.sel == 0 {
		t.Fatal("expected the selection to have moved")
	}

	typeText(e, "a")
	if e.pick.sel != 0 {
		t.Errorf("sel = %d after typing, want 0", e.pick.sel)
	}
}

func TestPickerBackspaceEditsQuery(t *testing.T) {
	e := newPickerEditor(t, map[string]string{"t.txt": "x"}, "t.txt")
	e.b = buffer.New([]string{"alpha", "beta"})

	press(e, tcell.KeyCtrlG)
	waitPickRows(t, e, func(p *pickState) bool { return len(p.rows) == 2 })

	typeText(e, "alp")
	if e.pick.query != "alp" {
		t.Fatalf("query = %q, want %q", e.pick.query, "alp")
	}
	press(e, tcell.KeyBackspace)
	if e.pick.query != "al" {
		t.Errorf("query = %q after backspace, want %q", e.pick.query, "al")
	}
	// Backspace on an empty query must not underflow.
	press(e, tcell.KeyBackspace, tcell.KeyBackspace, tcell.KeyBackspace)
	if e.pick.query != "" {
		t.Errorf("query = %q, want empty", e.pick.query)
	}
}

// The picker swallows keys that would otherwise edit the buffer.
func TestPickerDoesNotEditBuffer(t *testing.T) {
	e := newPickerEditor(t, map[string]string{"t.txt": "x"}, "t.txt")
	e.b = buffer.New([]string{"untouched"})

	press(e, tcell.KeyCtrlG)
	waitPickRows(t, e, func(p *pickState) bool { return len(p.rows) > 0 })
	typeText(e, "zzz")

	if got := e.b.Lines()[0]; got != "untouched" {
		t.Errorf("picker typing edited the buffer: %q", got)
	}
}

// A stale result - one tagged with a superseded generation - must be dropped.
func TestPickerDropsStaleGeneration(t *testing.T) {
	e := newPickerEditor(t, map[string]string{"t.txt": "x"}, "t.txt")
	e.b = buffer.New([]string{"aa", "bb"})

	press(e, tcell.KeyCtrlG)
	waitPickRows(t, e, func(p *pickState) bool { return len(p.rows) == 2 })
	current := e.pick.gen

	e.applyPickResult(picker.Result{
		Gen:  current - 1,
		Rows: []picker.Row{{Cand: picker.Candidate{Text: "GHOST"}}},
	})
	for _, r := range e.pick.rows {
		if r.Cand.Text == "GHOST" {
			t.Fatal("a stale-generation result was applied")
		}
	}

	// The matching generation is accepted.
	e.applyPickResult(picker.Result{
		Gen:  current,
		Rows: []picker.Row{{Cand: picker.Candidate{Text: "FRESH"}}},
	})
	if len(e.pick.rows) != 1 || e.pick.rows[0].Cand.Text != "FRESH" {
		t.Errorf("current-generation result was not applied: %+v", e.pick.rows)
	}
}

// Opening a picker dismisses the browser rather than stacking two overlays.
func TestPickerClosesBrowser(t *testing.T) {
	e := newPickerEditor(t, map[string]string{"t.txt": "x", "u.txt": "y"}, "t.txt")

	press(e, tcell.KeyCtrlB)
	if e.browser == nil {
		t.Fatal("Ctrl+B should open the browser")
	}
	// Ctrl+B is consumed by the browser, so close it first, then open the picker.
	press(e, tcell.KeyEscape, tcell.KeyCtrlG)
	if e.browser != nil {
		t.Error("browser should be closed")
	}
	if e.pick == nil {
		t.Error("picker should be open")
	}
}

// A picker error is surfaced without breaking the editor.
func TestPickerSurfacesListingError(t *testing.T) {
	e := newPickerEditor(t, map[string]string{"t.txt": "x"}, "t.txt")
	e.b = buffer.New([]string{""})

	e.pickGen++
	e.pick = &pickState{title: "T", gen: e.pickGen}
	e.applyPickResult(picker.Result{Gen: e.pickGen, Err: os.ErrPermission})

	if e.pick.err == nil {
		t.Error("expected the error to be recorded on the picker state")
	}
	press(e, tcell.KeyEscape)
	if e.pick != nil {
		t.Error("Esc should still close an errored picker")
	}
	// Editing still works afterwards.
	typeText(e, "ok")
	if got := e.b.Lines()[0]; got != "ok" {
		t.Errorf("editing broken after a picker error: %q", got)
	}
}

func TestPickerRootPrefersGitRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(deep, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Fini)
	s.SetSize(80, 24)
	e := New(s, buffer.New([]string{"x"}), file)

	gotAbs, _ := filepath.Abs(e.pickerRoot())
	wantAbs, _ := filepath.Abs(root)
	if gotAbs != wantAbs {
		t.Errorf("pickerRoot = %q, want the git root %q", gotAbs, wantAbs)
	}
}

// The overlays must actually reach the screen through the editor's own draw
// path, not just exist as state. This is what would catch a panic or a missing
// DrawPicker/DrawFindBar call in draw().
func TestDrawRendersOverlays(t *testing.T) {
	screenHas := func(t *testing.T, s tcell.SimulationScreen, want string) bool {
		t.Helper()
		cells, width, _ := s.GetContents()
		height := len(cells) / width
		for y := 0; y < height; y++ {
			var row []rune
			for x := 0; x < width; x++ {
				c := cells[y*width+x]
				if len(c.Runes) > 0 {
					row = append(row, c.Runes[0])
				} else {
					row = append(row, ' ')
				}
			}
			if strings.Contains(string(row), want) {
				return true
			}
		}
		return false
	}

	t.Run("picker overlay", func(t *testing.T) {
		e := newPickerEditor(t, map[string]string{"t.txt": "x"}, "t.txt")
		e.b = buffer.New([]string{"alpha", "beta needle", "gamma"})

		press(e, tcell.KeyCtrlG)
		waitPickRows(t, e, func(p *pickState) bool { return len(p.rows) == 3 })
		typeText(e, "needle")
		waitPickRows(t, e, func(p *pickState) bool {
			return len(p.rows) == 1 && p.rows[0].Cand.Row == 1
		})

		e.draw()
		s := e.s.(tcell.SimulationScreen)
		if !screenHas(t, s, "needle") {
			t.Error("picker overlay row not drawn")
		}
		if !screenHas(t, s, "> needle") {
			t.Error("picker query line not drawn")
		}
	})

	t.Run("find bar", func(t *testing.T) {
		e := newPickerEditor(t, map[string]string{"t.txt": "x"}, "t.txt")
		e.b = buffer.New([]string{"foo bar foo"})

		openFindFor(e, "foo")
		e.draw()

		s := e.s.(tcell.SimulationScreen)
		if !screenHas(t, s, "Find: foo") {
			t.Error("find bar not drawn")
		}
		if !screenHas(t, s, "[1/2]") {
			t.Error("find bar counter not drawn")
		}
	})

	t.Run("picker over splash-eligible empty buffer", func(t *testing.T) {
		// draw() short-circuits to the splash for an unnamed, unmodified buffer.
		// A picker opened there must still render rather than being swallowed.
		s := tcell.NewSimulationScreen("")
		if err := s.Init(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Fini)
		s.SetSize(80, 24)
		e := New(s, buffer.New(nil), "")

		press(e, tcell.KeyCtrlG)
		e.draw()
		if e.pick == nil {
			t.Fatal("picker should be open")
		}
		if !screenHas(t, s, "> ") {
			t.Error("picker not drawn over the splash screen")
		}
	})
}
