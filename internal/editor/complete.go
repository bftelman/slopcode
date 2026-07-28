package editor

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/completion"
	"github.com/bftelman/slopcode/internal/render"
)

// completionEvent carries an engine Result into the tcell event loop.
type completionEvent struct {
	when time.Time
	res  completion.Result
}

func (c *completionEvent) When() time.Time { return c.when }

// bridge forwards engine results into the event loop as tcell events.
func (e *Editor) bridge() {
	for r := range e.eng.Results() {
		_ = e.s.PostEvent(&completionEvent{when: time.Now(), res: r})
	}
}

// document snapshots the buffer as an immutable completion.Document.
func (e *Editor) document() completion.Document {
	return completion.Document{
		URI:     e.uri(),
		LangID:  "go",
		Text:    strings.Join(e.b.Lines(), "\n"),
		Version: e.docVersion,
	}
}

// uri returns a file:// URI for the current path (empty path -> empty URI).
func (e *Editor) uri() string {
	if e.path == "" {
		return ""
	}
	abs, err := filepath.Abs(e.path)
	if err != nil {
		abs = e.path
	}
	return completion.PathToFileURI(abs)
}

// cursorPos returns the cursor as a completion.Position (byte column).
func (e *Editor) cursorPos() completion.Position {
	row, col := e.b.Cursor()
	return completion.Position{Line: row, Character: col}
}

// requestCompletion asks the engine for completions when r warrants it.
func (e *Editor) requestCompletion(r rune) {
	if e.path == "" || filepath.Ext(e.path) == "" {
		return // no route without an on-disk extension
	}
	if !shouldTrigger(r) {
		e.dismissPopup()
		return
	}
	e.eng.Update(e.document(), e.cursorPos())
}

// applyResult shows the popup for a fresh, non-empty result; drops stale ones.
func (e *Editor) applyResult(res completion.Result) {
	if res.Version != e.docVersion || res.Err != nil || len(res.Items) == 0 {
		e.dismissPopup()
		return
	}
	p := &render.Popup{Items: res.Items, Sel: 0}
	row, col := e.b.Cursor()
	gw := render.GutterWidth(e.b.LineCount())
	scrCol := col
	if lines := e.b.Lines(); row >= 0 && row < len(lines) {
		// Match Draw's own cursor placement (render.go): byteCol is not the
		// screen column once tabs or multi-byte runes are involved.
		scrCol = render.ScreenCol(lines[row], col, render.TabWidth)
	}
	p.Anchor.X = gw + scrCol
	p.Anchor.Y = row - e.scroll + 1
	e.popupMu.Lock()
	e.popup = p
	e.popupMu.Unlock()
}

func (e *Editor) dismissPopup() {
	if e.popup != nil {
		e.popupMu.Lock()
		e.popup = nil
		e.popupMu.Unlock()
		e.eng.Cancel()
	}
}

// handlePopupKey routes a key while the popup is open. Returns true if consumed.
func (e *Editor) handlePopupKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyUp:
		if e.popup.Sel > 0 {
			e.popup.Sel--
		}
		return true
	case tcell.KeyDown:
		if e.popup.Sel < len(e.popup.Items)-1 {
			e.popup.Sel++
		}
		return true
	case tcell.KeyEnter, tcell.KeyTab:
		e.acceptCompletion()
		return true
	case tcell.KeyEscape:
		e.dismissPopup()
		return true
	}
	return false // fall through to normal editing
}

// acceptCompletion replaces the current word with the selected item's insert
// text and closes the popup.
func (e *Editor) acceptCompletion() {
	item := e.popup.Items[e.popup.Sel]
	row, col := e.b.Cursor()
	line := e.b.Lines()[row]
	start := wordStart(line, col)
	e.b.Checkpoint()
	// Delete the existing prefix, then insert the full text.
	for i := 0; i < col-start; i++ {
		e.b.Backspace()
	}
	for _, r := range item.Insert {
		e.b.InsertRune(r)
	}
	e.docVersion++
	e.dismissPopup()
	e.eng.SyncOnly(e.document()) // keep server in sync after accept
}

// popupOpenForTest reports whether the completion popup is visible. It is
// polled from a test goroutine while Run() mutates e.popup on its own
// goroutine (via applyResult/dismissPopup), so it goes through popupMu —
// the same lock those setters take — to stay race-safe under `-race`.
func (e *Editor) popupOpenForTest() bool {
	e.popupMu.Lock()
	defer e.popupMu.Unlock()
	return e.popup != nil
}

// popupAnchorForTest exposes the popup's anchor; (-1, -1) if closed. Guarded
// by popupMu like popupOpenForTest, for the same cross-goroutine reason.
func (e *Editor) popupAnchorForTest() (x, y int) {
	e.popupMu.Lock()
	defer e.popupMu.Unlock()
	if e.popup == nil {
		return -1, -1
	}
	return e.popup.Anchor.X, e.popup.Anchor.Y
}

// identChar reports whether r is part of an identifier ([A-Za-z0-9_]).
func identChar(r rune) bool {
	return r == '_' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}

// wordStart returns the byte index where the identifier ending at col begins.
// col is a byte offset into line. The scan stops at the first non-identifier
// byte (ASCII-correct; see the R4 limitation).
func wordStart(line string, col int) int {
	i := col
	for i > 0 && identChar(rune(line[i-1])) {
		i--
	}
	return i
}

// shouldTrigger reports whether typing r should request completions.
func shouldTrigger(r rune) bool {
	return identChar(r) || r == '.'
}
