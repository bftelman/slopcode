// Package editor runs the interactive event loop over a buffer and screen.
package editor

import (
	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/buffer"
	"github.com/bftelman/slopcode/internal/fileio"
	"github.com/bftelman/slopcode/internal/render"
)

// Editor owns the screen, buffer, and view state for one file.
type Editor struct {
	s        tcell.Screen
	b        *buffer.Buffer
	path     string
	scroll   int
	modified bool
	notice   string // transient message; cleared on the next key
}

// New builds an Editor for the given screen, buffer, and file path.
func New(s tcell.Screen, b *buffer.Buffer, path string) *Editor {
	return &Editor{s: s, b: b, path: path}
}

// Run polls events and redraws until the user quits (Ctrl+Q).
func (e *Editor) Run() {
	e.draw()
	for {
		ev := e.s.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventResize:
			e.s.Sync()
		case *tcell.EventKey:
			if e.handleKey(ev) {
				return
			}
		}
		e.draw()
	}
}

// handleKey applies one key event. It returns true when the editor should quit.
func (e *Editor) handleKey(ev *tcell.EventKey) bool {
	e.notice = "" // any key clears the transient notice
	switch ev.Key() {
	case tcell.KeyCtrlQ:
		return true
	case tcell.KeyCtrlS:
		if err := fileio.Save(e.path, e.b.Lines()); err != nil {
			e.notice = "SAVE ERROR: " + err.Error()
		} else {
			e.modified = false
			e.notice = e.path + " saved"
		}
	case tcell.KeyEnter:
		e.b.InsertNewline()
		e.modified = true
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		e.b.Backspace()
		e.modified = true
	case tcell.KeyLeft:
		e.b.MoveLeft()
	case tcell.KeyRight:
		e.b.MoveRight()
	case tcell.KeyUp:
		e.b.MoveUp()
	case tcell.KeyDown:
		e.b.MoveDown()
	case tcell.KeyHome:
		e.b.MoveHome()
	case tcell.KeyEnd:
		e.b.MoveEnd()
	case tcell.KeyTab:
		e.b.InsertTab(render.TabWidth)
		e.modified = true
	case tcell.KeyRune:
		e.b.InsertRune(ev.Rune())
		e.modified = true
	}
	return false
}

// draw adjusts the scroll offset to keep the cursor visible, then renders.
func (e *Editor) draw() {
	_, height := e.s.Size()
	textRows := height - 1
	if textRows < 1 {
		textRows = 1
	}
	row, _ := e.b.Cursor()
	if row < e.scroll {
		e.scroll = row
	} else if row >= e.scroll+textRows {
		e.scroll = row - textRows + 1
	}
	render.Draw(e.s, e.b, e.path, e.notice, e.modified, e.scroll)
}
