package render

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/buffer"
	"github.com/bftelman/slopcode/internal/textsearch"
)

// rowText reads a whole screen row back as a string, one rune per screen cell.
//
// An unpainted cell becomes a space rather than being skipped: overlays such as
// the picker only paint their own box, and dropping the untouched cells would
// desynchronize string indexes from screen columns, so a test locating text with
// strings.Index would compute the wrong x.
func rowText(t *testing.T, s tcell.SimulationScreen, y int) string {
	t.Helper()
	cells, width, _ := s.GetContents()
	var sb strings.Builder
	for x := 0; x < width; x++ {
		c := cellAt(cells, width, x, y)
		if len(c.Runes) > 0 {
			sb.WriteRune(c.Runes[0])
			continue
		}
		sb.WriteByte(' ')
	}
	return sb.String()
}

func TestDrawFindBarShowsQueryAndCounter(t *testing.T) {
	s := newSimScreen(t, 80, 10)
	defer s.Fini()

	DrawFindBar(s, FindBar{Query: "src", Total: 7, Current: 1})

	got := rowText(t, s, 9)
	if !strings.Contains(got, "Find: src") {
		t.Errorf("row missing query: %q", got)
	}
	if !strings.Contains(got, "[2/7]") {
		t.Errorf("row missing 1-based counter [2/7]: %q", got)
	}
}

func TestDrawFindBarZeroMatches(t *testing.T) {
	s := newSimScreen(t, 80, 10)
	defer s.Fini()

	DrawFindBar(s, FindBar{Query: "nope", Total: 0})

	if got := rowText(t, s, 9); !strings.Contains(got, "[0/0]") {
		t.Errorf("want [0/0] for no matches, got %q", got)
	}
}

func TestDrawFindBarReplaceField(t *testing.T) {
	s := newSimScreen(t, 80, 10)
	defer s.Fini()

	DrawFindBar(s, FindBar{Query: "src", Repl: "source", Replace: true, Total: 1})

	got := rowText(t, s, 9)
	if !strings.Contains(got, "src") || !strings.Contains(got, "source") {
		t.Errorf("row missing one of the fields: %q", got)
	}
	// Hidden until revealed.
	s2 := newSimScreen(t, 80, 10)
	defer s2.Fini()
	DrawFindBar(s2, FindBar{Query: "src", Repl: "source", Replace: false, Total: 1})
	if got := rowText(t, s2, 9); strings.Contains(got, "source") {
		t.Errorf("replace field shown while not revealed: %q", got)
	}
}

func TestDrawFindBarCursorFollowsFocus(t *testing.T) {
	s := newSimScreen(t, 80, 10)
	defer s.Fini()

	DrawFindBar(s, FindBar{Query: "src", Repl: "source", Replace: true, OnRepl: false, Total: 1})
	qx, _, _ := s.GetCursor()

	DrawFindBar(s, FindBar{Query: "src", Repl: "source", Replace: true, OnRepl: true, Total: 1})
	rx, _, _ := s.GetCursor()

	if !(rx > qx) {
		t.Errorf("cursor did not move into the replace field: query x=%d, replace x=%d", qx, rx)
	}
}

// A narrow screen must not panic or draw outside its bounds.
func TestDrawFindBarNarrowScreen(t *testing.T) {
	s := newSimScreen(t, 12, 5)
	defer s.Fini()
	DrawFindBar(s, FindBar{Query: "averylongquerystring", Total: 3, Current: 0})
	if got := rowText(t, s, 4); len([]rune(got)) != 12 {
		t.Errorf("row is %d cells, want 12: %q", len([]rune(got)), got)
	}
}

// Frame.BottomRows must shrink the text area so the find bar is not painted over.
func TestDrawReservesBottomRows(t *testing.T) {
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, "line")
	}
	s := newSimScreen(t, 40, 10)
	defer s.Fini()

	Draw(s, Frame{Buf: buffer.New(lines), Filename: "t.txt", ShowCursor: true, BottomRows: FindBarRows})

	// Rows 1..8 are text; row 9 is reserved and must be left blank by Draw.
	if got := strings.TrimSpace(rowText(t, s, 8)); got == "" {
		t.Errorf("row 8 should still be text, got blank")
	}
	if got := strings.TrimSpace(rowText(t, s, 9)); got != "" {
		t.Errorf("row 9 is reserved for the find bar but Draw wrote %q", got)
	}
}

// Match highlighting must land on the right screen columns on a line with tabs,
// which is why it goes through the same tab expansion as the glyphs themselves.
func TestDrawHighlightsMatchesWithTabs(t *testing.T) {
	s := newSimScreen(t, 80, 6)
	defer s.Fini()

	line := "\t\tfoo bar"
	b := buffer.New([]string{line})
	ms := textsearch.FindAll([]string{line}, "foo")
	if len(ms) != 1 {
		t.Fatalf("setup: got %d matches, want 1", len(ms))
	}

	Draw(s, Frame{Buf: b, Filename: "t.txt", ShowCursor: true, Matches: ms, Current: 0})

	cells, width, _ := s.GetContents()
	gw := GutterWidth(1)
	// Two leading tabs expand to 8 columns, so "foo" occupies screen cols 8..10
	// after the gutter.
	startX := gw + ScreenCol(line, ms[0].Col, TabWidth)
	if startX != gw+8 {
		t.Fatalf("setup: match starts at screen col %d, want %d", startX, gw+8)
	}
	for i := 0; i < 3; i++ {
		c := cellAt(cells, width, startX+i, 1)
		if _, _, attr := c.Style.Decompose(); attr&tcell.AttrReverse == 0 {
			t.Errorf("cell %d of the current match is not reversed (rune %q)", i, c.Runes)
		}
	}
	// The character just before the match must be untouched.
	if c := cellAt(cells, width, startX-1, 1); func() bool {
		_, _, a := c.Style.Decompose()
		return a&tcell.AttrReverse != 0
	}() {
		t.Errorf("cell before the match was highlighted")
	}
}

// Non-current matches are emphasised differently from the current one.
func TestDrawDistinguishesCurrentMatch(t *testing.T) {
	s := newSimScreen(t, 80, 6)
	defer s.Fini()

	line := "foo and foo"
	b := buffer.New([]string{line})
	ms := textsearch.FindAll([]string{line}, "foo")
	if len(ms) != 2 {
		t.Fatalf("setup: got %d matches, want 2", len(ms))
	}

	Draw(s, Frame{Buf: b, Filename: "t.txt", ShowCursor: true, Matches: ms, Current: 1})

	cells, width, _ := s.GetContents()
	gw := GutterWidth(1)

	_, _, first := cellAt(cells, width, gw+ms[0].Col, 1).Style.Decompose()
	_, _, second := cellAt(cells, width, gw+ms[1].Col, 1).Style.Decompose()

	if first&tcell.AttrReverse != 0 {
		t.Error("non-current match should not be reversed")
	}
	if first&tcell.AttrUnderline == 0 {
		t.Error("non-current match should be underlined")
	}
	if second&tcell.AttrReverse == 0 {
		t.Error("current match should be reversed")
	}
}

func TestRowSpan(t *testing.T) {
	ms := []textsearch.Match{
		{Row: 0, Col: 0, Len: 1}, {Row: 0, Col: 5, Len: 1},
		{Row: 3, Col: 2, Len: 1},
	}
	got, first := rowSpan(ms, 0)
	if len(got) != 2 || first != 0 {
		t.Errorf("row 0: got %v first=%d, want 2 matches at 0", got, first)
	}
	got, first = rowSpan(ms, 3)
	if len(got) != 1 || first != 2 {
		t.Errorf("row 3: got %v first=%d, want 1 match at 2", got, first)
	}
	if got, _ = rowSpan(ms, 1); len(got) != 0 {
		t.Errorf("row 1: got %v, want none", got)
	}
	if got, _ = rowSpan(nil, 0); len(got) != 0 {
		t.Errorf("nil matches: got %v, want none", got)
	}
}
