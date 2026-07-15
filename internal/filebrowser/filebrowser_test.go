package filebrowser

import (
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"b.txt", "a.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestOpenOrdersEntries(t *testing.T) {
	dir := setup(t)
	br, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := br.Entries()
	// Expect: .., sub/(dir), a.txt, b.txt
	if len(got) != 4 {
		t.Fatalf("want 4 entries, got %d: %#v", len(got), got)
	}
	if got[0].Name != ".." || !got[0].IsDir {
		t.Fatalf("first entry should be .. dir, got %#v", got[0])
	}
	if got[1].Name != "sub" || !got[1].IsDir {
		t.Fatalf("second should be sub dir, got %#v", got[1])
	}
	if got[2].Name != "a.txt" || got[3].Name != "b.txt" {
		t.Fatalf("files out of order: %#v", got[2:])
	}
}

func TestMoveClamps(t *testing.T) {
	br, _ := Open(setup(t))
	br.MoveUp() // already at 0
	if br.SelIndex() != 0 {
		t.Fatalf("want 0 got %d", br.SelIndex())
	}
	for i := 0; i < 10; i++ {
		br.MoveDown()
	}
	if br.SelIndex() != len(br.Entries())-1 {
		t.Fatalf("want last got %d", br.SelIndex())
	}
}

func TestEnterDirAndFile(t *testing.T) {
	dir := setup(t)
	br, _ := Open(dir)
	br.MoveDown() // select "sub"
	_, isDir, err := br.Enter()
	if err != nil || !isDir {
		t.Fatalf("want dir nav, isDir=%v err=%v", isDir, err)
	}
	if filepath.Base(br.Dir()) != "sub" {
		t.Fatalf("want dir sub, got %q", br.Dir())
	}
	// Go back up via "..".
	for br.Selected().Name != ".." {
		br.MoveUp()
	}
	_, isDir, _ = br.Enter()
	if !isDir || filepath.Clean(br.Dir()) != filepath.Clean(dir) {
		t.Fatalf("want back to %q, got %q", dir, br.Dir())
	}
	// Select a file and open it.
	for br.Selected().Name != "a.txt" {
		br.MoveDown()
	}
	path, isDir, err := br.Enter()
	if err != nil || isDir {
		t.Fatalf("want file, isDir=%v err=%v", isDir, err)
	}
	if filepath.Base(path) != "a.txt" {
		t.Fatalf("want a.txt path, got %q", path)
	}
}
