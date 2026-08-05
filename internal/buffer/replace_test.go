package buffer

import "testing"

func TestReplaceRangeSameLength(t *testing.T) {
	b := New([]string{"foo bar"})
	b.ReplaceRange(0, 0, 3, "baz")
	if got := b.Lines()[0]; got != "baz bar" {
		t.Errorf("got %q, want %q", got, "baz bar")
	}
	if row, col := b.Cursor(); row != 0 || col != 3 {
		t.Errorf("cursor at (%d,%d), want (0,3)", row, col)
	}
}

func TestReplaceRangeGrowAndShrink(t *testing.T) {
	b := New([]string{"src = src"})
	b.ReplaceRange(0, 6, 3, "source") // grow
	if got := b.Lines()[0]; got != "src = source" {
		t.Errorf("grow: got %q", got)
	}
	if row, col := b.Cursor(); row != 0 || col != 12 {
		t.Errorf("grow: cursor at (%d,%d), want (0,12)", row, col)
	}

	b = New([]string{"source = 1"})
	b.ReplaceRange(0, 0, 6, "s") // shrink
	if got := b.Lines()[0]; got != "s = 1" {
		t.Errorf("shrink: got %q", got)
	}
	if row, col := b.Cursor(); row != 0 || col != 1 {
		t.Errorf("shrink: cursor at (%d,%d), want (0,1)", row, col)
	}
}

func TestReplaceRangeToEmpty(t *testing.T) {
	b := New([]string{"delete me"})
	b.ReplaceRange(0, 0, 7, "") // [0,7) is "delete " - six letters plus the space
	if got := b.Lines()[0]; got != "me" {
		t.Errorf("got %q, want %q", got, "me")
	}
}

// A replacement whose span sits after a multi-byte rune must not corrupt it.
func TestReplaceRangeMultiByte(t *testing.T) {
	line := "日本語 foo tail"
	b := New([]string{line})
	// "foo" starts right after the 9-byte CJK run plus a space.
	col := 10
	if line[col:col+3] != "foo" {
		t.Fatalf("test setup wrong: line[%d:%d] = %q", col, col+3, line[col:col+3])
	}
	b.ReplaceRange(0, col, 3, "BAR")
	if got, want := b.Lines()[0], "日本語 BAR tail"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReplaceRangeClamps(t *testing.T) {
	tests := []struct {
		name             string
		row, col, length int
		want             string
	}{
		{"length past end of line truncates", 0, 4, 99, "abc "},
		{"col past end of line appends nothing", 0, 99, 3, "abc def"},
		// col and length clamp to the line, matching SetCursor's contract; an
		// out-of-range *row* is the only case treated as a no-op.
		{"negative col clamps to 0", 0, -5, 3, " def"},
		{"negative length becomes empty span", 0, 0, -1, "abc def"},
		{"row past end is a no-op", 5, 0, 1, "abc def"},
		{"negative row is a no-op", -1, 0, 1, "abc def"},
	}
	for _, tc := range tests {
		b := New([]string{"abc def"})
		b.ReplaceRange(tc.row, tc.col, tc.length, "")
		if got := b.Lines()[0]; got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Replace-all applies matches in reverse document order under one checkpoint,
// so a single Undo reverts the entire sweep. This is the core guarantee the
// editor's Ctrl+A relies on.
func TestReplaceRangeReverseOrderIsOneUndo(t *testing.T) {
	orig := []string{"foo a foo", "b", "foo"}
	b := New(append([]string(nil), orig...))
	b.Checkpoint()
	// Reverse document order: (2,0), (0,6), (0,0).
	b.ReplaceRange(2, 0, 3, "quux")
	b.ReplaceRange(0, 6, 3, "quux")
	b.ReplaceRange(0, 0, 3, "quux")
	if got, want := b.Lines()[0], "quux a quux"; got != want {
		t.Fatalf("line 0: got %q, want %q", got, want)
	}
	if got, want := b.Lines()[2], "quux"; got != want {
		t.Fatalf("line 2: got %q, want %q", got, want)
	}
	if !b.Undo() {
		t.Fatal("Undo returned false")
	}
	for i := range orig {
		if got := b.Lines()[i]; got != orig[i] {
			t.Errorf("after undo line %d: got %q, want %q", i, got, orig[i])
		}
	}
}

func TestSetCursorClamps(t *testing.T) {
	tests := []struct {
		name             string
		row, col         int
		wantRow, wantCol int
	}{
		{"in range", 1, 2, 1, 2},
		{"col past end of line", 1, 99, 1, 5},
		{"row past end of buffer", 99, 0, 2, 0},
		{"negative row", -3, 1, 0, 1},
		{"negative col", 0, -3, 0, 0},
		{"row clamp then col clamp", 99, 99, 2, 3},
	}
	for _, tc := range tests {
		b := New([]string{"abcdefg", "hijkl", "mno"})
		b.SetCursor(tc.row, tc.col)
		row, col := b.Cursor()
		if row != tc.wantRow || col != tc.wantCol {
			t.Errorf("%s: SetCursor(%d,%d) -> (%d,%d), want (%d,%d)",
				tc.name, tc.row, tc.col, row, col, tc.wantRow, tc.wantCol)
		}
	}
}
