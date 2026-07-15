// Package buffer is a pure text model with a cursor. It has no UI dependencies.
package buffer

// Buffer holds text as a slice of lines plus a cursor position.
type Buffer struct {
	lines []string
	row   int
	col   int
	undo  []state
	redo  []state
}

const maxUndo = 500

type state struct {
	lines []string
	row   int
	col   int
}

func (b *Buffer) snapshot() state {
	cp := make([]string, len(b.lines))
	copy(cp, b.lines)
	return state{lines: cp, row: b.row, col: b.col}
}

func (b *Buffer) restore(s state) {
	cp := make([]string, len(s.lines))
	copy(cp, s.lines)
	b.lines = cp
	b.row = s.row
	b.col = s.col
}

// Checkpoint records the current state for undo and clears the redo stack.
// The editor calls this once before each mutating action.
func (b *Buffer) Checkpoint() {
	b.undo = append(b.undo, b.snapshot())
	if len(b.undo) > maxUndo {
		b.undo = b.undo[len(b.undo)-maxUndo:]
	}
	b.redo = nil
}

// Undo reverts to the previous checkpoint. Returns false if there is none.
func (b *Buffer) Undo() bool {
	if len(b.undo) == 0 {
		return false
	}
	b.redo = append(b.redo, b.snapshot())
	last := b.undo[len(b.undo)-1]
	b.undo = b.undo[:len(b.undo)-1]
	b.restore(last)
	return true
}

// Redo reapplies the most recently undone state. Returns false if there is none.
func (b *Buffer) Redo() bool {
	if len(b.redo) == 0 {
		return false
	}
	b.undo = append(b.undo, b.snapshot())
	last := b.redo[len(b.redo)-1]
	b.redo = b.redo[:len(b.redo)-1]
	b.restore(last)
	return true
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

// RuneAt returns the rune at col+offset in the current line, or false if out of range.
func (b *Buffer) RuneAt(offset int) (rune, bool) {
	line := b.lines[b.row]
	i := b.col + offset
	if i < 0 || i >= len(line) {
		return 0, false
	}
	return rune(line[i]), true
}

// InsertTab inserts spaces to advance the cursor to the next tab stop.
func (b *Buffer) InsertTab(width int) {
	if width < 1 {
		width = 1
	}
	n := width - (b.col % width)
	for i := 0; i < n; i++ {
		b.InsertRune(' ')
	}
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
