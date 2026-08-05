package autopair

import (
	"testing"

	"github.com/bftelman/namlet/internal/buffer"
)

func lineCol(b *buffer.Buffer) (string, int) {
	_, c := b.Cursor()
	return b.Lines()[0], c
}

func TestInsertOpenerAutoCloses(t *testing.T) {
	b := buffer.New([]string{""})
	InsertRune(b, '(')
	if l, c := lineCol(b); l != "()" || c != 1 {
		t.Fatalf("want () col1, got %q col%d", l, c)
	}
}

func TestInsertClosingSkipsOver(t *testing.T) {
	b := buffer.New([]string{""})
	InsertRune(b, '(') // "(|)"
	InsertRune(b, ')') // should step over -> "()|"
	if l, c := lineCol(b); l != "()" || c != 2 {
		t.Fatalf("want () col2, got %q col%d", l, c)
	}
}

func TestInsertQuotePair(t *testing.T) {
	b := buffer.New([]string{""})
	InsertRune(b, '"')
	if l, c := lineCol(b); l != "\"\"" || c != 1 {
		t.Fatalf("want two quotes col1, got %q col%d", l, c)
	}
	InsertRune(b, '"') // step over
	if l, c := lineCol(b); l != "\"\"" || c != 2 {
		t.Fatalf("want two quotes col2, got %q col%d", l, c)
	}
}

func TestBackspaceDeletesEmptyPair(t *testing.T) {
	b := buffer.New([]string{""})
	InsertRune(b, '{') // "{|}"
	Backspace(b)       // delete both -> ""
	if l, c := lineCol(b); l != "" || c != 0 {
		t.Fatalf("want empty col0, got %q col%d", l, c)
	}
}

func TestBackspacePlain(t *testing.T) {
	b := buffer.New([]string{"ab"})
	b.MoveEnd()
	Backspace(b)
	if l := b.Lines()[0]; l != "a" {
		t.Fatalf("want a, got %q", l)
	}
}

func TestInsertPlainRune(t *testing.T) {
	b := buffer.New([]string{""})
	InsertRune(b, 'x')
	if l, c := lineCol(b); l != "x" || c != 1 {
		t.Fatalf("want x col1, got %q col%d", l, c)
	}
}
