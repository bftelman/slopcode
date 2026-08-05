package render

import (
	"fmt"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/highlight"
	"github.com/bftelman/slopcode/internal/picker"
)

// Picker is the overlay to draw: a centered box with a query line and a ranked
// list of candidates.
type Picker struct {
	Title string
	Query string
	Rows  []picker.Row
	Sel   int
	Total int // total matches, which may exceed len(Rows)
	Err   error
}

// matchAccentToken is the chroma token whose color accents the characters that
// matched the query. This is the only place to edit to recolor them; swapping
// StyleName re-themes the picker along with syntax highlighting, per the
// theming invariant in AGENTS.md.
const matchAccentToken = chroma.Keyword

// picker box geometry, as a fraction of the screen.
const (
	pickerWidthNum, pickerWidthDen   = 7, 10 // 70% of the width
	pickerHeightNum, pickerHeightDen = 7, 10 // at most 70% of the height
	pickerMinWidth                   = 24
	pickerChromeRows                 = 2 // header + query line
)

// DrawPicker renders p as a centered overlay. Colors come from the active
// syntax-highlight theme, as the completion popup's do.
func DrawPicker(s tcell.Screen, p Picker) {
	width, height := s.Size()
	if width < pickerMinWidth || height < pickerChromeRows+1 {
		return
	}

	boxW := width * pickerWidthNum / pickerWidthDen
	if boxW < pickerMinWidth {
		boxW = pickerMinWidth
	}
	if boxW > width {
		boxW = width
	}

	maxBoxH := height * pickerHeightNum / pickerHeightDen
	if maxBoxH < pickerChromeRows+1 {
		maxBoxH = pickerChromeRows + 1
	}
	listRows := maxBoxH - pickerChromeRows
	if listRows > len(p.Rows) {
		listRows = len(p.Rows)
	}
	if listRows < 1 {
		listRows = 1 // keep one row for the empty/error message
	}
	boxH := listRows + pickerChromeRows
	if boxH > height {
		boxH = height
		listRows = boxH - pickerChromeRows
	}

	x0 := (width - boxW) / 2
	y0 := (height - boxH) / 2
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}

	panel := highlight.BackgroundStyle(StyleName)
	header := panel.Reverse(true).Bold(true)
	sel := panel.Reverse(true)
	accent := panel
	if fg, _, _ := highlight.Style(matchAccentToken, StyleName).Decompose(); fg != tcell.ColorDefault {
		accent = panel.Foreground(fg).Bold(true)
	}
	selAccent := sel.Bold(true)

	// Header: title on the left, match count on the right.
	count := fmt.Sprintf("%d/%d ", min(p.Total, len(p.Rows)), p.Total)
	if p.Total <= len(p.Rows) {
		count = fmt.Sprintf("%d ", p.Total)
	}
	drawText(s, x0, y0, clipPad(" "+p.Title, boxW), header)
	if cx := x0 + boxW - utf8.RuneCountInString(count); cx > x0 {
		drawText(s, cx, y0, count, header)
	}

	// Query line.
	drawText(s, x0, y0+1, clipPad(" > "+p.Query, boxW), panel)

	// Scroll the list so the selection stays visible.
	start := 0
	if p.Sel >= listRows {
		start = p.Sel - listRows + 1
	}

	if p.Err != nil {
		drawText(s, x0, y0+2, clipPad(" error: "+p.Err.Error(), boxW), panel)
		s.ShowCursor(x0+3+utf8.RuneCountInString(p.Query), y0+1)
		s.Show()
		return
	}
	if len(p.Rows) == 0 {
		drawText(s, x0, y0+2, clipPad(" (no matches)", boxW), panel)
		s.ShowCursor(x0+3+utf8.RuneCountInString(p.Query), y0+1)
		s.Show()
		return
	}

	for i := 0; i < listRows; i++ {
		idx := start + i
		y := y0 + pickerChromeRows + i
		if idx >= len(p.Rows) {
			drawText(s, x0, y, clipPad("", boxW), panel)
			continue
		}
		row := p.Rows[idx]
		rowStyle, accentStyle := panel, accent
		if idx == p.Sel {
			rowStyle, accentStyle = sel, selAccent
		}
		drawText(s, x0, y, clipPad(" "+row.Cand.Text, boxW), rowStyle)
		accentMatched(s, x0+1, y, x0+boxW, row.Cand.Text, row.Matched, accentStyle)
	}

	// The cursor belongs in the query line.
	qx := x0 + 3 + utf8.RuneCountInString(p.Query)
	if qx >= x0+boxW {
		qx = x0 + boxW - 1
	}
	s.ShowCursor(qx, y0+1)
	s.Show()
}

// accentMatched repaints the runes of text at the given byte offsets in style.
//
// The offsets from the matcher are byte offsets, while the screen advances one
// cell per rune, so this walks the string tracking both rather than treating
// them as interchangeable.
func accentMatched(s tcell.Screen, x, y, limit int, text string, matched []int, style tcell.Style) {
	if len(matched) == 0 {
		return
	}
	set := make(map[int]struct{}, len(matched))
	for _, off := range matched {
		set[off] = struct{}{}
	}
	col, byteOff := 0, 0
	for _, r := range text {
		if x+col >= limit {
			return
		}
		if _, ok := set[byteOff]; ok {
			s.SetContent(x+col, y, r, nil, style)
		}
		col++
		byteOff += utf8.RuneLen(r)
	}
}
