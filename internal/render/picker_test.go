package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/picker"
)

func pickerRows(texts ...string) []picker.Row {
	out := make([]picker.Row, len(texts))
	for i, s := range texts {
		out[i] = picker.Row{Cand: picker.Candidate{Text: s, Path: s}}
	}
	return out
}

// screenText joins every row so a test can assert on the whole overlay.
func screenText(t *testing.T, s tcell.SimulationScreen) string {
	t.Helper()
	_, _, h := 0, 0, 0
	cells, width, _ := s.GetContents()
	h = len(cells) / width
	var sb strings.Builder
	for y := 0; y < h; y++ {
		sb.WriteString(rowText(t, s, y))
		sb.WriteByte('\n')
	}
	return sb.String()
}

func TestDrawPickerShowsTitleQueryAndRows(t *testing.T) {
	s := newSimScreen(t, 80, 24)
	defer s.Fini()

	DrawPicker(s, Picker{
		Title: "Files · slopcode",
		Query: "buf",
		Rows:  pickerRows("internal/buffer/buffer.go", "internal/buffer/buffer_test.go"),
		Total: 2,
	})
	s.Show() // compose-then-flush: Draw* no longer flushes

	got := screenText(t, s)
	for _, want := range []string{"Files · slopcode", "> buf", "internal/buffer/buffer.go"} {
		if !strings.Contains(got, want) {
			t.Errorf("overlay missing %q:\n%s", want, got)
		}
	}
}

func TestDrawPickerHighlightsSelection(t *testing.T) {
	s := newSimScreen(t, 80, 24)
	defer s.Fini()

	DrawPicker(s, Picker{Title: "T", Rows: pickerRows("first", "second", "third"), Sel: 1, Total: 3})
	s.Show() // compose-then-flush: Draw* no longer flushes

	cells, width, _ := s.GetContents()
	// Find the rows holding "first" and "second" and compare their attributes.
	var firstY, secondY = -1, -1
	for y := 0; y < 24; y++ {
		line := rowText(t, s, y)
		if strings.Contains(line, "first") {
			firstY = y
		}
		if strings.Contains(line, "second") {
			secondY = y
		}
	}
	if firstY < 0 || secondY < 0 {
		t.Fatalf("rows not found: firstY=%d secondY=%d", firstY, secondY)
	}
	x := strings.Index(rowText(t, s, secondY), "second")
	_, _, selAttr := cellAt(cells, width, x, secondY).Style.Decompose()
	_, _, plainAttr := cellAt(cells, width, x, firstY).Style.Decompose()
	if selAttr&tcell.AttrReverse == 0 {
		t.Error("selected row is not reversed")
	}
	if plainAttr&tcell.AttrReverse != 0 {
		t.Error("unselected row should not be reversed")
	}
}

// Matched characters get the accent style, and only those characters.
func TestDrawPickerAccentsMatchedCharacters(t *testing.T) {
	s := newSimScreen(t, 80, 24)
	defer s.Fini()

	text := "internal/buffer.go"
	rows := []picker.Row{{
		Cand:    picker.Candidate{Text: text},
		Matched: []int{0, 9, 10}, // "i", "b", "u"
	}}
	DrawPicker(s, Picker{Title: "T", Rows: rows, Total: 1})
	s.Show() // compose-then-flush: Draw* no longer flushes

	cells, width, _ := s.GetContents()
	var y int = -1
	for yy := 0; yy < 24; yy++ {
		if strings.Contains(rowText(t, s, yy), text) {
			y = yy
			break
		}
	}
	if y < 0 {
		t.Fatalf("row not found:\n%s", screenText(t, s))
	}
	x0 := strings.Index(rowText(t, s, y), text)

	_, _, accentAttr := cellAt(cells, width, x0+0, y).Style.Decompose()
	_, _, plainAttr := cellAt(cells, width, x0+1, y).Style.Decompose()
	if accentAttr&tcell.AttrBold == 0 {
		t.Error("matched character 0 is not accented (expected bold)")
	}
	if plainAttr&tcell.AttrBold != 0 {
		t.Error("unmatched character 1 should not be accented")
	}
}

// Byte offsets past a multi-byte rune must accent the right screen cell.
func TestDrawPickerAccentsAfterMultiByteRune(t *testing.T) {
	s := newSimScreen(t, 80, 24)
	defer s.Fini()

	text := "日本/x.go" // the CJK run is 3 bytes per rune
	xByte := strings.Index(text, "x")
	rows := []picker.Row{{Cand: picker.Candidate{Text: text}, Matched: []int{xByte}}}
	DrawPicker(s, Picker{Title: "T", Rows: rows, Total: 1})
	s.Show() // compose-then-flush: Draw* no longer flushes

	cells, width, _ := s.GetContents()
	var y = -1
	for yy := 0; yy < 24; yy++ {
		if strings.Contains(rowText(t, s, yy), "x.go") {
			y = yy
			break
		}
	}
	if y < 0 {
		t.Fatalf("row not found:\n%s", screenText(t, s))
	}
	line := rowText(t, s, y)
	xCell := strings.Index(line, "x") // rune-indexed via rowText's rune walk
	if xCell < 0 {
		t.Fatalf("could not locate 'x' in %q", line)
	}
	_, _, attr := cellAt(cells, width, len([]rune(line[:xCell])), y).Style.Decompose()
	if attr&tcell.AttrBold == 0 {
		t.Errorf("the matched rune after a multi-byte prefix was not accented (row %q)", line)
	}
}

func TestDrawPickerNoMatches(t *testing.T) {
	s := newSimScreen(t, 80, 24)
	defer s.Fini()

	DrawPicker(s, Picker{Title: "T", Query: "zzz", Rows: nil, Total: 0})
	s.Show() // compose-then-flush: Draw* no longer flushes

	if got := screenText(t, s); !strings.Contains(got, "no matches") {
		t.Errorf("expected a no-matches message:\n%s", got)
	}
}

func TestDrawPickerShowsError(t *testing.T) {
	s := newSimScreen(t, 80, 24)
	defer s.Fini()

	DrawPicker(s, Picker{Title: "T", Err: fmt.Errorf("cannot read root")})
	s.Show() // compose-then-flush: Draw* no longer flushes

	if got := screenText(t, s); !strings.Contains(got, "cannot read root") {
		t.Errorf("expected the error to be shown:\n%s", got)
	}
}

// The counter reports the true total even when the row list is truncated.
func TestDrawPickerCounterReportsTruncation(t *testing.T) {
	s := newSimScreen(t, 80, 24)
	defer s.Fini()

	DrawPicker(s, Picker{Title: "T", Rows: pickerRows("a", "b"), Total: 4321})
	s.Show() // compose-then-flush: Draw* no longer flushes

	if got := screenText(t, s); !strings.Contains(got, "4321") {
		t.Errorf("counter should report the untruncated total:\n%s", got)
	}
}

// A long list scrolls so the selection stays on screen.
func TestDrawPickerScrollsToSelection(t *testing.T) {
	s := newSimScreen(t, 80, 12)
	defer s.Fini()

	var texts []string
	for i := 0; i < 100; i++ {
		texts = append(texts, fmt.Sprintf("row%03d", i))
	}
	DrawPicker(s, Picker{Title: "T", Rows: pickerRows(texts...), Sel: 99, Total: 100})
	s.Show() // compose-then-flush: Draw* no longer flushes

	got := screenText(t, s)
	if !strings.Contains(got, "row099") {
		t.Errorf("selected row not visible after scrolling:\n%s", got)
	}
	if strings.Contains(got, "row000") {
		t.Errorf("list did not scroll away from the top:\n%s", got)
	}
}

// A tiny screen must not panic or index out of bounds.
func TestDrawPickerTinyScreen(t *testing.T) {
	for _, dim := range [][2]int{{10, 3}, {20, 4}, {1, 1}, {80, 2}} {
		s := newSimScreen(t, dim[0], dim[1])
		DrawPicker(s, Picker{Title: "T", Query: "q", Rows: pickerRows("a", "b", "c"), Sel: 2, Total: 3})
		s.Show() // compose-then-flush: Draw* no longer flushes
		s.Fini()
	}
}

// The cursor sits in the query line, not on the list.
func TestDrawPickerCursorInQueryLine(t *testing.T) {
	s := newSimScreen(t, 80, 24)
	defer s.Fini()

	DrawPicker(s, Picker{Title: "T", Query: "abc", Rows: pickerRows("x"), Total: 1})
	s.Show() // compose-then-flush: Draw* no longer flushes

	_, cy, visible := s.GetCursor()
	if !visible {
		t.Fatal("cursor should be visible")
	}
	queryY := -1
	for y := 0; y < 24; y++ {
		if strings.Contains(rowText(t, s, y), "> abc") {
			queryY = y
			break
		}
	}
	if queryY < 0 {
		t.Fatalf("query line not found:\n%s", screenText(t, s))
	}
	if cy != queryY {
		t.Errorf("cursor on row %d, want the query row %d", cy, queryY)
	}
}
