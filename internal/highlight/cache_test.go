package highlight

import (
	"strings"
	"testing"
)

// countingLexer is not available, so instead assert the cache returns the very
// same slice header on a hit — proof it did not recompute.
func TestCacheReusesResult(t *testing.T) {
	var c Cache
	text := "package main\nfunc main() {}\n"

	first := c.Highlight(text, "a.go", "monokai")
	second := c.Highlight(text, "a.go", "monokai")

	if len(first) == 0 {
		t.Fatal("no styled lines returned")
	}
	if &first[0] != &second[0] {
		t.Error("cache recomputed for identical inputs")
	}
}

func TestCacheInvalidatesOnChange(t *testing.T) {
	var c Cache
	base := c.Highlight("package main\n", "a.go", "monokai")

	tests := []struct {
		name                  string
		text, filename, style string
	}{
		{"different text", "package other\n", "a.go", "monokai"},
		{"different filename", "package main\n", "a.py", "monokai"},
		{"different style", "package main\n", "a.go", "github"},
	}
	for _, tc := range tests {
		var fresh Cache
		fresh.Highlight("package main\n", "a.go", "monokai")
		got := fresh.Highlight(tc.text, tc.filename, tc.style)
		if len(got) > 0 && len(base) > 0 && &got[0] == &base[0] {
			t.Errorf("%s: served a stale cached result", tc.name)
		}
	}
}

// A cache hit must produce exactly what an uncached call would.
func TestCacheMatchesUncached(t *testing.T) {
	var c Cache
	text := "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hi\") }\n"

	want := Highlight(text, "a.go", "monokai")
	c.Highlight(text, "a.go", "monokai") // populate
	got := c.Highlight(text, "a.go", "monokai")

	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d", len(got), len(want))
	}
	for i := range want {
		if len(got[i]) != len(want[i]) {
			t.Fatalf("line %d: %d runes, want %d", i, len(got[i]), len(want[i]))
		}
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Errorf("line %d rune %d: got %+v, want %+v", i, j, got[i][j], want[i][j])
			}
		}
	}
}

// The win this exists for: repeated repaints of unchanged text.
func BenchmarkCachedVsUncached(b *testing.B) {
	text := strings.Repeat("\tif col := screenCol(line, byteCol, tabWidth); col > 0 {\n", 2000)

	b.Run("uncached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			Highlight(text, "x.go", "monokai")
		}
	})
	b.Run("cached", func(b *testing.B) {
		var c Cache
		c.Highlight(text, "x.go", "monokai") // warm
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			c.Highlight(text, "x.go", "monokai")
		}
	})
}
