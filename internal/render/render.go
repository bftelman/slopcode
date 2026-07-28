// Package render draws the editor frame (statusbar, gutter, text) to a tcell screen.
package render

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/buffer"
	"github.com/bftelman/slopcode/internal/filebrowser"
	"github.com/bftelman/slopcode/internal/highlight"
)

// TabWidth is the number of columns a tab stop spans.
const TabWidth = 4

// StyleName is the chroma style used for syntax highlighting.
const StyleName = "monokai"

// SidebarWidth is the column width of the file browser panel.
const SidebarWidth = 30

// ScreenCol returns the screen x-offset of byteCol in line, expanding tabs
// and accounting for multi-byte runes — for UI elements (e.g. the
// completion popup) that must anchor at the same screen position Draw put
// the cursor, not at the buffer's byte column.
func ScreenCol(line string, byteCol, tabWidth int) int {
	return screenCol(line, byteCol, tabWidth)
}

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
	col := 0
	for _, r := range text {
		s.SetContent(x+col, y, r, nil, style)
		col++
	}
}

// Draw renders the editor into columns [originX, width) and positions the cursor.
// When showCursor is false (e.g. while browsing) the cursor is hidden.
func Draw(s tcell.Screen, b *buffer.Buffer, filename, notice string, modified bool, scroll, originX int, showCursor bool) {
	// No s.Clear(): we repaint every cell in the editor region explicitly, so
	// tcell's cell diff only flushes what actually changed (avoids flicker).
	width, height := s.Size()
	editorWidth := width - originX

	// Statusbar (row 0, inverted) across the editor region.
	bar := tcell.StyleDefault.Reverse(true)
	for x := originX; x < width; x++ {
		s.SetContent(x, 0, ' ', nil, bar)
	}
	left := fmt.Sprintf(" %s — %d lines", filename, b.LineCount())
	drawText(s, originX, 0, clip(left, editorWidth), bar)

	row, col := b.Cursor()
	right := fmt.Sprintf("Ln %d, Col %d", row+1, col+1)
	if modified {
		right += "  [modified]"
	}
	right += " "
	rightStart := width - len(right)
	if rightStart > originX {
		drawText(s, rightStart, 0, right, bar)
	}
	if notice != "" {
		noticeStyle := bar.Foreground(tcell.ColorGreen)
		nx := rightStart - len(notice) - 2
		if nx > originX+len(left) {
			drawText(s, nx, 0, notice, noticeStyle)
		}
	}

	// Text area with syntax highlighting and tab expansion.
	gw := GutterWidth(b.LineCount())
	numStyle := tcell.StyleDefault.Foreground(tcell.ColorGray)
	styled := highlight.Highlight(strings.Join(b.Lines(), "\n"), filename, StyleName)
	textRows := height - 1
	for i := 0; i < textRows; i++ {
		y := i + 1
		// Blank the whole editor region for this row first, so shrinking lines
		// and rows past the end of the buffer don't leave stale content.
		for x := originX; x < width; x++ {
			s.SetContent(x, y, ' ', nil, tcell.StyleDefault)
		}
		lineIdx := scroll + i
		if lineIdx >= len(styled) {
			continue
		}
		num := strconv.Itoa(lineIdx + 1)
		drawText(s, originX+gw-2-len(num), y, num, numStyle)
		s.SetContent(originX+gw-1, y, '│', nil, numStyle)

		x := 0
		for _, sr := range styled[lineIdx] {
			if originX+gw+x >= width {
				break
			}
			if sr.R == '\t' {
				stop := TabWidth - (x % TabWidth)
				for k := 0; k < stop && originX+gw+x < width; k++ {
					s.SetContent(originX+gw+x, y, ' ', nil, tcell.StyleDefault)
					x++
				}
				continue
			}
			s.SetContent(originX+gw+x, y, sr.R, nil, sr.Style)
			x++
		}
	}

	// Cursor position on screen (tab-aware).
	lines := b.Lines()
	cy := row - scroll + 1
	cx := originX + gw
	if row >= 0 && row < len(lines) {
		cx = originX + gw + screenCol(lines[row], col, TabWidth)
	}
	if showCursor && cy >= 1 && cy < height {
		s.ShowCursor(cx, cy)
	} else {
		s.HideCursor()
	}
	s.Show()
}

// DrawSidebar renders the file browser into columns [0, SidebarWidth).
func DrawSidebar(s tcell.Screen, br *filebrowser.Browser) {
	_, height := s.Size()
	sep := SidebarWidth - 1
	normal := tcell.StyleDefault
	dirStyle := tcell.StyleDefault.Foreground(tcell.ColorAqua)
	selStyle := tcell.StyleDefault.Reverse(true)
	header := tcell.StyleDefault.Reverse(true)

	// Clear panel + draw separator.
	for y := 0; y < height; y++ {
		for x := 0; x < sep; x++ {
			s.SetContent(x, y, ' ', nil, normal)
		}
		s.SetContent(sep, y, '│', nil, normal)
	}

	// Header (row 0): directory basename.
	drawText(s, 0, 0, clipPad(" "+filepath.Base(br.Dir()), sep), header)

	entries := br.Entries()
	sel := br.SelIndex()
	rows := height - 1
	start := 0
	if rows > 0 && sel >= rows {
		start = sel - rows + 1
	}
	for i := 0; i < rows; i++ {
		idx := start + i
		if idx >= len(entries) {
			break
		}
		y := i + 1
		e := entries[idx]
		label := e.Name
		if e.IsDir {
			label += "/"
		}
		st := normal
		if e.IsDir {
			st = dirStyle
		}
		if idx == sel {
			st = selStyle
		}
		drawText(s, 0, y, clipPad(" "+label, sep), st)
	}
	s.Show()
}

var splashArt = []string{
	"█████ █     █████ █████ █████ █████ ████  █████",
	"█     █     █   █ █   █ █     █   █ █   █ █    ",
	"█████ █     █   █ █████ █     █   █ █   █ █████",
	"    █ █     █   █ █     █     █   █ █   █ █    ",
	"█████ █████ █████ █     █████ █████ ████  █████",
}

const (
	splashPrefix = "by "
	splashHandle = "@bftelman"
	githubURL    = "https://github.com/bftelman"
)

// DrawSplash renders the no-file welcome screen: statusbar + centered banner.
func DrawSplash(s tcell.Screen, filename, notice string) {
	s.Clear()
	width, height := s.Size()

	bar := tcell.StyleDefault.Reverse(true)
	for x := 0; x < width; x++ {
		s.SetContent(x, 0, ' ', nil, bar)
	}
	drawText(s, 0, 0, clip(" "+filename, width), bar)
	if notice != "" {
		noticeStyle := bar.Foreground(tcell.ColorGreen)
		nx := width - utf8.RuneCountInString(notice) - 1
		if nx > utf8.RuneCountInString(filename)+2 {
			drawText(s, nx, 0, notice, noticeStyle)
		}
	}

	artStyle := tcell.StyleDefault.Foreground(tcell.ColorGold)
	block := len(splashArt) + 2 // banner + blank line + subtitle
	top := 1 + (height-1-block)/2
	if top < 1 {
		top = 1
	}
	for i, line := range splashArt {
		x := (width - utf8.RuneCountInString(line)) / 2
		if x < 0 {
			x = 0
		}
		drawText(s, x, top+i, line, artStyle)
	}
	// Subtitle: "by " in plain text, "@bftelman" as a clickable link (OSC 8).
	subtitleLen := utf8.RuneCountInString(splashPrefix) + utf8.RuneCountInString(splashHandle)
	sx := (width - subtitleLen) / 2
	if sx < 0 {
		sx = 0
	}
	sy := top + len(splashArt) + 1
	linkStyle := tcell.StyleDefault.Foreground(tcell.ColorAqua).Underline(true).Url(githubURL)
	drawText(s, sx, sy, splashPrefix, tcell.StyleDefault)
	drawText(s, sx+utf8.RuneCountInString(splashPrefix), sy, splashHandle, linkStyle)

	s.HideCursor()
	s.Show()
}

// clipPad truncates or right-pads text to exactly width columns.
func clipPad(text string, width int) string {
	if width < 0 {
		return ""
	}
	if len(text) > width {
		return text[:width]
	}
	for len(text) < width {
		text += " "
	}
	return text
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
