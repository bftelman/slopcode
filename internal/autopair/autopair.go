// Package autopair adds bracket/quote auto-completion on top of a buffer.
package autopair

import "github.com/bftelman/namlet/internal/buffer"

var openers = map[rune]rune{
	'(': ')', '[': ']', '{': '}',
	'"': '"', '\'': '\'', '`': '`',
}

var closers = map[rune]bool{
	')': true, ']': true, '}': true,
	'"': true, '\'': true, '`': true,
}

// InsertRune inserts r with auto-close / skip-over behavior.
func InsertRune(b *buffer.Buffer, r rune) {
	if closers[r] {
		if next, ok := b.RuneAt(0); ok && next == r {
			b.MoveRight() // step over existing closer
			return
		}
	}
	if close, ok := openers[r]; ok {
		b.InsertRune(r)
		b.InsertRune(close)
		b.MoveLeft()
		return
	}
	b.InsertRune(r)
}

// Backspace deletes an empty pair as a unit, else deletes one rune.
func Backspace(b *buffer.Buffer) {
	prev, hasPrev := b.RuneAt(-1)
	next, hasNext := b.RuneAt(0)
	if hasPrev && hasNext {
		if close, ok := openers[prev]; ok && close == next {
			b.MoveRight()
			b.Backspace()
			b.Backspace()
			return
		}
	}
	b.Backspace()
}
