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
	baseline    []string // content as last loaded/saved; "modified" is derived from this
	notice      string   // transient message; cleared on the next key
	browser     *filebrowser.Browser // non-nil while browsing
	pendingOpen string               // unsaved-changes guard latch
}

// New builds an Editor for the given screen, buffer, and file path.
func New(s tcell.Screen, b *buffer.Buffer, path string) *Editor {
	return &Editor{s: s, b: b, path: path, baseline: cloneLines(b.Lines())}
}

// isModified reports whether the buffer differs from the last loaded/saved state.
func (e *Editor) isModified() bool {
	return !linesEqual(e.baseline, e.b.Lines())
}

func cloneLines(src []string) []string {
	cp := make([]string, len(src))
	copy(cp, src)
	return cp
}

func linesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
		return e.tryQuit()
	case tcell.KeyCtrlB:
		e.openBrowser()
	case tcell.KeyCtrlS:
		e.save()
	case tcell.KeyCtrlZ:
		e.b.Undo()
	case tcell.KeyCtrlY:
		e.b.Redo()
	case tcell.KeyEnter:
		e.b.Checkpoint()
		e.b.InsertNewline()
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		e.b.Checkpoint()
		autopair.Backspace(e.b)
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
		e.b.Checkpoint()
		e.b.InsertTab(render.TabWidth)
	case tcell.KeyRune:
		e.b.Checkpoint()
		autopair.InsertRune(e.b, ev.Rune())
	}
	return false
}

// tryQuit returns true only when it is safe to quit (no unsaved changes).
func (e *Editor) tryQuit() bool {
	if e.isModified() {
		e.notice = "unsaved changes — Ctrl+S to save before quitting"
		return false
	}
	return true
}

// displayName is the filename shown in the statusbar.
func (e *Editor) displayName() string {
	if e.path == "" {
		return "[No Name]"
	}
	return e.path
}

func (e *Editor) save() {
	if e.path == "" {
		e.path = "untitled.txt"
	}
	if err := fileio.Save(e.path, e.b.Lines()); err != nil {
		e.notice = "SAVE ERROR: " + err.Error()
		return
	}
	e.baseline = cloneLines(e.b.Lines())
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
		return e.tryQuit()
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
	if e.isModified() && e.pendingOpen != path {
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
	e.baseline = cloneLines(e.b.Lines())
	e.browser = nil
	e.pendingOpen = ""
	e.notice = path + " opened"
}

// draw adjusts the scroll offset to keep the cursor visible, then renders.
func (e *Editor) draw() {
	if e.browser == nil && e.path == "" && !e.isModified() {
		render.DrawSplash(e.s, e.displayName(), e.notice)
		return
	}
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
	modified := e.isModified()
	if e.browser != nil {
		render.Draw(e.s, e.b, e.displayName(), e.notice, modified, e.scroll, render.SidebarWidth, false)
		render.DrawSidebar(e.s, e.browser)
	} else {
		render.Draw(e.s, e.b, e.displayName(), e.notice, modified, e.scroll, 0, true)
	}
}
