package editor

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/buffer"
)

// newFindEditor builds an editor over lines on a ".txt" path, so the completion
// registry never routes to gopls and these tests do not depend on it.
func newFindEditor(t *testing.T, lines []string) *Editor {
	t.Helper()
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatalf("init sim screen: %v", err)
	}
	t.Cleanup(s.Fini)
	s.SetSize(80, 24)
	return New(s, buffer.New(lines), filepath.Join(t.TempDir(), "t.txt"))
}

// press sends each key in order.
func press(e *Editor, keys ...tcell.Key) {
	for _, k := range keys {
		e.handleKey(keyEvent(k))
	}
}

// typeText sends each rune of text as a key event.
func typeText(e *Editor, text string) {
	for _, r := range text {
		e.handleKey(keyEvent(tcell.KeyRune, r))
	}
}

// openFindFor opens the bar and types query into it.
func openFindFor(e *Editor, query string) {
	press(e, tcell.KeyCtrlF)
	typeText(e, query)
}

func TestFindOpensAndSelectsNearestMatch(t *testing.T) {
	e := newFindEditor(t, []string{"alpha foo", "beta", "gamma foo"})

	openFindFor(e, "foo")

	if e.find == nil {
		t.Fatal("find bar should be open")
	}
	if got := len(e.find.matches); got != 2 {
		t.Fatalf("got %d matches, want 2", got)
	}
	if e.find.cur != 0 {
		t.Errorf("selected match %d, want 0 (nearest to the opening cursor)", e.find.cur)
	}
	if row, col := e.b.Cursor(); row != 0 || col != 6 {
		t.Errorf("cursor at (%d,%d), want (0,6) - on the first match", row, col)
	}
}

// Opening on top of a match must select that match, not skip past it. This is
// why NearestFrom and NextFrom are separate functions.
func TestFindSelectsMatchUnderCursor(t *testing.T) {
	e := newFindEditor(t, []string{"foo bar foo"})
	e.b.SetCursor(0, 0)

	openFindFor(e, "foo")

	if e.find.cur != 0 {
		t.Errorf("cur = %d, want 0 - the match the cursor already sat on", e.find.cur)
	}
}

func TestFindStepsAndWraps(t *testing.T) {
	e := newFindEditor(t, []string{"foo", "foo", "foo"})
	openFindFor(e, "foo")

	press(e, tcell.KeyCtrlN, tcell.KeyCtrlN)
	if e.find.cur != 2 {
		t.Fatalf("after two Ctrl+N: cur = %d, want 2", e.find.cur)
	}
	press(e, tcell.KeyCtrlN)
	if e.find.cur != 0 {
		t.Fatalf("Ctrl+N past the end: cur = %d, want 0 (wrapped)", e.find.cur)
	}
	press(e, tcell.KeyCtrlP)
	if e.find.cur != 2 {
		t.Fatalf("Ctrl+P before the start: cur = %d, want 2 (wrapped)", e.find.cur)
	}
}

func TestFindEscapeRestoresCursor(t *testing.T) {
	e := newFindEditor(t, []string{"aaa", "bbb foo"})
	e.b.SetCursor(0, 1)

	openFindFor(e, "foo")
	if row, _ := e.b.Cursor(); row != 1 {
		t.Fatalf("search should have moved the cursor to row 1, got row %d", row)
	}
	press(e, tcell.KeyEscape)

	if e.find != nil {
		t.Error("Esc should close the find bar")
	}
	if row, col := e.b.Cursor(); row != 0 || col != 1 {
		t.Errorf("after Esc cursor at (%d,%d), want (0,1)", row, col)
	}
}

func TestFindEnterKeepsCursorOnMatch(t *testing.T) {
	e := newFindEditor(t, []string{"aaa", "bbb foo"})
	e.b.SetCursor(0, 1)

	openFindFor(e, "foo")
	press(e, tcell.KeyEnter)

	if e.find != nil {
		t.Error("Enter should close the find bar")
	}
	if row, col := e.b.Cursor(); row != 1 || col != 4 {
		t.Errorf("after Enter cursor at (%d,%d), want (1,4) - on the match", row, col)
	}
}

func TestFindBackspaceEditsQuery(t *testing.T) {
	e := newFindEditor(t, []string{"foo food"})

	openFindFor(e, "food")
	if got := len(e.find.matches); got != 1 {
		t.Fatalf("query %q: got %d matches, want 1", e.find.query, got)
	}

	press(e, tcell.KeyBackspace)
	if e.find.query != "foo" {
		t.Fatalf("after backspace query = %q, want %q", e.find.query, "foo")
	}
	if got := len(e.find.matches); got != 2 {
		t.Fatalf("query %q: got %d matches, want 2", e.find.query, got)
	}
}

func TestFindSmartCaseEndToEnd(t *testing.T) {
	e := newFindEditor(t, []string{"Foo foo FOO"})

	openFindFor(e, "foo") // all lowercase -> case-insensitive
	if got := len(e.find.matches); got != 3 {
		t.Errorf("lowercase query: got %d matches, want 3", got)
	}

	typeText(e, "!") // "foo!" matches nothing
	press(e, tcell.KeyBackspace)
	press(e, tcell.KeyBackspace) // -> "fo"
	typeText(e, "O")             // -> "foO", now case-sensitive
	if got := len(e.find.matches); got != 0 {
		t.Errorf("mixed-case query %q: got %d matches, want 0", e.find.query, got)
	}
}

func TestReplaceCurrentAdvances(t *testing.T) {
	e := newFindEditor(t, []string{"foo bar foo"})

	openFindFor(e, "foo")
	press(e, tcell.KeyTab) // reveal the replace field
	typeText(e, "qux")
	press(e, tcell.KeyCtrlR)

	if got, want := e.b.Lines()[0], "qux bar foo"; got != want {
		t.Fatalf("after one Ctrl+R: %q, want %q", got, want)
	}
	if got := len(e.find.matches); got != 1 {
		t.Fatalf("got %d remaining matches, want 1", got)
	}
	if e.find.cur != 0 {
		t.Errorf("cur = %d, want 0 (the one remaining match)", e.find.cur)
	}
}

// A replacement that contains the query must not be rediscovered and replaced
// again - the cursor lands past the inserted text.
func TestReplaceCurrentDoesNotReplaceItsOwnOutput(t *testing.T) {
	e := newFindEditor(t, []string{"foo"})

	openFindFor(e, "foo")
	press(e, tcell.KeyTab)
	typeText(e, "foofoo")
	press(e, tcell.KeyCtrlR)

	if got, want := e.b.Lines()[0], "foofoo"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// The sweep does not wrap, so there is nothing left to replace even though
	// the line now contains two occurrences behind the cursor.
	if e.find.cur != -1 {
		t.Errorf("cur = %d, want -1: the sweep must not wrap onto its own output", e.find.cur)
	}
}

// Holding Ctrl+R with a replacement that contains the query must terminate.
// Wrapping the post-replace selection would grow the line without end.
func TestRepeatedReplaceTerminates(t *testing.T) {
	e := newFindEditor(t, []string{"foo bar"})

	openFindFor(e, "foo")
	press(e, tcell.KeyTab)
	typeText(e, "foofoo")
	for i := 0; i < 20; i++ {
		press(e, tcell.KeyCtrlR)
	}

	if got, want := e.b.Lines()[0], "foofoo bar"; got != want {
		t.Errorf("after 20 Ctrl+R: %q, want %q - one replacement then no-ops", got, want)
	}
}

func TestReplaceCurrentNeedsReplaceField(t *testing.T) {
	e := newFindEditor(t, []string{"foo"})

	openFindFor(e, "foo")
	press(e, tcell.KeyCtrlR) // replace field not revealed

	if got := e.b.Lines()[0]; got != "foo" {
		t.Errorf("buffer changed without a revealed replace field: %q", got)
	}
	if !strings.Contains(e.notice, "Tab") {
		t.Errorf("expected a hint about Tab, got notice %q", e.notice)
	}
}

// The core guarantee: replace-all is a single undo step.
func TestReplaceAllIsOneUndoStep(t *testing.T) {
	orig := []string{"foo a foo", "b", "foo end foo"}
	e := newFindEditor(t, append([]string(nil), orig...))

	openFindFor(e, "foo")
	press(e, tcell.KeyTab)
	typeText(e, "X")
	press(e, tcell.KeyCtrlA)

	want := []string{"X a X", "b", "X end X"}
	for i := range want {
		if got := e.b.Lines()[i]; got != want[i] {
			t.Fatalf("line %d after replace-all: %q, want %q", i, got, want[i])
		}
	}
	if !strings.Contains(e.notice, "replaced 4") {
		t.Errorf("notice = %q, want it to report 4 replacements", e.notice)
	}

	press(e, tcell.KeyEscape, tcell.KeyCtrlZ) // close the bar, then ONE undo

	for i := range orig {
		if got := e.b.Lines()[i]; got != orig[i] {
			t.Errorf("line %d after one undo: %q, want %q", i, got, orig[i])
		}
	}
}

// Replace-all on multiple matches on one line must not corrupt later offsets -
// this is what the reverse-order sweep guarantees.
func TestReplaceAllGrowingTextOnOneLine(t *testing.T) {
	e := newFindEditor(t, []string{"a x a x a"})

	openFindFor(e, "a")
	press(e, tcell.KeyTab)
	typeText(e, "LONGER")
	press(e, tcell.KeyCtrlA)

	if got, want := e.b.Lines()[0], "LONGER x LONGER x LONGER"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReplaceAllShrinkingText(t *testing.T) {
	e := newFindEditor(t, []string{"aaaa b aaaa b aaaa"})

	openFindFor(e, "aaaa")
	press(e, tcell.KeyTab)
	typeText(e, "z")
	press(e, tcell.KeyCtrlA)

	if got, want := e.b.Lines()[0], "z b z b z"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Deleting via replace-all (empty replacement) must work too.
func TestReplaceAllWithEmptyReplacement(t *testing.T) {
	e := newFindEditor(t, []string{"xxAxxAxx"})

	openFindFor(e, "A")
	press(e, tcell.KeyTab) // revealed, but left empty
	press(e, tcell.KeyCtrlA)

	if got, want := e.b.Lines()[0], "xxxxxx"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Replacing must bump docVersion, or the completion provider completes against
// text that no longer exists.
func TestReplaceBumpsDocVersion(t *testing.T) {
	e := newFindEditor(t, []string{"foo foo"})

	openFindFor(e, "foo")
	press(e, tcell.KeyTab)
	typeText(e, "z")

	before := e.docVersion
	press(e, tcell.KeyCtrlR)
	if e.docVersion <= before {
		t.Errorf("Ctrl+R did not advance docVersion: %d -> %d", before, e.docVersion)
	}

	before = e.docVersion
	press(e, tcell.KeyCtrlA)
	if e.docVersion <= before {
		t.Errorf("Ctrl+A did not advance docVersion: %d -> %d", before, e.docVersion)
	}
}

// Typing into the replace field must not re-run the search.
func TestReplaceFieldDoesNotChangeMatches(t *testing.T) {
	e := newFindEditor(t, []string{"foo bar"})

	openFindFor(e, "foo")
	press(e, tcell.KeyTab)
	typeText(e, "bar") // "bar" is in the buffer, but is not the query

	if e.find.query != "foo" || e.find.repl != "bar" {
		t.Fatalf("fields: query=%q repl=%q", e.find.query, e.find.repl)
	}
	if got := len(e.find.matches); got != 1 {
		t.Errorf("got %d matches, want 1 - the replace field must not re-search", got)
	}
}

func TestFindTabTogglesFocus(t *testing.T) {
	e := newFindEditor(t, []string{"foo"})

	press(e, tcell.KeyCtrlF, tcell.KeyTab)
	if !e.find.replace || !e.find.onRepl {
		t.Fatalf("first Tab: replace=%v onRepl=%v, want both true", e.find.replace, e.find.onRepl)
	}
	press(e, tcell.KeyTab)
	if e.find.onRepl {
		t.Error("second Tab should move focus back to the query field")
	}
}

// With no matches the stepping and replace keys must be inert, not panic.
func TestFindNoMatchesIsInert(t *testing.T) {
	e := newFindEditor(t, []string{"alpha"})

	openFindFor(e, "zzz")
	press(e, tcell.KeyTab)
	typeText(e, "q")
	press(e, tcell.KeyCtrlN, tcell.KeyCtrlP, tcell.KeyCtrlR, tcell.KeyCtrlA)

	if e.find.cur != -1 {
		t.Errorf("cur = %d, want -1 with no matches", e.find.cur)
	}
	if got := e.b.Lines()[0]; got != "alpha" {
		t.Errorf("buffer changed with no matches: %q", got)
	}
}

// An empty query selects nothing and highlights nothing.
func TestFindEmptyQueryHasNoMatches(t *testing.T) {
	e := newFindEditor(t, []string{"anything"})
	press(e, tcell.KeyCtrlF)

	if len(e.find.matches) != 0 || e.find.cur != -1 {
		t.Errorf("empty query: %d matches, cur=%d; want 0 and -1", len(e.find.matches), e.find.cur)
	}
}

// The find bar swallows keys that would otherwise edit the buffer.
func TestFindBarDoesNotEditBuffer(t *testing.T) {
	e := newFindEditor(t, []string{"start"})

	openFindFor(e, "xyz")

	if got := e.b.Lines()[0]; got != "start" {
		t.Errorf("typing in the find bar edited the buffer: %q", got)
	}
	if e.isModified() {
		t.Error("buffer should not be modified by searching")
	}
}

// Normal editing keys must be unaffected when the bar is closed.
func TestEditingUnaffectedWhenFindClosed(t *testing.T) {
	e := newFindEditor(t, []string{""})

	typeText(e, "hello")

	if got := e.b.Lines()[0]; got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
	if e.find != nil {
		t.Error("find bar should not be open")
	}
}

// Ctrl+S still saves while the bar is open, and Ctrl+Q still honors the guard.
func TestFindBarPassesThroughSaveAndQuit(t *testing.T) {
	e := newFindEditor(t, []string{"foo"})

	openFindFor(e, "foo")
	press(e, tcell.KeyTab)
	typeText(e, "bar")
	press(e, tcell.KeyCtrlR) // now modified

	if !e.isModified() {
		t.Fatal("expected the replacement to mark the buffer modified")
	}
	if e.handleKey(keyEvent(tcell.KeyCtrlQ)) {
		t.Error("Ctrl+Q must not quit while there are unsaved changes")
	}
	e.handleKey(keyEvent(tcell.KeyCtrlS))
	if e.isModified() {
		t.Error("Ctrl+S inside the find bar should have saved")
	}
	if !e.handleKey(keyEvent(tcell.KeyCtrlQ)) {
		t.Error("Ctrl+Q should quit once saved")
	}
}
