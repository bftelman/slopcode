// Package textsearch finds literal substrings in a slice of lines. It is a pure
// text utility with no UI dependencies.
//
// Search uses strings.Index rather than a hand-rolled Boyer-Moore. That is a
// measured decision, not an assumption: strings.Index dispatches to
// internal/bytealg, which is hand-written SIMD assembly for needles up to 32
// bytes with a Rabin-Karp fallback beyond, and it beats or ties both
// Boyer-Moore-Horspool and full Boyer-Moore at every needle length tested. See
// docs/superpowers/specs/2026-08-05-search-replace-fuzzy-find-design.md for the
// numbers, and BenchmarkFindAll to re-measure.
package textsearch

import "strings"

// Match is one occurrence: a byte span on a single line.
type Match struct {
	Row int // 0-based line index
	Col int // 0-based byte offset within the line
	Len int // byte length of the matched text
}

// FindAll returns every occurrence of query across lines, in document order.
// An empty query returns nil. Overlapping occurrences are all reported:
// searching "aa" in "aaa" yields offsets 0 and 1.
//
// Matching is smart-case. An all-lowercase query matches case-insensitively;
// any uppercase character in the query makes the whole query case-sensitive.
// Case-insensitive matching is ASCII-only - see foldASCII.
func FindAll(lines []string, query string) []Match {
	if query == "" {
		return nil
	}
	fold := !hasUpperASCII(query)
	needle := query
	if fold {
		needle = foldASCII(query)
	}

	var out []Match
	for row, line := range lines {
		hay := line
		if fold {
			hay = foldASCII(line)
		}
		for off := 0; off+len(needle) <= len(hay); {
			i := strings.Index(hay[off:], needle)
			if i < 0 {
				break
			}
			out = append(out, Match{Row: row, Col: off + i, Len: len(needle)})
			off += i + 1
		}
	}
	return out
}

// NearestFrom returns the index of the first match at or after (row, col),
// wrapping to 0. It is what selects the initial match when the find bar opens,
// so a cursor already sitting on a match selects that match instead of skipping
// it. Returns -1 when ms is empty.
func NearestFrom(ms []Match, row, col int) int {
	for i, m := range ms {
		if m.Row > row || (m.Row == row && m.Col >= col) {
			return i
		}
	}
	return wrapFirst(ms)
}

// NearestForward returns the index of the first match at or after (row, col),
// or -1 when there is none. Unlike NearestFrom it does **not** wrap.
//
// This is what a replace sweep advances with. Wrapping there would be a trap: a
// replacement that contains the query ("foo" -> "foofoo") would wrap the
// selection back onto the text just inserted, so holding the replace key would
// grow the line without ever terminating.
func NearestForward(ms []Match, row, col int) int {
	for i, m := range ms {
		if m.Row > row || (m.Row == row && m.Col >= col) {
			return i
		}
	}
	return -1
}

// NextFrom returns the index of the first match strictly after (row, col),
// wrapping to 0. Unlike NearestFrom it always advances, which is what Ctrl+N
// needs. Returns -1 when ms is empty.
func NextFrom(ms []Match, row, col int) int {
	for i, m := range ms {
		if m.Row > row || (m.Row == row && m.Col > col) {
			return i
		}
	}
	return wrapFirst(ms)
}

// PrevFrom returns the index of the last match strictly before (row, col),
// wrapping to the end. Returns -1 when ms is empty.
func PrevFrom(ms []Match, row, col int) int {
	for i := len(ms) - 1; i >= 0; i-- {
		m := ms[i]
		if m.Row < row || (m.Row == row && m.Col < col) {
			return i
		}
	}
	if len(ms) == 0 {
		return -1
	}
	return len(ms) - 1
}

func wrapFirst(ms []Match) int {
	if len(ms) == 0 {
		return -1
	}
	return 0
}

// hasUpperASCII reports whether s contains an A-Z byte. Only ASCII counts,
// mirroring foldASCII: a query of "ÄÖÜ" is not treated as "uppercase", because
// the folding path could not have matched it case-insensitively anyway.
func hasUpperASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			return true
		}
	}
	return false
}

// foldASCII lowercases A-Z and leaves every other byte untouched, including all
// multi-byte UTF-8 sequences.
//
// It is deliberately not strings.ToLower. Unicode lowering can change a
// string's byte length - U+0130 'İ' lowers to a 3-byte, 2-rune sequence - which
// would desynchronize every match offset found in the folded string from the
// original line, corrupting the Col of that match and all later ones on the
// line. Byte-length preservation is the whole point.
//
// The documented consequence: case-insensitive search is ASCII-only, so
// non-ASCII letters always compare case-sensitively.
func foldASCII(s string) string {
	// Avoid allocating for the common all-lowercase or non-alphabetic line.
	if !hasUpperASCII(s) {
		return s
	}
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
