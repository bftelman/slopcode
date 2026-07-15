// Package render draws the editor frame (statusbar, gutter, text) to a tcell screen.
package render

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/buffer"
	"github.com/bftelman/slopcode/internal/highlight"
)

// TabWidth is the number of columns a tab stop spans.
const TabWidth = 4

// StyleName is the chroma style used for syntax highlighting.
const StyleName = "monokai"

// screenCol returns the screen x-offset of byteCol in line, expanding tabs.
func screenCol(line string, byteCol, tabWidth int) int {
	x := 0
	for i, r := range line {
		if i >= byteCol {
			break
		}
		if r == '\t' {
			x += tabWidth - (x % tabWidth)
		} else {
			x++
		}
	}
	return x
}

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
func Draw(s tcell.Screen, b *buffer.Buffer, filename, notice string, modified bool, scroll int) {
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
	rightStart := width - len(right)
	if rightStart > 0 {
		drawText(s, rightStart, 0, right, bar)
	}
	if notice != "" {
		noticeStyle := bar.Foreground(tcell.ColorGreen)
		nx := rightStart - len(notice) - 2
		if nx > len(left) {
			drawText(s, nx, 0, notice, noticeStyle)
		}
	}

	// Text area with syntax highlighting and tab expansion.
	gw := GutterWidth(b.LineCount())
	numStyle := tcell.StyleDefault.Foreground(tcell.ColorGray)
	styled := highlight.Highlight(strings.Join(b.Lines(), "\n"), filename, StyleName)
	textRows := height - 1
	for i := 0; i < textRows; i++ {
		lineIdx := scroll + i
		if lineIdx >= len(styled) {
			break
		}
		y := i + 1
		num := strconv.Itoa(lineIdx + 1)
		drawText(s, gw-2-len(num), y, num, numStyle)
		s.SetContent(gw-1, y, '│', nil, numStyle)

		x := 0
		for _, sr := range styled[lineIdx] {
			if gw+x >= width {
				break
			}
			if sr.R == '\t' {
				stop := TabWidth - (x % TabWidth)
				for k := 0; k < stop && gw+x < width; k++ {
					s.SetContent(gw+x, y, ' ', nil, tcell.StyleDefault)
					x++
				}
				continue
			}
			s.SetContent(gw+x, y, sr.R, nil, sr.Style)
			x++
		}
	}

	// Cursor position on screen (tab-aware).
	lines := b.Lines()
	cy := row - scroll + 1
	cx := gw
	if row >= 0 && row < len(lines) {
		cx = gw + screenCol(lines[row], col, TabWidth)
	}
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
