// Package editor runs the interactive event loop over a buffer and screen.
package editor

import (
	"path/filepath"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/autopair"
	"github.com/bftelman/slopcode/internal/buffer"
	"github.com/bftelman/slopcode/internal/filebrowser"
	"github.com/bftelman/slopcode/internal/fileio"
	"github.com/bftelman/slopcode/internal/render"
)

// Editor owns the screen, buffer, and view state for one file.
type Editor struct {
	s           tcell.Screen
	b           *buffer.Buffer
	path        string
	scroll      int
	modified    bool
	notice      string // transient message; cleared on the next key
	browser     *filebrowser.Browser // non-nil while browsing
	pendingOpen string               // unsaved-changes guard latch
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
	if e.browser != nil {
		return e.handleBrowseKey(ev)
	}
	e.notice = "" // any key clears the transient notice
	switch ev.Key() {
	case tcell.KeyCtrlQ:
		return true
	case tcell.KeyCtrlB:
		e.openBrowser()
	case tcell.KeyCtrlS:
		e.save()
	case tcell.KeyEnter:
		e.b.InsertNewline()
		e.modified = true
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		autopair.Backspace(e.b)
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
		autopair.InsertRune(e.b, ev.Rune())
		e.modified = true
	}
	return false
}

func (e *Editor) save() {
	if err := fileio.Save(e.path, e.b.Lines()); err != nil {
		e.notice = "SAVE ERROR: " + err.Error()
		return
	}
	e.modified = false
	e.notice = e.path + " saved"
}

func (e *Editor) openBrowser() {
	dir := filepath.Dir(e.path)
	if dir == "" {
		dir = "."
	}
	br, err := filebrowser.Open(dir)
	if err != nil {
		e.notice = "BROWSE ERROR: " + err.Error()
		return
	}
	e.browser = br
	e.pendingOpen = ""
}

// handleBrowseKey handles input while the file browser is open.
func (e *Editor) handleBrowseKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyCtrlQ:
		return true
	case tcell.KeyCtrlB, tcell.KeyEscape:
		e.browser = nil
		e.pendingOpen = ""
	case tcell.KeyCtrlS:
		e.save()
	case tcell.KeyUp:
		e.browser.MoveUp()
		e.pendingOpen = ""
	case tcell.KeyDown:
		e.browser.MoveDown()
		e.pendingOpen = ""
	case tcell.KeyEnter:
		e.browseEnter()
	}
	return false
}

func (e *Editor) browseEnter() {
	path, isDir, err := e.browser.Enter()
	if err != nil {
		e.notice = "BROWSE ERROR: " + err.Error()
		return
	}
	if isDir {
		e.pendingOpen = ""
		return
	}
	if e.modified && e.pendingOpen != path {
		e.pendingOpen = path
		e.notice = "unsaved changes — Ctrl+S, or Enter again to discard"
		return
	}
	lines, err := fileio.Load(path)
	if err != nil {
		e.notice = "OPEN ERROR: " + err.Error()
		return
	}
	e.b = buffer.New(lines)
	e.path = path
	e.scroll = 0
	e.modified = false
	e.browser = nil
	e.pendingOpen = ""
	e.notice = path + " opened"
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
	if e.browser != nil {
		render.Draw(e.s, e.b, e.path, e.notice, e.modified, e.scroll, render.SidebarWidth, false)
		render.DrawSidebar(e.s, e.browser)
	} else {
		render.Draw(e.s, e.b, e.path, e.notice, e.modified, e.scroll, 0, true)
	}
}
