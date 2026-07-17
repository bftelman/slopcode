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

	// Row below the anchor holds the first item.
	if got := cellText(s, 2, 4, 5); got != "alpha" {
		t.Fatalf("row 4 = %q, want alpha", got)
	}
	if got := cellText(s, 2, 5, 4); got != "beta" {
		t.Fatalf("row 5 = %q, want beta", got)
	}
}
