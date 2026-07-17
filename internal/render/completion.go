package render

import (
	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/completion"
)

// completionMaxRows caps the visible popup height.
const completionMaxRows = 8

// Popup is the completion list to draw, anchored at a screen cell.
type Popup struct {
	Items  []completion.Item
	Sel    int
	Anchor struct{ X, Y int }
}

// DrawCompletion renders p as a list below its anchor, flipping above when it
// would clip the bottom. The selected row is highlighted. A nil/empty popup
// draws nothing.
func DrawCompletion(s tcell.Screen, p Popup) {
	if len(p.Items) == 0 {
		return
	}
	width, height := s.Size()

	rows := len(p.Items)
	if rows > completionMaxRows {
		rows = completionMaxRows
	}
	// Scroll so the selection is visible.
	start := 0
	if p.Sel >= rows {
		start = p.Sel - rows + 1
	}

	boxW := 0
	for _, it := range p.Items {
		if l := len(it.Label); l > boxW {
			boxW = l
		}
	}
	boxW += 2 // padding
	if boxW > width {
		boxW = width
	}

	top := p.Anchor.Y + 1
	if top+rows > height { // flip above
		top = p.Anchor.Y - rows
	}
	if top < 0 {
		top = 0
	}
	x := p.Anchor.X
	if x+boxW > width {
		x = width - boxW
	}
	if x < 0 {
		x = 0
	}

	normal := tcell.StyleDefault.Reverse(true)
	sel := normal.Bold(true) // layer emphasis onto the box, don't replace it
	for i := 0; i < rows; i++ {
		idx := start + i
		st := normal
		if idx == p.Sel {
			st = sel
		}
		label := clipPad(p.Items[idx].Label, boxW)
		drawText(s, x, top+i, label, st)
	}
	s.Show()
}
