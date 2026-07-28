//go:build lsp_integration

package completion

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGoplsRealCompletion(t *testing.T) {
	// resolveCmd, not a bare exec.LookPath: production resolves gopls via
	// $GOBIN/$GOPATH/bin when it's not on $PATH (see resolvecmd.go), and this
	// guard must match that or it silently skips on exactly the machines
	// that fallback exists for, giving no real coverage there.
	if resolveCmd("gopls") == "gopls" {
		t.Skip("gopls not resolvable via PATH, GOBIN, or GOPATH/bin")
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
