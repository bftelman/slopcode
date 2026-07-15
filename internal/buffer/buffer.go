// Package buffer is a pure text model with a cursor. It has no UI dependencies.
package buffer

// Buffer holds text as a slice of lines plus a cursor position.
type Buffer struct {
	lines []string
	row   int
	col   int
}

// New creates a Buffer from lines. Empty input becomes a single blank line.
func New(lines []string) *Buffer {
	if len(lines) == 0 {
		lines = []string{""}
	}
	return &Buffer{lines: lines}
}

// Lines returns the buffer's lines.
func (b *Buffer) Lines() []string { return b.lines }

// Cursor returns the current cursor row and column (both 0-based).
func (b *Buffer) Cursor() (row, col int) { return b.row, b.col }

// LineCount returns the number of lines.
func (b *Buffer) LineCount() int { return len(b.lines) }

func (b *Buffer) curLen() int { return len(b.lines[b.row]) }

func (b *Buffer) clampCol() {
	if b.col > b.curLen() {
		b.col = b.curLen()
	}
	if b.col < 0 {
		b.col = 0
	}
}

// MoveLeft moves one column left, wrapping to the end of the previous line.
func (b *Buffer) MoveLeft() {
	if b.col > 0 {
		b.col--
		return
	}
	if b.row > 0 {
		b.row--
		b.col = b.curLen()
	}
}

// MoveRight moves one column right, wrapping to the start of the next line.
func (b *Buffer) MoveRight() {
	if b.col < b.curLen() {
		b.col++
		return
	}
	if b.row < len(b.lines)-1 {
		b.row++
		b.col = 0
	}
}

// MoveUp moves to the previous line, clamping the column.
func (b *Buffer) MoveUp() {
	if b.row > 0 {
		b.row--
		b.clampCol()
	}
}

// MoveDown moves to the next line, clamping the column.
func (b *Buffer) MoveDown() {
	if b.row < len(b.lines)-1 {
		b.row++
		b.clampCol()
	}
}

// MoveHome moves to the start of the current line.
func (b *Buffer) MoveHome() { b.col = 0 }

// MoveEnd moves to the end of the current line.
func (b *Buffer) MoveEnd() { b.col = b.curLen() }

// InsertRune inserts r at the cursor and advances the column.
func (b *Buffer) InsertRune(r rune) {
	line := b.lines[b.row]
	b.lines[b.row] = line[:b.col] + string(r) + line[b.col:]
	b.col++
}

// InsertNewline splits the current line at the cursor and moves to the new line.
func (b *Buffer) InsertNewline() {
	line := b.lines[b.row]
	head, tail := line[:b.col], line[b.col:]
	b.lines[b.row] = head
	b.lines = append(b.lines, "")
	copy(b.lines[b.row+2:], b.lines[b.row+1:])
	b.lines[b.row+1] = tail
	b.row++
	b.col = 0
}

// Backspace deletes the character before the cursor, joining lines at column 0.
func (b *Buffer) Backspace() {
	if b.col > 0 {
		line := b.lines[b.row]
		b.lines[b.row] = line[:b.col-1] + line[b.col:]
		b.col--
		return
	}
	if b.row == 0 {
		return
	}
	prev := b.lines[b.row-1]
	joinCol := len(prev)
	b.lines[b.row-1] = prev + b.lines[b.row]
	b.lines = append(b.lines[:b.row], b.lines[b.row+1:]...)
	b.row--
	b.col = joinCol
}
