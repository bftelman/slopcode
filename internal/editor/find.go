package editor

import (
	"fmt"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/render"
	"github.com/bftelman/slopcode/internal/textsearch"
)

// findState is the find/replace prompt. It is nil when the bar is closed.
type findState struct {
	query   string
	repl    string
	replace bool // the replace field has been revealed
	onRepl  bool // focus is in the replace field

	matches []textsearch.Match
	cur     int // index into matches; -1 when there are none

	// origRow/origCol is where the cursor was when the bar opened. It is both
	// the anchor for incremental search - so the selection does not drift
	// forward as more characters are typed - and where Esc restores to.
	origRow, origCol int
}

// openFind opens the find bar anchored at the current cursor position.
func (e *Editor) openFind() {
	e.dismissPopup()
	row, col := e.b.Cursor()
	e.find = &findState{cur: -1, origRow: row, origCol: col}
}

// closeFind closes the bar, restoring the original cursor position when the
// prompt was cancelled rather than accepted.
func (e *Editor) closeFind(restore bool) {
	if e.find == nil {
		return
	}
	if restore {
		e.b.SetCursor(e.find.origRow, e.find.origCol)
	}
	e.find = nil
}

// research recomputes the match list for the current query and reselects from
// the anchor. Called on every edit to either field.
func (e *Editor) research() {
	fs := e.find
	fs.matches = textsearch.FindAll(e.b.Lines(), fs.query)
	fs.cur = textsearch.NearestFrom(fs.matches, fs.origRow, fs.origCol)
	e.gotoCurrentMatch()
}

// gotoCurrentMatch parks the cursor on the selected match so draw() scrolls it
// into view. It is a no-op when there is no selection.
func (e *Editor) gotoCurrentMatch() {
	fs := e.find
	if fs.cur < 0 || fs.cur >= len(fs.matches) {
		return
	}
	m := fs.matches[fs.cur]
	e.b.SetCursor(m.Row, m.Col)
}

// handleFindKey routes a key while the find bar is open. It returns true when
// the editor should quit.
func (e *Editor) handleFindKey(ev *tcell.EventKey) bool {
	fs := e.find
	switch ev.Key() {
	case tcell.KeyCtrlQ:
		return e.tryQuit()
	case tcell.KeyCtrlS:
		e.save()
	case tcell.KeyEscape:
		e.closeFind(true)
	case tcell.KeyEnter:
		e.closeFind(false) // accept: stay on the current match
	case tcell.KeyTab:
		if !fs.replace {
			fs.replace, fs.onRepl = true, true
		} else {
			fs.onRepl = !fs.onRepl
		}
	case tcell.KeyCtrlN, tcell.KeyDown:
		e.stepMatch(true)
	case tcell.KeyCtrlP, tcell.KeyUp:
		e.stepMatch(false)
	case tcell.KeyCtrlR:
		e.replaceCurrent()
	case tcell.KeyCtrlA:
		e.replaceAll()
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		e.editField(0, true)
	case tcell.KeyRune:
		e.editField(ev.Rune(), false)
	}
	return false
}

// editField appends r to, or deletes the last rune of, the focused field.
func (e *Editor) editField(r rune, del bool) {
	fs := e.find
	edit := func(s string) string {
		if del {
			if s == "" {
				return s
			}
			rs := []rune(s)
			return string(rs[:len(rs)-1])
		}
		return s + string(r)
	}
	if fs.onRepl && fs.replace {
		fs.repl = edit(fs.repl)
		return // the replacement text does not affect the match list
	}
	fs.query = edit(fs.query)
	e.research()
}

// stepMatch moves the selection to the next or previous match, wrapping.
func (e *Editor) stepMatch(forward bool) {
	fs := e.find
	if len(fs.matches) == 0 {
		return
	}
	row, col := e.b.Cursor()
	if forward {
		fs.cur = textsearch.NextFrom(fs.matches, row, col)
	} else {
		fs.cur = textsearch.PrevFrom(fs.matches, row, col)
	}
	e.gotoCurrentMatch()
}

// replaceCurrent replaces the selected match and advances to the next one.
// Each call is its own undo step.
func (e *Editor) replaceCurrent() {
	fs := e.find
	if !fs.replace {
		e.notice = "press Tab to open the replace field first"
		return
	}
	if fs.cur < 0 || fs.cur >= len(fs.matches) {
		return
	}
	m := fs.matches[fs.cur]
	e.b.Checkpoint()
	e.b.ReplaceRange(m.Row, m.Col, m.Len, fs.repl)
	e.afterReplace()

	// Re-search: the replacement text may itself contain the query, and every
	// later match on the line has shifted. Advance with NearestForward, which
	// does not wrap - the cursor sits just past the inserted text, and wrapping
	// would select the replacement's own output, so repeated Ctrl+R on a
	// query-containing replacement ("foo" -> "foofoo") would never terminate.
	fs.matches = textsearch.FindAll(e.b.Lines(), fs.query)
	row, col := e.b.Cursor()
	fs.cur = textsearch.NearestForward(fs.matches, row, col)
	e.gotoCurrentMatch()
}

// replaceAll replaces every match under a single checkpoint, so one Undo
// reverts the whole sweep.
func (e *Editor) replaceAll() {
	fs := e.find
	if !fs.replace {
		e.notice = "press Tab to open the replace field first"
		return
	}
	if len(fs.matches) == 0 {
		return
	}
	n := len(fs.matches)
	e.b.Checkpoint()
	// Reverse document order: replacing from the end means the offsets of the
	// matches still to be applied are unaffected by earlier edits.
	for i := n - 1; i >= 0; i-- {
		m := fs.matches[i]
		e.b.ReplaceRange(m.Row, m.Col, m.Len, fs.repl)
	}
	e.afterReplace()
	e.notice = fmt.Sprintf("replaced %d %s", n, plural(n, "occurrence", "occurrences"))

	// Same non-wrapping rule as replaceCurrent: normally nothing is left, but a
	// replacement containing the query leaves its own output behind, and the
	// selection must not jump backwards onto it.
	fs.matches = textsearch.FindAll(e.b.Lines(), fs.query)
	row, col := e.b.Cursor()
	fs.cur = textsearch.NearestForward(fs.matches, row, col)
	e.gotoCurrentMatch()
}

// afterReplace records a buffer mutation: bump the document version and push
// the new text to the completion provider, or it completes against stale text.
func (e *Editor) afterReplace() {
	e.docVersion++
	if e.path != "" {
		e.eng.SyncOnly(e.document())
	}
}

// findBar projects the prompt state onto its render value.
func (fs *findState) findBar() render.FindBar {
	cur := fs.cur
	if cur < 0 {
		cur = 0
	}
	return render.FindBar{
		Query:   fs.query,
		Repl:    fs.repl,
		Replace: fs.replace,
		OnRepl:  fs.onRepl,
		Total:   len(fs.matches),
		Current: cur,
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
