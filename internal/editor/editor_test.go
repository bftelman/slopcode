package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/buffer"
	"github.com/bftelman/slopcode/internal/fileio"
)

// TestRunTypesEditsAndSaves drives the real event loop through a simulation
// screen: type "hi", newline, "there", Ctrl+S to save, Ctrl+Q to quit.
func TestRunTypesEditsAndSaves(t *testing.T) {
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatalf("init sim screen: %v", err)
	}
	defer s.Fini()
	s.SetSize(80, 24)

	path := filepath.Join(t.TempDir(), "out.txt")
	b := buffer.New(nil)
	e := New(s, b, path)

	for _, r := range "hi" {
		s.InjectKey(tcell.KeyRune, r, tcell.ModNone)
	}
	s.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	for _, r := range "there" {
		s.InjectKey(tcell.KeyRune, r, tcell.ModNone)
	}
	s.InjectKey(tcell.KeyCtrlS, 0, tcell.ModNone)
	s.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone)

	e.Run() // returns when Ctrl+Q is handled

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(got) != "hi\nthere\n" {
		t.Fatalf("saved content = %q, want %q", string(got), "hi\nthere\n")
	}

	// Buffer should also reflect the edits and clear the modified flag on save.
	lines, _ := fileio.Load(path)
	if len(lines) != 2 || lines[0] != "hi" || lines[1] != "there" {
		t.Fatalf("reloaded lines = %#v", lines)
	}
	if e.modified {
		t.Fatalf("expected modified=false after save")
	}
}

// TestRunBackspaceJoinsLines verifies Backspace at column 0 joins lines.
func TestRunBackspaceJoinsLines(t *testing.T) {
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatalf("init sim screen: %v", err)
	}
	defer s.Fini()
	s.SetSize(80, 24)

	b := buffer.New([]string{"ab", "cd"})
	e := New(s, b, filepath.Join(t.TempDir(), "x.txt"))

	// Move down to row 1 col 0, then backspace to join.
	s.InjectKey(tcell.KeyDown, 0, tcell.ModNone)
	s.InjectKey(tcell.KeyBackspace2, 0, tcell.ModNone)
	s.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone)

	e.Run()

	if lines := b.Lines(); len(lines) != 1 || lines[0] != "abcd" {
		t.Fatalf("lines = %#v, want [abcd]", b.Lines())
	}
}
