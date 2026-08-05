package editor

import (
	"fmt"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/namlet/internal/buffer"
)

func benchLines(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("\tif col := screenCol(line%d, byteCol, tabWidth); col > 0 {", i)
	}
	return out
}

// BenchmarkCursorMoveDraw measures one keystroke of cursor movement end to end:
// handleKey plus the repaint it triggers. This is the path that felt sluggish.
func BenchmarkCursorMoveDraw(b *testing.B) {
	for _, n := range []int{500, 2000, 8000} {
		b.Run(fmt.Sprintf("%dlines", n), func(b *testing.B) {
			s := tcell.NewSimulationScreen("")
			if err := s.Init(); err != nil {
				b.Fatal(err)
			}
			defer s.Fini()
			s.SetSize(120, 40)
			e := New(s, buffer.New(benchLines(n)), "bench.go")
			e.draw() // warm the highlight cache

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if i%2 == 0 {
					e.handleKey(keyEvent(tcell.KeyDown))
				} else {
					e.handleKey(keyEvent(tcell.KeyUp))
				}
				e.draw()
			}
		})
	}
}

// BenchmarkDrawOnly isolates the repaint from key handling.
func BenchmarkDrawOnly(b *testing.B) {
	for _, n := range []int{500, 2000, 8000} {
		b.Run(fmt.Sprintf("%dlines", n), func(b *testing.B) {
			s := tcell.NewSimulationScreen("")
			if err := s.Init(); err != nil {
				b.Fatal(err)
			}
			defer s.Fini()
			s.SetSize(120, 40)
			e := New(s, buffer.New(benchLines(n)), "bench.go")
			e.draw()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				e.draw()
			}
		})
	}
}

// BenchmarkIsModified isolates the modified check, which walks every line and
// runs twice per repaint.
func BenchmarkIsModified(b *testing.B) {
	for _, n := range []int{500, 2000, 8000} {
		b.Run(fmt.Sprintf("%dlines", n), func(b *testing.B) {
			s := tcell.NewSimulationScreen("")
			if err := s.Init(); err != nil {
				b.Fatal(err)
			}
			defer s.Fini()
			e := New(s, buffer.New(benchLines(n)), "bench.go")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = e.isModified()
			}
		})
	}
}
