//go:build lsp_integration

package completion

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestGoplsRealCompletion(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not on PATH")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	src := "package main\nimport \"fmt\"\nfunc main() {\n\tfmt.\n}\n"
	if err := os.WriteFile(file, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	eng := New(LSPRegistry(GoplsSpecs()), WithDebounce(10*time.Millisecond))
	defer eng.Close()

	uri := PathToFileURI(file)
	doc := Document{URI: uri, LangID: "go", Text: src, Version: 1}
	eng.Open(doc)
	// Cursor right after "fmt." on line index 3, character 5.
	eng.Update(doc, Position{Line: 3, Character: 5})

	select {
	case r := <-eng.Results():
		if r.Err != nil {
			t.Fatalf("result error: %v", r.Err)
		}
		found := false
		for _, it := range r.Items {
			if it.Label == "Println" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected Println among %d items", len(r.Items))
		}
	case <-time.After(30 * time.Second):
		t.Fatal("no completion result from gopls in time")
	}
}
