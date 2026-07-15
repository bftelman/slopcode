package render

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/buffer"
	"github.com/bftelman/slopcode/internal/filebrowser"
)

func TestScreenColNoTabs(t *testing.T) {
	if got := screenCol("abc", 2, 4); got != 2 {
		t.Fatalf("want 2 got %d", got)
	}
}

func TestScreenColLeadingTab(t *testing.T) {
	if got := screenCol("\tx", 1, 4); got != 4 {
		t.Fatalf("want 4 got %d", got)
	}
}

func TestScreenColTabAfterTwoChars(t *testing.T) {
	// "ab\t": a(0->1) b(1->2) tab(2->4)
	if got := screenCol("ab\t", 3, 4); got != 4 {
		t.Fatalf("want 4 got %d", got)
	}
}

// newSimScreen makes a simulation screen for end-to-end Draw assertions.
func newSimScreen(t *testing.T, w, h int) tcell.SimulationScreen {
	t.Helper()
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatalf("init sim screen: %v", err)
	}
	s.SetSize(w, h)
	return s
}

func cellAt(cells []tcell.SimCell, width, x, y int) tcell.SimCell {
	return cells[y*width+x]
}

// TestDrawHighlightsKeyword verifies that Go source produces colored cells.
func TestDrawHighlightsKeyword(t *testing.T) {
	s := newSimScreen(t, 80, 24)
	defer s.Fini()
	b := buffer.New([]string{"func main() {}"})

	Draw(s, b, "t.go", "", false, 0, 0, true)

	cells, width, _ := s.GetContents()
	gw := GutterWidth(b.LineCount())
	colored := false
	for x := gw; x < width; x++ {
		fg, _, _ := cellAt(cells, width, x, 1).Style.Decompose()
		if fg != tcell.ColorDefault {
			colored = true
			break
		}
	}
	if !colored {
		t.Fatal("expected at least one colored (highlighted) cell in the text row")
	}
}

// TestDrawExpandsTab verifies a literal tab renders as spaces to the tab stop.
func TestDrawExpandsTab(t *testing.T) {
	s := newSimScreen(t, 80, 24)
	defer s.Fini()
	b := buffer.New([]string{"\tx"})

	Draw(s, b, "t.txt", "", false, 0, 0, true)

	cells, width, _ := s.GetContents()
	gw := GutterWidth(b.LineCount())
	// Tab occupies gw..gw+TabWidth-1 as spaces; 'x' lands at gw+TabWidth.
	for x := gw; x < gw+TabWidth; x++ {
		if r := cellAt(cells, width, x, 1).Runes; len(r) != 1 || r[0] != ' ' {
			t.Fatalf("cell (%d,1) = %q, want space", x, r)
		}
	}
	if r := cellAt(cells, width, gw+TabWidth, 1).Runes; len(r) != 1 || r[0] != 'x' {
		t.Fatalf("cell (%d,1) = %q, want 'x'", gw+TabWidth, r)
	}
}

// TestDrawShowsNotice verifies the notice text appears in the statusbar row.
func TestDrawShowsNotice(t *testing.T) {
	s := newSimScreen(t, 80, 24)
	defer s.Fini()
	b := buffer.New([]string{"hi"})

	Draw(s, b, "t.txt", "t.txt saved", false, 0, 0, true)

	cells, width, _ := s.GetContents()
	var row0 []rune
	for x := 0; x < width; x++ {
		r := cellAt(cells, width, x, 0).Runes
		if len(r) == 1 {
			row0 = append(row0, r[0])
		} else {
			row0 = append(row0, ' ')
		}
	}
	if got := string(row0); !contains(got, "t.txt saved") {
		t.Fatalf("statusbar row %q missing notice", got)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestDrawClearsStaleTrailingText verifies that shrinking a line blanks the
// leftover cells even though Draw no longer calls Screen.Clear().
func TestDrawClearsStaleTrailingText(t *testing.T) {
	s := newSimScreen(t, 80, 24)
	defer s.Fini()

	Draw(s, buffer.New([]string{"hello"}), "t.txt", "", false, 0, 0, true)
	Draw(s, buffer.New([]string{"hi"}), "t.txt", "", false, 0, 0, true)

	cells, width, _ := s.GetContents()
	gw := GutterWidth(1)
	// "hi" occupies gw..gw+1; the old "llo" tail at gw+2.. must be blank now.
	for _, x := range []int{gw + 2, gw + 3, gw + 4} {
		if r := cellAt(cells, width, x, 1).Runes; len(r) == 1 && r[0] != ' ' {
			t.Fatalf("stale content at col %d: %q", x, r)
		}
	}
	// Sanity: the new text is present.
	if r := cellAt(cells, width, gw, 1).Runes; len(r) != 1 || r[0] != 'h' {
		t.Fatalf("expected 'h' at col %d, got %q", gw, r)
	}
}

func TestDrawSplashShowsBanner(t *testing.T) {
	s := newSimScreen(t, 80, 24)
	defer s.Fini()
	DrawSplash(s, "[No Name]", "")

	cells, width, height := s.GetContents()
	blocks, subtitle := false, false
	for y := 0; y < height; y++ {
		var row []rune
		for x := 0; x < width; x++ {
			r := cellAt(cells, width, x, y).Runes
			if len(r) == 1 {
				row = append(row, r[0])
				if r[0] == '█' {
					blocks = true
				}
			} else {
				row = append(row, ' ')
			}
		}
		if contains(string(row), "bftelman") {
			subtitle = true
		}
	}
	if !blocks {
		t.Fatal("splash should render block banner")
	}
	if !subtitle {
		t.Fatal("splash should show subtitle by @bftelman")
	}
}

func TestSplashHandleIsHyperlink(t *testing.T) {
	s := newSimScreen(t, 80, 24)
	defer s.Fini()
	DrawSplash(s, "[No Name]", "")

	linkStyle := tcell.StyleDefault.Foreground(tcell.ColorAqua).Underline(true).Url(githubURL)
	cells, width, height := s.GetContents()
	foundAt := false
	for y := 0; y < height && !foundAt; y++ {
		for x := 0; x < width; x++ {
			c := cellAt(cells, width, x, y)
			if len(c.Runes) == 1 && c.Runes[0] == '@' {
				if c.Style != linkStyle {
					t.Fatalf("@ cell should carry the github link style")
				}
				// The 'b' in "by " just before should NOT be a link.
				prev := cellAt(cells, width, x-3, y)
				if prev.Style == linkStyle {
					t.Fatal("prefix 'by ' should not be a link")
				}
				foundAt = true
				break
			}
		}
	}
	if !foundAt {
		t.Fatal("expected @bftelman handle in splash")
	}
}

func TestDrawTextMultibyteColumns(t *testing.T) {
	s := newSimScreen(t, 20, 3)
	defer s.Fini()
	drawText(s, 0, 0, "█x", tcell.StyleDefault)
	s.Show()
	cells, width, _ := s.GetContents()
	if r := cellAt(cells, width, 0, 0).Runes; len(r) != 1 || r[0] != '█' {
		t.Fatalf("cell 0 = %q, want block", r)
	}
	if r := cellAt(cells, width, 1, 0).Runes; len(r) != 1 || r[0] != 'x' {
		t.Fatalf("cell 1 = %q, want x at column 1", r)
	}
}

// TestDrawSidebarShowsEntries verifies the browser panel lists directory entries.
func TestDrawSidebarShowsEntries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	br, err := filebrowser.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := newSimScreen(t, 80, 24)
	defer s.Fini()
	DrawSidebar(s, br)

	cells, width, height := s.GetContents()
	found := false
	for y := 0; y < height; y++ {
		var row []rune
		for x := 0; x < SidebarWidth-1; x++ {
			r := cellAt(cells, width, x, y).Runes
			if len(r) == 1 {
				row = append(row, r[0])
			} else {
				row = append(row, ' ')
			}
		}
		if contains(string(row), "hello.txt") {
			found = true
		}
	}
	if !found {
		t.Fatal("sidebar should list hello.txt")
	}
}
