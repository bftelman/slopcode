package highlight

import (
	"strings"
	"testing"
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
