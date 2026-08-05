// Package editor runs the interactive event loop over a buffer and screen.
package editor

import (
	"path/filepath"
	"sync"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/autopair"
	"github.com/bftelman/slopcode/internal/buffer"
	"github.com/bftelman/slopcode/internal/completion"
	"github.com/bftelman/slopcode/internal/filebrowser"
	"github.com/bftelman/slopcode/internal/fileio"
	"github.com/bftelman/slopcode/internal/picker"
	"github.com/bftelman/slopcode/internal/render"
)

// Editor owns the screen, buffer, and view state for one file.
type Editor struct {
	s           tcell.Screen
	b           *buffer.Buffer
	path        string
	scroll      int
	baseline    []string             // content as last loaded/saved; "modified" is derived from this
	notice      string               // transient message; cleared on the next key
	browser     *filebrowser.Browser // non-nil while browsing
	pendingOpen string               // unsaved-changes guard latch
	splashShown bool                 // true when the last frame was the splash screen

	find    *findState // non-nil while the find/replace bar is open
	pick    *pickState // non-nil while the fuzzy picker overlay is open
	pk      *picker.Engine
	pickGen int // editor-owned generation, like docVersion, for dropping stale results

	eng        *completion.Engine
	docVersion int
	reqPos     completion.Position // cursor position the outstanding request was issued at
	popup      *render.Popup       // nil when the completion popup is closed
	popupMu    sync.Mutex          // guards popup: written on Run()'s goroutine, polled by tests
}

// New builds an Editor with the default gopls-backed completion engine.
func New(s tcell.Screen, b *buffer.Buffer, path string) *Editor {
	eng := completion.New(completion.LSPRegistry(completion.GoplsSpecs()))
	return NewWithEngine(s, b, path, eng)
}

// NewWithEngine builds an Editor with an injected completion engine (tests).
func NewWithEngine(s tcell.Screen, b *buffer.Buffer, path string, eng *completion.Engine) *Editor {
	e := &Editor{
		s: s, b: b, path: path,
		baseline: cloneLines(b.Lines()),
		eng:      eng,
		pk:       picker.NewEngine(),
	}
	if path != "" {
		e.eng.Open(e.document())
	}
	go e.bridge()
	go e.pickBridge()
	return e
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
		case *completionEvent:
			e.applyResult(ev.res)
		case *pickerEvent:
			e.applyPickResult(ev.res)
		}
		e.draw()
	}
}

// handleKey applies one key event. It returns true when the editor should quit.
//
// Precedence: the find bar and the browser are modal surfaces that consume keys
// wholesale; the completion popup only intercepts the few keys it reinterprets
// and otherwise falls through to normal editing.
func (e *Editor) handleKey(ev *tcell.EventKey) bool {
	if e.pick != nil {
		return e.handlePickKey(ev)
	}
	if e.find != nil {
		return e.handleFindKey(ev)
	}
	if e.browser != nil {
		return e.handleBrowseKey(ev)
	}
	if e.popup != nil {
		if e.handlePopupKey(ev) {
			return false
		}
	}
	e.notice = "" // any key clears the transient notice
	switch ev.Key() {
	case tcell.KeyCtrlQ:
		return e.tryQuit()
	case tcell.KeyCtrlF:
		e.openFind()
	case tcell.KeyCtrlP:
		e.openFilePicker()
	case tcell.KeyCtrlG:
		e.openLinePicker()
	case tcell.KeyCtrlB:
		e.openBrowser()
	case tcell.KeyCtrlS:
		e.save()
	case tcell.KeyCtrlZ:
		e.b.Undo()
		e.docVersion++
		e.dismissPopup()
	case tcell.KeyCtrlY:
		e.b.Redo()
		e.docVersion++
		e.dismissPopup()
	case tcell.KeyEnter:
		e.b.Checkpoint()
		e.b.InsertNewline()
		e.docVersion++
		e.requestCompletion(0)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		e.b.Checkpoint()
		autopair.Backspace(e.b)
		e.docVersion++
		e.requestCompletion(0)
	case tcell.KeyLeft:
		e.dismissPopup() // a stale popup's items/anchor no longer match the new cursor position
		e.b.MoveLeft()
	case tcell.KeyRight:
		e.dismissPopup()
		e.b.MoveRight()
	case tcell.KeyUp:
		e.dismissPopup()
		e.b.MoveUp()
	case tcell.KeyDown:
		e.dismissPopup()
		e.b.MoveDown()
	case tcell.KeyHome:
		e.dismissPopup()
		e.b.MoveHome()
	case tcell.KeyEnd:
		e.dismissPopup()
		e.b.MoveEnd()
	case tcell.KeyTab:
		e.b.Checkpoint()
		e.b.InsertTab(render.TabWidth)
		e.docVersion++
		e.requestCompletion(0)
	case tcell.KeyRune:
		e.b.Checkpoint()
		autopair.InsertRune(e.b, ev.Rune())
		e.docVersion++
		e.requestCompletion(ev.Rune())
	}
	return false
}

// tryQuit returns true only when it is safe to quit (no unsaved changes). On
// the confirmed-quit path it also shuts down the completion engine (and its
// gopls subprocess, if any) so Run() never returns with an orphaned process.
func (e *Editor) tryQuit() bool {
	if e.isModified() {
		e.notice = "unsaved changes — Ctrl+S to save before quitting"
		return false
	}
	e.eng.Close()
	e.pk.Close()
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
	e.dismissPopup() // the browser paints over it, and it made no sense there anyway
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
	if err := e.openPath(path); err != nil {
		e.notice = "OPEN ERROR: " + err.Error()
		return
	}
	e.browser = nil
	e.pendingOpen = ""
	e.notice = path + " opened"
}

// openPath loads path into the buffer and resets the view state for it.
//
// The ordering here matters and is shared by every caller (the browser and the
// picker) rather than duplicated: the completion engine must be told the old
// document is gone *before* e.path changes under it, or it keeps sending
// didChange for a URI it never opened and the new file never gets a didOpen at
// all. Getting this wrong broke real gopls symbol resolution once already.
func (e *Editor) openPath(path string) error {
	lines, err := fileio.Load(path)
	if err != nil {
		return err
	}
	e.dismissPopup()
	if oldURI := e.uri(); oldURI != "" {
		e.eng.CloseDoc(oldURI)
	}
	e.b = buffer.New(lines)
	e.path = path
	e.scroll = 0
	e.baseline = cloneLines(e.b.Lines())
	e.docVersion++
	if e.path != "" {
		e.eng.Open(e.document())
	}
	return nil
}

// draw adjusts the scroll offset to keep the cursor visible, then renders.
func (e *Editor) draw() {
	// The splash screen replaces the whole frame, so every surface that can be
	// open over it has to suppress it — otherwise the early return here means
	// the overlay is never drawn at all.
	if e.browser == nil && e.pick == nil && e.find == nil && e.path == "" && !e.isModified() {
		render.DrawSplash(e.s, e.displayName(), e.notice)
		e.splashShown = true
		return
	}
	_, height := e.s.Size()
	bottomRows := 0
	if e.find != nil {
		bottomRows = render.FindBarRows
	}
	textRows := height - 1 - bottomRows
	if textRows < 1 {
		textRows = 1
	}
	row, _ := e.b.Cursor()
	if row < e.scroll {
		e.scroll = row
	} else if row >= e.scroll+textRows {
		e.scroll = row - textRows + 1
	}
	f := render.Frame{
		Buf:        e.b,
		Filename:   e.displayName(),
		Notice:     e.notice,
		Modified:   e.isModified(),
		Scroll:     e.scroll,
		ShowCursor: true,
		BottomRows: bottomRows,
	}
	if e.find != nil {
		// The find bar draws its own cursor in the focused field, and there is
		// only one terminal cursor to go around.
		f.ShowCursor = false
		f.Matches, f.Current = e.find.matches, e.find.cur
	}
	if e.pick != nil {
		f.ShowCursor = false // the picker's query line owns the cursor
	}
	if e.browser != nil {
		f.OriginX, f.ShowCursor = render.SidebarWidth, false
		render.Draw(e.s, f)
		render.DrawSidebar(e.s, e.browser)
	} else {
		render.Draw(e.s, f)
	}
	if e.find != nil {
		render.DrawFindBar(e.s, e.find.findBar())
	}
	if e.popup != nil {
		render.DrawCompletion(e.s, *e.popup)
	}
	// The picker is drawn last: it is a modal overlay and must sit on top.
	if e.pick != nil {
		render.DrawPicker(e.s, e.pick.picker())
	}
	// Leaving the splash: the OSC 8 link left an open hyperlink region. Force a
	// full repaint so tcell re-emits every cell, closing the link and clearing
	// any ghost underlines the terminal painted while it was open.
	if e.splashShown {
		e.splashShown = false
		e.s.Sync()
	}
}
