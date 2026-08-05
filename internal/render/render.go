// Package render draws the editor frame (statusbar, gutter, text) to a tcell screen.
package render

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/namlet/internal/buffer"
	"github.com/bftelman/namlet/internal/filebrowser"
	"github.com/bftelman/namlet/internal/highlight"
	"github.com/bftelman/namlet/internal/textsearch"
)

// Flush pushes the composed frame to the terminal. It is the single flush point:
// none of the Draw* functions call Show themselves.
//
// That matters because a frame is composed from several of them in sequence — the
// base frame, then the sidebar, find bar, completion popup, or picker on top. If
// each one flushed, every repaint would put an intermediate frame on screen: Draw
// repaints the text area over where an overlay was, so flushing there shows the
// editor *without* the overlay, and the next flush paints it back. The result is
// a visible full-frame flicker on every keystroke, worst for the largest overlay.
// Compose everything, then Flush once.
func Flush(s tcell.Screen) { s.Show() }

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

// Frame is everything Draw needs for one repaint. It replaces what used to be a
// long positional parameter list; match highlighting and the reserved bottom row
// would have pushed it to ten arguments.
type Frame struct {
	Buf        *buffer.Buffer
	Filename   string
	Notice     string
	Modified   bool
	Scroll     int
	OriginX    int
	ShowCursor bool

	// BottomRows is how many rows at the bottom of the screen are reserved for
	// another surface (the find bar), and so excluded from the text area.
	BottomRows int

	// Matches are highlighted in the text. Must be in document order, as
	// textsearch.FindAll returns them. Nil when not searching.
	Matches []textsearch.Match
	// Current indexes Matches; that one match is drawn as the active one.
	Current int

	// Hl memoizes syntax highlighting across repaints. Callers that redraw
	// frequently should supply one — without it every repaint re-tokenizes the
	// whole document, which is the dominant cost of a frame. Nil is valid and
	// simply highlights afresh each time.
	Hl *highlight.Cache
}

// Draw renders the editor into columns [OriginX, width) and positions the
// cursor. When ShowCursor is false (e.g. while browsing) the cursor is hidden.
func Draw(s tcell.Screen, f Frame) {
	// No s.Clear(): we repaint every cell in the editor region explicitly, so
	// tcell's cell diff only flushes what actually changed (avoids flicker).
	b := f.Buf
	width, height := s.Size()
	editorWidth := width - f.OriginX

	// Statusbar (row 0, inverted) across the editor region.
	bar := tcell.StyleDefault.Reverse(true)
	for x := f.OriginX; x < width; x++ {
		s.SetContent(x, 0, ' ', nil, bar)
	}
	left := fmt.Sprintf(" %s — %d lines", f.Filename, b.LineCount())
	drawText(s, f.OriginX, 0, clip(left, editorWidth), bar)

	row, col := b.Cursor()
	right := fmt.Sprintf("Ln %d, Col %d", row+1, col+1)
	if f.Modified {
		right += "  [modified]"
	}
	right += " "
	rightStart := width - len(right)
	if rightStart > f.OriginX {
		drawText(s, rightStart, 0, right, bar)
	}
	if f.Notice != "" {
		noticeStyle := bar.Foreground(tcell.ColorGreen)
		nx := rightStart - len(f.Notice) - 2
		if nx > f.OriginX+len(left) {
			drawText(s, nx, 0, f.Notice, noticeStyle)
		}
	}

	// Text area with syntax highlighting and tab expansion.
	gw := GutterWidth(b.LineCount())
	numStyle := tcell.StyleDefault.Foreground(tcell.ColorGray)
	text := strings.Join(b.Lines(), "\n")
	styled := highlightOr(f.Hl, text, f.Filename)
	textRows := height - 1 - f.BottomRows
	if textRows < 1 {
		textRows = 1
	}
	for i := 0; i < textRows; i++ {
		y := i + 1
		// Blank the whole editor region for this row first, so shrinking lines
		// and rows past the end of the buffer don't leave stale content.
		for x := f.OriginX; x < width; x++ {
			s.SetContent(x, y, ' ', nil, tcell.StyleDefault)
		}
		lineIdx := f.Scroll + i
		if lineIdx >= len(styled) {
			continue
		}
		num := strconv.Itoa(lineIdx + 1)
		drawText(s, f.OriginX+gw-2-len(num), y, num, numStyle)
		s.SetContent(f.OriginX+gw-1, y, '│', nil, numStyle)

		rowMs, first := rowSpan(f.Matches, lineIdx)
		x, byteOff := 0, 0
		for _, sr := range styled[lineIdx] {
			if f.OriginX+gw+x >= width {
				break
			}
			st := emphasize(sr.Style, rowMs, first, f.Current, byteOff)
			if sr.R == '\t' {
				stop := TabWidth - (x % TabWidth)
				for k := 0; k < stop && f.OriginX+gw+x < width; k++ {
					s.SetContent(f.OriginX+gw+x, y, ' ', nil, st)
					x++
				}
				byteOff++
				continue
			}
			s.SetContent(f.OriginX+gw+x, y, sr.R, nil, st)
			x++
			byteOff += utf8.RuneLen(sr.R)
		}
	}

	// Cursor position on screen (tab-aware).
	lines := b.Lines()
	cy := row - f.Scroll + 1
	cx := f.OriginX + gw
	if row >= 0 && row < len(lines) {
		cx = f.OriginX + gw + screenCol(lines[row], col, TabWidth)
	}
	if f.ShowCursor && cy >= 1 && cy <= textRows {
		s.ShowCursor(cx, cy)
	} else {
		s.HideCursor()
	}
}

// highlightOr highlights through c, or directly when c is nil. A nil cache is a
// valid no-cache mode, so callers that repaint rarely (notably tests) need not
// construct one.
func highlightOr(c *highlight.Cache, text, filename string) [][]highlight.StyledRune {
	if c == nil {
		return highlight.Highlight(text, filename, StyleName)
	}
	return c.Highlight(text, filename, StyleName)
}

// rowSpan returns the matches that fall on row, plus the index of the first of
// them in ms. ms must be in document order.
func rowSpan(ms []textsearch.Match, row int) ([]textsearch.Match, int) {
	if len(ms) == 0 {
		return nil, 0
	}
	lo := sort.Search(len(ms), func(i int) bool { return ms[i].Row >= row })
	hi := lo
	for hi < len(ms) && ms[hi].Row == row {
		hi++
	}
	return ms[lo:hi], lo
}

// emphasize overlays search-match emphasis onto base for the byte at off.
//
// It uses attributes rather than colors on purpose: per the theming invariant in
// AGENTS.md, this package must not hardcode colors, and Reverse/Underline
// compose with whatever foreground the active chroma theme gave the glyph.
func emphasize(base tcell.Style, rowMs []textsearch.Match, first, current, off int) tcell.Style {
	inMatch, isCurrent := false, false
	for j, m := range rowMs {
		if off >= m.Col && off < m.Col+m.Len {
			inMatch = true
			if first+j == current {
				isCurrent = true
			}
		}
	}
	switch {
	case isCurrent:
		return base.Reverse(true)
	case inMatch:
		return base.Underline(true).Bold(true)
	default:
		return base
	}
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
}

var splashArt = []string{
	"█   █ █████ █   █ █     █████ █████",
	"██  █ █   █ ██ ██ █     █       █  ",
	"█ █ █ █████ █ █ █ █     █████   █  ",
	"█  ██ █   █ █   █ █     █       █  ",
	"█   █ █   █ █   █ █████ █████   █  ",
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
