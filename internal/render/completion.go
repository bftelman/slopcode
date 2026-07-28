package render

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/completion"
	"github.com/bftelman/slopcode/internal/highlight"
)

// completionMaxRows caps the visible popup height.
const completionMaxRows = 8

// kindTagWidth is the fixed width of each row's leading kind tag ("[F] ", or
// 4 spaces for a kind with no tag). Every row is padded to this width so
// labels line up in a column regardless of kind.
const kindTagWidth = 4

// kindIcon is the single-letter tag shown before a completion item's label,
// keyed by completion.Kind. This is the only place to edit to change an
// icon; a kind absent from this map gets no tag (just padding).
var kindIcon = map[completion.Kind]string{
	completion.KindFunction: "F",
	completion.KindVariable: "V",
	completion.KindConstant: "C",
	completion.KindField:    "P", // property/field
	completion.KindKeyword:  "K",
	completion.KindType:     "T", // class/struct/interface
	completion.KindModule:   "M", // package/namespace
}

// kindToken maps a completion.Kind to the chroma token type whose color (in
// the active syntax-highlight theme, StyleName) accents that kind's icon.
// This is the only place to edit to change a kind's color; swapping
// StyleName re-themes both syntax highlighting and this popup together.
var kindToken = map[completion.Kind]chroma.TokenType{
	completion.KindFunction: chroma.NameFunction,
	completion.KindVariable: chroma.NameVariable,
	completion.KindConstant: chroma.NameConstant,
	completion.KindField:    chroma.NameAttribute,
	completion.KindKeyword:  chroma.Keyword,
	completion.KindType:     chroma.NameClass,
	completion.KindModule:   chroma.NameNamespace,
}

// kindTag returns the fixed-width leading tag for k ("[F] ", or kindTagWidth
// spaces if k has no icon).
func kindTag(k completion.Kind) string {
	icon, ok := kindIcon[k]
	if !ok {
		return "    "
	}
	return "[" + icon + "] "
}

// Popup is the completion list to draw, anchored at a screen cell.
type Popup struct {
	Items  []completion.Item
	Sel    int
	Anchor struct{ X, Y int }
}

// DrawCompletion renders p as a list below its anchor, flipping above when it
// would clip the bottom. Colors follow the active syntax-highlight theme
// (StyleName): each row uses the theme's canvas background and default text
// color, the selected row inverts them, and each item's kind tag is accented
// with that kind's token color. A nil/empty popup draws nothing.
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
		if l := len(it.Label) + kindTagWidth; l > boxW {
			boxW = l
		}
	}
	boxW += 1 // trailing padding
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

	normal := highlight.BackgroundStyle(StyleName)
	sel := normal.Reverse(true).Bold(true)
	for i := 0; i < rows; i++ {
		idx := start + i
		it := p.Items[idx]
		rowStyle := normal
		if idx == p.Sel {
			rowStyle = sel
		}
		tag := kindTag(it.Kind)
		drawText(s, x, top+i, clipPad(tag+it.Label, boxW), rowStyle)
		// Accent the tag with the kind's theme color, but only on unselected
		// rows — the selection bar's inversion already carries the emphasis,
		// and layering a second color onto it would fight for attention.
		if idx != p.Sel {
			if tok, ok := kindToken[it.Kind]; ok {
				fg, _, _ := highlight.Style(tok, StyleName).Decompose()
				drawText(s, x, top+i, tag, rowStyle.Foreground(fg))
			}
		}
	}
	s.Show()
}
