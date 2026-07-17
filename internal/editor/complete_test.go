package editor

import "testing"

func TestWordStart(t *testing.T) {
	cases := []struct {
		line string
		col  int
		want int
	}{
		{"fmt.Pri", 7, 4}, // after '.', word is "Pri"
		{"foo", 3, 0},
		{"  bar", 5, 2},
		{"", 0, 0},
		{"a.b.c", 5, 4},
	}
	for _, c := range cases {
		if got := wordStart(c.line, c.col); got != c.want {
			t.Errorf("wordStart(%q,%d) = %d, want %d", c.line, c.col, got, c.want)
		}
	}
}

func TestShouldTrigger(t *testing.T) {
	if !shouldTrigger('a') || !shouldTrigger('.') || !shouldTrigger('_') {
		t.Fatal("ident/trigger chars must trigger")
	}
	if shouldTrigger(' ') || shouldTrigger('\t') {
		t.Fatal("whitespace must not trigger")
	}
}
