package render

import "testing"

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
