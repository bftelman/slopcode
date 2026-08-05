package editor

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/buffer"
)

// countingScreen counts Show calls. It embeds SimulationScreen so it still
// satisfies that interface for tests that reach for it.
type countingScreen struct {
	tcell.SimulationScreen
	shows int
}

func (c *countingScreen) Show() {
	c.shows++
	c.SimulationScreen.Show()
}

func newCountingEditor(t *testing.T, lines []string, path string) (*Editor, *countingScreen) {
	t.Helper()
	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatalf("init sim screen: %v", err)
	}
	t.Cleanup(sim.Fini)
	sim.SetSize(80, 24)
	cs := &countingScreen{SimulationScreen: sim}
	return New(cs, buffer.New(lines), path), cs
}

// One repaint must produce exactly one flush, however many surfaces contributed
// to the frame. Flushing per-surface puts an intermediate, overlay-less frame on
// screen every repaint, which reads as a full-screen flicker on every keystroke.
func TestDrawFlushesExactlyOncePerFrame(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, e *Editor)
	}{
		{"plain text frame", func(*testing.T, *Editor) {}},
		{"splash screen", func(t *testing.T, e *Editor) {
			e.path = ""
			e.b = buffer.New(nil)
			e.baseline = cloneLines(e.b.Lines())
		}},
		{"find bar open", func(t *testing.T, e *Editor) {
			press(e, tcell.KeyCtrlF)
			typeText(e, "b")
		}},
		{"picker open", func(t *testing.T, e *Editor) {
			press(e, tcell.KeyCtrlG)
		}},
		{"browser open", func(t *testing.T, e *Editor) {
			press(e, tcell.KeyCtrlB)
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, cs := newCountingEditor(t, []string{"alpha", "beta", "gamma"}, "t.txt")
			tc.setup(t, e)

			cs.shows = 0
			e.draw()

			if cs.shows != 1 {
				t.Errorf("one draw() produced %d flushes, want exactly 1", cs.shows)
			}
		})
	}
}

// Moving the picker selection must also be one flush per repaint - this is the
// path the flicker was reported on (Ctrl+P, then moving the selection).
func TestPickerMovementFlushesOncePerFrame(t *testing.T) {
	e, cs := newCountingEditor(t, []string{"aa", "ab", "ac", "ad"}, "t.txt")

	press(e, tcell.KeyCtrlG)
	waitPickRows(t, e, func(p *pickState) bool { return len(p.rows) == 4 })

	for i, key := range []tcell.Key{tcell.KeyDown, tcell.KeyDown, tcell.KeyUp, tcell.KeyCtrlN} {
		cs.shows = 0
		e.handleKey(keyEvent(key))
		e.draw()
		if cs.shows != 1 {
			t.Errorf("move %d: %d flushes for one repaint, want 1", i, cs.shows)
		}
	}
}
