package render

import (
	"fmt"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
)

// FindBarRows is how many screen rows DrawFindBar occupies. Frame.BottomRows
// should be set to this while the bar is open, so the text area shrinks instead
// of being painted over.
const FindBarRows = 1

// FindBar is the state of the find/replace prompt line.
type FindBar struct {
	Query   string
	Repl    string
	Replace bool // the replace field is revealed
	OnRepl  bool // focus is in the replace field rather than the query field
	Total   int  // number of matches
	Current int  // 0-based index of the active match; ignored when Total is 0
}

// DrawFindBar renders the prompt on the last screen row and places the cursor in
// the focused field. It returns nothing: the caller draws it after Draw, so the
// bar's cursor wins over the text area's.
func DrawFindBar(s tcell.Screen, fb FindBar) {
	width, height := s.Size()
	y := height - 1
	if y < 0 {
		return
	}

	bar := tcell.StyleDefault.Reverse(true)
	for x := 0; x < width; x++ {
		s.SetContent(x, y, ' ', nil, bar)
	}

	// Left: the field labels and values. Track where each value starts so the
	// cursor can be placed on the focused one.
	x := 0
	put := func(text string, st tcell.Style) {
		drawText(s, x, y, clip(text, max0(width-x)), st)
		x += utf8.RuneCountInString(text)
	}

	put(" Find: ", bar)
	queryX := x
	put(fb.Query, bar)
	replX := x
	if fb.Replace {
		put("  ->  ", bar)
		replX = x
		put(fb.Repl, bar)
	}

	// Right: the match counter and key hints, dropped when the row is too narrow
	// to hold them without colliding with the fields.
	counter := "[0/0]"
	if fb.Total > 0 {
		counter = fmt.Sprintf("[%d/%d]", fb.Current+1, fb.Total)
	}
	hint := counter + "  ^N/^P step  ^R replace  ^A all  Esc cancel "
	hx := width - utf8.RuneCountInString(hint)
	if hx > x+2 {
		drawText(s, hx, y, hint, bar)
	} else if cx := width - utf8.RuneCountInString(counter) - 1; cx > x+2 {
		drawText(s, cx, y, counter, bar)
	}

	cursorX := queryX + utf8.RuneCountInString(fb.Query)
	if fb.OnRepl && fb.Replace {
		cursorX = replX + utf8.RuneCountInString(fb.Repl)
	}
	if cursorX >= width {
		cursorX = width - 1
	}
	s.ShowCursor(cursorX, y)
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
