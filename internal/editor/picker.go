package editor

import (
	"os"
	"path/filepath"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/picker"
	"github.com/bftelman/slopcode/internal/render"
)

// pickerEvent carries a ranking Result into the tcell event loop, mirroring how
// completionEvent bridges the completion engine.
type pickerEvent struct {
	when time.Time
	res  picker.Result
}

func (p *pickerEvent) When() time.Time { return p.when }

// pickBridge forwards ranking results into the event loop as tcell events.
func (e *Editor) pickBridge() {
	for r := range e.pk.Results() {
		_ = e.s.PostEvent(&pickerEvent{when: time.Now(), res: r})
	}
}

// pickState is the picker overlay. It is nil when no picker is open.
type pickState struct {
	title string
	gen   int // generation this picker owns; results tagged otherwise are stale
	query string
	rows  []picker.Row
	sel   int
	total int
	err   error

	// loading is true while the candidate listing is still running, so the
	// overlay can say "searching" instead of "no matches".
	loading bool
}

// pickerRoot resolves the directory a file picker lists from: the nearest
// ancestor holding a .git, falling back to the current file's directory, or the
// process working directory for an unsaved [No Name] buffer.
func (e *Editor) pickerRoot() string {
	start := "."
	if e.path != "" {
		if dir := filepath.Dir(e.path); dir != "" {
			start = dir
		}
	} else if wd, err := os.Getwd(); err == nil {
		start = wd
	}
	return picker.GitRoot(start)
}

// openFilePicker opens the recursive file picker for the project root.
func (e *Editor) openFilePicker() {
	root := e.pickerRoot()
	e.startPicker(picker.Files(root))
}

// openLinePicker opens the fuzzy line picker for the current buffer.
func (e *Editor) openLinePicker() {
	e.startPicker(picker.Lines(e.b.Lines()))
}

// startPicker dismisses competing surfaces and hands src to the engine under a
// fresh generation.
func (e *Editor) startPicker(src picker.Source) {
	e.dismissPopup()
	e.browser = nil
	e.pendingOpen = ""
	e.pickGen++
	e.pick = &pickState{title: src.Title(), gen: e.pickGen, loading: true}
	e.pk.Open(e.pickGen, src)
}

// closePicker dismisses the overlay.
func (e *Editor) closePicker() {
	e.pick = nil
	e.pendingOpen = ""
}

// applyPickResult installs a ranking result, dropping any that belongs to a
// picker the user has already closed or replaced.
func (e *Editor) applyPickResult(res picker.Result) {
	if e.pick == nil || res.Gen != e.pick.gen {
		return
	}
	p := e.pick
	p.rows, p.total, p.err, p.loading = res.Rows, res.Total, res.Err, res.Loading
	if p.sel >= len(p.rows) {
		p.sel = len(p.rows) - 1
	}
	if p.sel < 0 {
		p.sel = 0
	}
}

// handlePickKey routes a key while the picker is open. It returns true when the
// editor should quit.
func (e *Editor) handlePickKey(ev *tcell.EventKey) bool {
	p := e.pick
	switch ev.Key() {
	case tcell.KeyCtrlQ:
		return e.tryQuit()
	case tcell.KeyCtrlS:
		e.save()
	case tcell.KeyEscape:
		e.closePicker()
	case tcell.KeyEnter:
		e.acceptPick()
	case tcell.KeyUp, tcell.KeyCtrlP:
		if p.sel > 0 {
			p.sel--
		}
	case tcell.KeyDown, tcell.KeyCtrlN:
		if p.sel < len(p.rows)-1 {
			p.sel++
		}
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if p.query != "" {
			rs := []rune(p.query)
			p.query = string(rs[:len(rs)-1])
			e.requery()
		}
	case tcell.KeyRune:
		p.query += string(ev.Rune())
		e.requery()
	}
	return false
}

// requery asks the engine to re-rank, and resets the selection to the top since
// the previous selection's row is about to be replaced.
func (e *Editor) requery() {
	e.pick.sel = 0
	e.pk.Query(e.pick.query)
}

// acceptPick acts on the selected row: open a file, or jump to a line.
func (e *Editor) acceptPick() {
	p := e.pick
	if p.sel < 0 || p.sel >= len(p.rows) {
		return
	}
	cand := p.rows[p.sel].Cand

	// A line candidate only moves the cursor, so it needs no unsaved-changes
	// guard - it does not replace the buffer.
	if cand.Path == "" {
		e.b.SetCursor(cand.Row, 0)
		e.closePicker()
		return
	}

	// Opening a different file discards unsaved edits, so reuse the browser's
	// two-step latch rather than inventing a second confirmation style.
	if e.isModified() && e.pendingOpen != cand.Path {
		e.pendingOpen = cand.Path
		e.notice = "unsaved changes — Ctrl+S, or Enter again to discard"
		return
	}
	if err := e.openPath(cand.Path); err != nil {
		e.notice = "OPEN ERROR: " + err.Error()
		return
	}
	e.notice = cand.Path + " opened"
	e.closePicker()
}

// picker projects the overlay state onto its render value.
func (p *pickState) picker() render.Picker {
	return render.Picker{
		Title:   p.title,
		Query:   p.query,
		Rows:    p.rows,
		Sel:     p.sel,
		Total:   p.total,
		Err:     p.err,
		Loading: p.loading,
	}
}
