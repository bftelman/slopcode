// Package render draws the editor frame (statusbar, gutter, text) to a tcell screen.
package render

import (
	"fmt"
	"strconv"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/buffer"
)

// GutterWidth returns the column width reserved for line numbers plus separator.
func GutterWidth(lineCount int) int {
	digits := len(strconv.Itoa(lineCount))
	w := digits + 2 // one leading space, one column for the separator
	if w < 4 {
		w = 4
	}
	return w
}

func drawText(s tcell.Screen, x, y int, text string, style tcell.Style) {
	for i, r := range text {
		s.SetContent(x+i, y, r, nil, style)
	}
}

// Draw renders the full editor frame and positions the cursor.
func Draw(s tcell.Screen, b *buffer.Buffer, filename string, modified bool, scroll int) {
	s.Clear()
	width, height := s.Size()

	// Statusbar (row 0, inverted).
	bar := tcell.StyleDefault.Reverse(true)
	for x := 0; x < width; x++ {
		s.SetContent(x, 0, ' ', nil, bar)
	}
	left := fmt.Sprintf(" %s — %d lines", filename, b.LineCount())
	drawText(s, 0, 0, clip(left, width), bar)
	row, col := b.Cursor()
	right := fmt.Sprintf("Ln %d, Col %d", row+1, col+1)
	if modified {
		right += "  [modified]"
	}
	right += " "
	if len(right) < width {
		drawText(s, width-len(right), 0, right, bar)
	}

	// Text area.
	gw := GutterWidth(b.LineCount())
	numStyle := tcell.StyleDefault.Foreground(tcell.ColorGray)
	lines := b.Lines()
	textRows := height - 1
	for i := 0; i < textRows; i++ {
		lineIdx := scroll + i
		if lineIdx >= len(lines) {
			break
		}
		y := i + 1
		num := strconv.Itoa(lineIdx + 1)
		drawText(s, gw-2-len(num), y, num, numStyle)
		s.SetContent(gw-1, y, '│', nil, numStyle)
		drawText(s, gw, y, clip(lines[lineIdx], width-gw), tcell.StyleDefault)
	}

	// Cursor position on screen.
	cy := row - scroll + 1
	cx := gw + col
	if cy >= 1 && cy < height {
		s.ShowCursor(cx, cy)
	} else {
		s.HideCursor()
	}
	s.Show()
}

func clip(s string, max int) string {
	if max < 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return s[:max]
}
