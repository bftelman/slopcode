package textsearch

import (
	"fmt"
	"testing"
)

func TestFindAllEmptyQuery(t *testing.T) {
	if got := FindAll([]string{"abc"}, ""); got != nil {
		t.Errorf("empty query: got %v, want nil", got)
	}
}

func TestFindAllBasic(t *testing.T) {
	lines := []string{"foo bar foo", "baz", "foo"}
	got := FindAll(lines, "foo")
	want := []Match{{0, 0, 3}, {0, 8, 3}, {2, 0, 3}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFindAllOverlapping(t *testing.T) {
	got := FindAll([]string{"aaa"}, "aa")
	want := []Match{{0, 0, 2}, {0, 1, 2}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("overlapping: got %v, want %v", got, want)
	}
}

func TestSmartCase(t *testing.T) {
	lines := []string{"Foo foo FOO"}

	// All-lowercase query: case-insensitive, so all three hit.
	if got := FindAll(lines, "foo"); len(got) != 3 {
		t.Errorf("lowercase query: got %d matches, want 3 (%v)", len(got), got)
	}
	// Query with an uppercase char: case-sensitive, so only the exact one hits.
	got := FindAll(lines, "Foo")
	if len(got) != 1 || got[0].Col != 0 {
		t.Errorf("mixed-case query: got %v, want one match at col 0", got)
	}
	if got := FindAll(lines, "FOO"); len(got) != 1 || got[0].Col != 8 {
		t.Errorf("uppercase query: got %v, want one match at col 8", got)
	}
}

// Offsets must be valid byte offsets into the *original* line even when the
// line holds non-ASCII text. This is what foldASCII exists to guarantee:
// strings.ToLower can change byte length and would desynchronize offsets.
func TestOffsetsSurviveNonASCII(t *testing.T) {
	lines := []string{
		"日本語 foo tail",
		"İstanbul foo",      // U+0130; strings.ToLower would grow this line
		"straße foo",        // ß stays 2 bytes under ToLower, still worth guarding
		"emoji 🎉 foo after", // 4-byte rune before the match
	}
	for _, m := range FindAll(lines, "foo") {
		line := lines[m.Row]
		if m.Col < 0 || m.Col+m.Len > len(line) {
			t.Fatalf("row %d: span [%d,%d) out of range for %q (len %d)",
				m.Row, m.Col, m.Col+m.Len, line, len(line))
		}
		if got := line[m.Col : m.Col+m.Len]; got != "foo" {
			t.Errorf("row %d: span sliced %q, want %q (line %q)", m.Row, got, "foo", line)
		}
	}
	if n := len(FindAll(lines, "foo")); n != len(lines) {
		t.Errorf("got %d matches, want %d (one per line)", n, len(lines))
	}
}

// Non-ASCII letters compare case-sensitively - the documented limitation.
func TestNonASCIICaseIsSensitive(t *testing.T) {
	if got := FindAll([]string{"ÄÖÜ"}, "äöü"); len(got) != 0 {
		t.Errorf("non-ASCII folding is not supported; got %v, want no matches", got)
	}
}

func TestSteppingEmpty(t *testing.T) {
	for name, fn := range map[string]func([]Match, int, int) int{
		"NearestFrom": NearestFrom, "NextFrom": NextFrom, "PrevFrom": PrevFrom,
	} {
		if got := fn(nil, 0, 0); got != -1 {
			t.Errorf("%s on empty: got %d, want -1", name, got)
		}
	}
}

func TestStepping(t *testing.T) {
	// Matches at (0,0), (0,8), (2,0).
	ms := FindAll([]string{"foo bar foo", "baz", "foo"}, "foo")

	tests := []struct {
		name     string
		fn       func([]Match, int, int) int
		row, col int
		want     int
	}{
		// NearestFrom includes a match starting exactly at the cursor.
		{"nearest on a match", NearestFrom, 0, 0, 0},
		{"nearest between", NearestFrom, 0, 4, 1},
		{"nearest wraps", NearestFrom, 2, 1, 0},
		// NextFrom always advances past the cursor.
		{"next off a match", NextFrom, 0, 0, 1},
		{"next from mid", NextFrom, 0, 9, 2},
		{"next wraps", NextFrom, 2, 0, 0},
		// PrevFrom always retreats.
		{"prev from last", PrevFrom, 2, 0, 1},
		{"prev from second", PrevFrom, 0, 8, 0},
		{"prev wraps", PrevFrom, 0, 0, 2},
	}
	for _, tc := range tests {
		if got := tc.fn(ms, tc.row, tc.col); got != tc.want {
			t.Errorf("%s: at (%d,%d) got %d, want %d", tc.name, tc.row, tc.col, got, tc.want)
		}
	}
}

func TestFoldASCIIPreservesLength(t *testing.T) {
	for _, s := range []string{"İstanbul", "ÄÖÜ", "straße", "日本語", "AbC", "🎉"} {
		if got := foldASCII(s); len(got) != len(s) {
			t.Errorf("foldASCII(%q) = %q: len %d, want %d", s, got, len(got), len(s))
		}
	}
	if got := foldASCII("AbC-123"); got != "abc-123" {
		t.Errorf("foldASCII(%q) = %q", "AbC-123", got)
	}
}

// BenchmarkFindAll documents why this package uses strings.Index rather than a
// hand-rolled Boyer-Moore. See the design spec for the measured comparison:
// stdlib wins or ties at every needle length, because strings.Index dispatches
// to SIMD assembly in internal/bytealg.
func BenchmarkFindAll(b *testing.B) {
	var lines []string
	for i := 0; i < 20000; i++ {
		lines = append(lines,
			fmt.Sprintf("\tif col := screenCol(line%d, byteCol, tabWidth); col > 0 {", i))
	}
	total := 0
	for _, l := range lines {
		total += len(l) + 1
	}
	for _, q := range []string{"col", "screenCol", "tabWidth); col > 0 {"} {
		b.Run(fmt.Sprintf("q=%dB", len(q)), func(b *testing.B) {
			b.SetBytes(int64(total))
			for i := 0; i < b.N; i++ {
				FindAll(lines, q)
			}
		})
	}
}
