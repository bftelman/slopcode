package editor

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/buffer"
	"github.com/bftelman/slopcode/internal/completion"
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

	// Buffer should also reflect the edits and be unmodified after save.
	lines, _ := fileio.Load(path)
	if len(lines) != 2 || lines[0] != "hi" || lines[1] != "there" {
		t.Fatalf("reloaded lines = %#v", lines)
	}
	if e.isModified() {
		t.Fatalf("expected unmodified after save")
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

func TestEmptyingUnnamedBufferBackToOriginalIsUnmodified(t *testing.T) {
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(80, 24)

	e := New(s, buffer.New(nil), "") // pristine empty, unnamed
	if e.isModified() {
		t.Fatal("fresh buffer should be unmodified")
	}
	e.handleKey(keyEvent(tcell.KeyRune, 'a'))
	e.handleKey(keyEvent(tcell.KeyRune, 'b'))
	if !e.isModified() {
		t.Fatal("after typing should be modified")
	}
	e.handleKey(keyEvent(tcell.KeyBackspace2))
	e.handleKey(keyEvent(tcell.KeyBackspace2))
	if e.isModified() {
		t.Fatalf("emptied back to original should be UNmodified, got lines %#v", e.b.Lines())
	}
}

func TestRevertingLoadedFileToOriginalIsUnmodified(t *testing.T) {
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(80, 24)

	e := New(s, buffer.New([]string{"hi"}), filepath.Join(t.TempDir(), "x.txt"))
	if e.isModified() {
		t.Fatal("freshly loaded file should be unmodified")
	}
	e.handleKey(keyEvent(tcell.KeyEnd))
	e.handleKey(keyEvent(tcell.KeyRune, 'x')) // "hix"
	if !e.isModified() {
		t.Fatal("after edit should be modified")
	}
	e.handleKey(keyEvent(tcell.KeyBackspace2)) // back to "hi"
	if e.isModified() {
		t.Fatalf("reverted to original should be UNmodified, got %#v", e.b.Lines())
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

// recordingProvider tracks DocSink calls (keyed by URI) so a file-switch
// test can assert the completion engine tells the server the old document
// closed and the new one opened.
type recordingProvider struct {
	mu     sync.Mutex
	opened []string
	closed []string
}

func (p *recordingProvider) Complete(context.Context, completion.Document, completion.Position) ([]completion.Item, error) {
	return nil, nil
}
func (p *recordingProvider) Close() error { return nil }
func (p *recordingProvider) DidOpen(doc completion.Document) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.opened = append(p.opened, doc.URI)
	return nil
}
func (p *recordingProvider) DidChange(completion.Document) error { return nil }
func (p *recordingProvider) DidClose(uri string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = append(p.closed, uri)
	return nil
}

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestBrowserSwitchSyncsCompletionEngine reproduces the bug the final review
// flagged: switching files via the file browser swapped e.b/e.path but never
// told the completion engine, so the provider kept seeing didChange for a
// document it never opened and never learned the old one closed — silently
// breaking completion (or worse, feeding edits to the wrong document) after
// every Ctrl+B file switch.
func TestBrowserSwitchSyncsCompletionEngine(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "other.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	start := filepath.Join(dir, "start.go")
	if err := os.WriteFile(start, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	prov := &recordingProvider{}
	reg := completion.Registry{Factory: func(ext, root string) (completion.Provider, error) {
		return prov, nil
	}}
	eng := completion.New(reg)

	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(80, 24)

	lines, _ := fileio.Load(start)
	e := NewWithEngine(s, buffer.New(lines), start, eng)

	e.handleKey(keyEvent(tcell.KeyCtrlB))
	for e.browser.Selected().Name != "other.go" {
		e.handleKey(keyEvent(tcell.KeyDown))
	}
	e.handleKey(keyEvent(tcell.KeyEnter))

	absStart, _ := filepath.Abs(start)
	absTarget, _ := filepath.Abs(target)
	startURI := completion.PathToFileURI(absStart)
	targetURI := completion.PathToFileURI(absTarget)

	waitFor(t, func() bool {
		prov.mu.Lock()
		defer prov.mu.Unlock()
		return containsStr(prov.opened, targetURI) && containsStr(prov.closed, startURI)
	})
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

// stubProvider returns canned items for editor tests.
type stubProvider struct{ items []completion.Item }

func (p *stubProvider) Complete(ctx context.Context, _ completion.Document, _ completion.Position) ([]completion.Item, error) {
	return p.items, nil
}
func (p *stubProvider) Close() error { return nil }

// closeTrackingProvider records whether Close was called, so the quit path
// test can assert the engine actually shuts its providers down.
type closeTrackingProvider struct{ closed bool }

func (p *closeTrackingProvider) Complete(context.Context, completion.Document, completion.Position) ([]completion.Item, error) {
	return nil, nil
}
func (p *closeTrackingProvider) Close() error {
	p.closed = true
	return nil
}

// TestQuitClosesCompletionEngine confirms the confirmed-quit path
// (tryQuit's true branch) shuts down the completion engine, which in turn
// closes every provider it opened — this is what prevents a real gopls
// subprocess from being orphaned on quit.
func TestQuitClosesCompletionEngine(t *testing.T) {
	prov := &closeTrackingProvider{}
	reg := completion.Registry{Factory: func(ext, root string) (completion.Provider, error) {
		return prov, nil
	}}
	eng := completion.New(reg)

	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(80, 24)

	e := NewWithEngine(s, buffer.New(nil), "main.go", eng)

	if !e.tryQuit() {
		t.Fatal("expected tryQuit to succeed on an unmodified buffer")
	}
	if !prov.closed {
		t.Fatal("expected quitting to call eng.Close(), which closes the provider")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for !cond() {
		select {
		case <-deadline:
			t.Fatal("condition not met in time")
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
}

func TestCompletionPopupOpensAndAccepts(t *testing.T) {
	items := []completion.Item{{Label: "Println", Insert: "Println"}}
	reg := completion.Registry{Factory: func(ext, root string) (completion.Provider, error) {
		return &stubProvider{items: items}, nil
	}}
	eng := completion.New(reg, completion.WithDebounce(5*time.Millisecond))

	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.SetSize(80, 24)
	b := buffer.New([]string{"Pri"})
	b.MoveEnd()
	e := NewWithEngine(s, b, "main.go", eng)

	// Simulate typing a trigger and receiving a result via the event loop.
	go e.Run()
	s.InjectKey(tcell.KeyRune, 'n', tcell.ModNone) // now "Prin"
	// Allow debounce + bridge to deliver and the loop to render.
	waitFor(t, func() bool { return e.popupOpenForTest() })

	// Accept with Enter.
	s.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, func() bool { return !e.popupOpenForTest() })

	if got := b.Lines()[0]; got != "Println" {
		t.Fatalf("line = %q, want Println", got)
	}
	s.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone)
}

// TestCursorMovementDismissesPopupPreventingCorruption reproduces the bug
// the final review flagged: leaving the popup open across a cursor move and
// then accepting used the *new* cursor position with the *old* popup's
// items, corrupting the buffer ("Pri" + Left + Enter used to yield
// "Printlni" instead of just moving left and inserting a newline).
func TestCursorMovementDismissesPopupPreventingCorruption(t *testing.T) {
	items := []completion.Item{{Label: "Println", Insert: "Println"}}
	reg := completion.Registry{Factory: func(ext, root string) (completion.Provider, error) {
		return &stubProvider{items: items}, nil
	}}
	eng := completion.New(reg, completion.WithDebounce(5*time.Millisecond))

	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.SetSize(80, 24)
	b := buffer.New([]string{"Pri"})
	b.MoveEnd()
	e := NewWithEngine(s, b, filepath.Join(t.TempDir(), "main.go"), eng)

	done := make(chan struct{})
	go func() { e.Run(); close(done) }()
	s.InjectKey(tcell.KeyRune, 'n', tcell.ModNone) // now "Prin"
	waitFor(t, func() bool { return e.popupOpenForTest() })

	s.InjectKey(tcell.KeyLeft, 0, tcell.ModNone)
	waitFor(t, func() bool { return !e.popupOpenForTest() })

	s.InjectKey(tcell.KeyEnter, 0, tcell.ModNone) // must be a plain newline, not an accept
	s.InjectKey(tcell.KeyCtrlS, 0, tcell.ModNone) // clear modified so Ctrl+Q is allowed to quit
	s.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after Ctrl+Q")
	}
	// Run() has returned (a real synchronization point via the closed
	// channel), so reading b directly here is race-safe.
	if got := b.Lines()[0] + "|" + b.Lines()[1]; got != "Pri|n" {
		t.Fatalf("lines = %q, want split \"Pri\"/\"n\" (no corruption from a stale popup)", got)
	}
}

// TestOpenBrowserDismissesPopup reproduces the bug the final review flagged:
// Ctrl+B while the popup was open left it stuck on screen, overlapping the
// sidebar, with no key able to close it.
func TestOpenBrowserDismissesPopup(t *testing.T) {
	items := []completion.Item{{Label: "Println", Insert: "Println"}}
	reg := completion.Registry{Factory: func(ext, root string) (completion.Provider, error) {
		return &stubProvider{items: items}, nil
	}}
	eng := completion.New(reg, completion.WithDebounce(5*time.Millisecond))

	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.SetSize(80, 24)
	b := buffer.New([]string{"Pri"})
	b.MoveEnd()
	e := NewWithEngine(s, b, filepath.Join(t.TempDir(), "main.go"), eng)

	go e.Run()
	s.InjectKey(tcell.KeyRune, 'n', tcell.ModNone)
	waitFor(t, func() bool { return e.popupOpenForTest() })

	s.InjectKey(tcell.KeyCtrlB, 0, tcell.ModNone)
	waitFor(t, func() bool { return !e.popupOpenForTest() })

	s.InjectKey(tcell.KeyCtrlB, 0, tcell.ModNone) // close the browser
	s.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone)
}

// TestCompletionPopupAnchorsAtScreenColumnNotByteColumn reproduces the bug
// the final review flagged: the popup anchored at the cursor's byte column,
// which diverges from its screen column (what render.Draw actually uses to
// place the cursor) once a tab is involved, misplacing the popup on any
// tab-indented line.
func TestCompletionPopupAnchorsAtScreenColumnNotByteColumn(t *testing.T) {
	items := []completion.Item{{Label: "Println", Insert: "Println"}}
	reg := completion.Registry{Factory: func(ext, root string) (completion.Provider, error) {
		return &stubProvider{items: items}, nil
	}}
	eng := completion.New(reg, completion.WithDebounce(5*time.Millisecond))

	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.SetSize(80, 24)
	b := buffer.New([]string{"\tPri"}) // leading tab expands wider than 1 byte-column
	b.MoveEnd()
	e := NewWithEngine(s, b, "main.go", eng)

	go e.Run()
	s.InjectKey(tcell.KeyRune, 'n', tcell.ModNone) // "\tPrin", byte col 5
	waitFor(t, func() bool { return e.popupOpenForTest() })

	x, _ := e.popupAnchorForTest()
	gw := render.GutterWidth(b.LineCount())
	byteCol := gw + 5                 // what the old, wrong code produced
	wantX := gw + render.TabWidth + 4 // tab expands to TabWidth, then "Prin" is 4 more columns
	if x != wantX {
		t.Fatalf("Anchor.X = %d, want %d (screen column; byte column would wrongly be %d)", x, wantX, byteCol)
	}
	s.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone)
}

func TestCompletionDropsStaleVersion(t *testing.T) {
	eng := completion.New(completion.Registry{}, completion.WithDebounce(time.Millisecond))
	s := tcell.NewSimulationScreen("")
	_ = s.Init()
	s.SetSize(80, 24)
	e := NewWithEngine(s, buffer.New([]string{""}), "main.go", eng)
	e.docVersion = 5
	e.applyResult(completion.Result{Version: 2, Items: []completion.Item{{Label: "old"}}})
	if e.popupOpenForTest() {
		t.Fatal("stale result must not open the popup")
	}
}

func TestCompletionMissingServerIsNonFatal(t *testing.T) {
	// Registry that always fails to produce a provider.
	reg := completion.Registry{Factory: func(ext, root string) (completion.Provider, error) {
		return nil, nil
	}}
	eng := completion.New(reg, completion.WithDebounce(time.Millisecond))
	s := tcell.NewSimulationScreen("")
	_ = s.Init()
	s.SetSize(80, 24)
	b := buffer.New([]string{"a"})
	b.MoveEnd()
	e := NewWithEngine(s, b, "main.go", eng)
	go e.Run()
	s.InjectKey(tcell.KeyRune, 'b', tcell.ModNone)
	time.Sleep(50 * time.Millisecond)
	// Editor still responsive and no popup.
	if e.popupOpenForTest() {
		t.Fatal("no provider -> no popup")
	}
	s.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone)
}
