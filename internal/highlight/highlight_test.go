package highlight

import (
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/gdamore/tcell/v2"
)

func TestHighlightRoundTripsCharacters(t *testing.T) {
	src := "package main\n\nfunc main() {}\n"
	lines := Highlight(src, "main.go", "monokai")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		for _, sr := range line {
			b.WriteRune(sr.R)
		}
	}
	if b.String() != src {
		t.Fatalf("round trip mismatch:\n got %q\nwant %q", b.String(), src)
	}
}

func TestHighlightSplitsLines(t *testing.T) {
	lines := Highlight("a\nb\nc", "x.go", "monokai")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d", len(lines))
	}
}

func TestHighlightEmpty(t *testing.T) {
	lines := Highlight("", "x.txt", "monokai")
	if len(lines) != 1 {
		t.Fatalf("want 1 line for empty input, got %d", len(lines))
	}
}

func TestStyleUsesThemeColor(t *testing.T) {
	// monokai's Keyword entry is #66d9ef; other UI surfaces (e.g. the
	// completion popup) share this so their colors track the active theme.
	got := Style(chroma.Keyword, "monokai")
	fg, _, _ := got.Decompose()
	want := tcell.NewRGBColor(0x66, 0xd9, 0xef)
	if fg != want {
		t.Fatalf("Keyword foreground = %v, want %v", fg, want)
	}
}

func TestStyleUnknownThemeFallsBack(t *testing.T) {
	// Must not panic and must still produce a usable style for an unknown
	// theme name (mirrors Highlight's existing fallback behavior).
	got := Style(chroma.Keyword, "definitely-not-a-real-theme")
	if got == tcell.StyleDefault {
		t.Fatalf("expected a resolved fallback style, got the zero value")
	}
}

func TestBackgroundStyleUsesThemeCanvas(t *testing.T) {
	// monokai's Background entry is bg:#272822 with no explicit foreground,
	// which chroma resolves to the Text entry's color (#f8f8f2).
	got := BackgroundStyle("monokai")
	fg, bg, _ := got.Decompose()
	wantBg := tcell.NewRGBColor(0x27, 0x28, 0x22)
	wantFg := tcell.NewRGBColor(0xf8, 0xf8, 0xf2)
	if bg != wantBg {
		t.Fatalf("background = %v, want %v", bg, wantBg)
	}
	if fg != wantFg {
		t.Fatalf("foreground = %v, want %v", fg, wantFg)
	}
}
