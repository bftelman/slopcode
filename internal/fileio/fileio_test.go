package fileio

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFile(t *testing.T) {
	lines, err := Load(filepath.Join(t.TempDir(), "nope.txt"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(lines) != 1 || lines[0] != "" {
		t.Fatalf("want one blank line, got %#v", lines)
	}
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := Save(p, []string{"hello", "world"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	lines, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(lines) != 2 || lines[0] != "hello" || lines[1] != "world" {
		t.Fatalf("round trip bad: %#v", lines)
	}
}

func TestLoadStripsCarriageReturn(t *testing.T) {
	p := filepath.Join(t.TempDir(), "crlf.txt")
	if err := os.WriteFile(p, []byte("a\r\nb\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	lines, _ := Load(p)
	if len(lines) < 2 || lines[0] != "a" || lines[1] != "b" {
		t.Fatalf("want a,b got %#v", lines)
	}
}
