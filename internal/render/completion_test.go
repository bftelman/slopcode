package render

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/completion"
)

func cellText(s tcell.SimulationScreen, x, y, n int) string {
	cells, _, _ := s.GetContents()
	w, _ := s.Size()
	out := make([]rune, 0, n)
	for i := 0; i < n; i++ {
		c := cells[y*w+x+i]
		if len(c.Runes) > 0 {
			out = append(out, c.Runes[0])
		}
	}
	return string(out)
}

func TestDrawCompletionListsItemsBelowAnchor(t *testing.T) {
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.SetSize(40, 10)

	p := Popup{Items: []completion.Item{{Label: "alpha"}, {Label: "beta"}}, Sel: 1}
	p.Anchor.X, p.Anchor.Y = 2, 3
	DrawCompletion(s, p)

	// Each row is prefixed with a 4-column kind tag ("[X] " or 4 spaces for
	// an untagged kind); labels start right after it.
	if got := cellText(s, 2+kindTagWidth, 4, 5); got != "alpha" {
		t.Fatalf("row 4 = %q, want alpha", got)
	}
	if got := cellText(s, 2+kindTagWidth, 5, 4); got != "beta" {
		t.Fatalf("row 5 = %q, want beta", got)
	}
}

func TestDrawCompletionShowsKindTag(t *testing.T) {
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.SetSize(40, 10)

	p := Popup{Items: []completion.Item{
		{Label: "Println", Kind: completion.KindFunction},
		{Label: "MaxInt", Kind: completion.KindConstant},
		{Label: "x", Kind: completion.KindText},
	}, Sel: -1}
	p.Anchor.X, p.Anchor.Y = 0, 0
	DrawCompletion(s, p)

	if got := cellText(s, 0, 1, kindTagWidth); got != "[F] " {
		t.Fatalf("function tag = %q, want %q", got, "[F] ")
	}
	if got := cellText(s, 0, 2, kindTagWidth); got != "[C] " {
		t.Fatalf("constant tag = %q, want %q", got, "[C] ")
	}
	if got := cellText(s, 0, 3, kindTagWidth); got != "    " {
		t.Fatalf("untagged kind = %q, want 4 spaces", got)
	}
}

func TestDrawCompletionSelectedRowIsInverted(t *testing.T) {
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.SetSize(40, 10)

	p := Popup{Items: []completion.Item{{Label: "alpha"}, {Label: "beta"}}, Sel: 1}
	p.Anchor.X, p.Anchor.Y = 0, 0
	DrawCompletion(s, p)

	cells, _, _ := s.GetContents()
	w, _ := s.Size()
	normalStyle := cells[1*w+0].Style
	selStyle := cells[2*w+0].Style
	if normalStyle == selStyle {
		t.Fatalf("selected row style must differ from normal row style")
	}
	if _, _, attrs := selStyle.Decompose(); attrs&tcell.AttrReverse == 0 {
		t.Fatalf("selected row must carry the Reverse attribute, got attrs=%v", attrs)
	}
}
