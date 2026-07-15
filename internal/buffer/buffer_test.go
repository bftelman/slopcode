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
	b.MoveDown() // row 1, col 0
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
	b.MoveLeft() // at 0,0 already
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
