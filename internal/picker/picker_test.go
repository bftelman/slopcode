package picker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestParseListerOutput(t *testing.T) {
	got := ParseListerOutput("main.go\n./internal/buffer/buffer.go\n\n  \nREADME.md\n")
	want := []string{"main.go", "internal/buffer/buffer.go", "README.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Windows listers emit backslashes; ranking and display want forward slashes.
func TestParseListerOutputNormalizesSeparators(t *testing.T) {
	got := ParseListerOutput("internal\\render\\render.go\n")
	if len(got) != 1 || !strings.Contains(got[0], "/") || strings.Contains(got[0], "\\") {
		t.Errorf("got %v, want a forward-slashed path", got)
	}
}

func TestGitRootFindsAncestor(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(deep, "f.go")
	if err := os.WriteFile(file, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// From a directory, and from a file inside it.
	wantAbs, _ := filepath.Abs(root)
	for _, start := range []string{deep, file} {
		got := GitRoot(start)
		gotAbs, _ := filepath.Abs(got)
		if gotAbs != wantAbs {
			t.Errorf("GitRoot(%q) = %q, want %q", start, gotAbs, wantAbs)
		}
	}
}

func TestGitRootFallsBackWhenNoRepo(t *testing.T) {
	// A temp dir with no .git anywhere above it is not guaranteed on every
	// machine, so assert the contract that matters: a usable, existing path.
	dir := t.TempDir()
	got := GitRoot(dir)
	if got == "" {
		t.Fatal("GitRoot returned empty")
	}
	if fi, err := os.Stat(got); err != nil || !fi.IsDir() {
		t.Errorf("GitRoot(%q) = %q, which is not a directory", dir, got)
	}
}

func TestListWalkFindsFilesAndHonorsSkipList(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("main.go")
	mk("internal/buffer/buffer.go")
	mk(".git/config")
	mk("node_modules/pkg/index.js")
	mk("vendor/dep/dep.go")

	got, err := listWalk(root)
	if err != nil {
		t.Fatalf("listWalk: %v", err)
	}
	set := map[string]bool{}
	for _, p := range got {
		set[p] = true
	}
	for _, want := range []string{"main.go", "internal/buffer/buffer.go"} {
		if !set[want] {
			t.Errorf("missing %q in %v", want, got)
		}
	}
	for _, skip := range []string{".git/config", "node_modules/pkg/index.js", "vendor/dep/dep.go"} {
		if set[skip] {
			t.Errorf("%q should have been skipped", skip)
		}
	}
}

func TestFilesWithInjectedLister(t *testing.T) {
	src := FilesWith("/root", func(string) ([]string, error) {
		return []string{"a/b.go", "c.go"}, nil
	})
	cands, err := src.Candidates()
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 {
		t.Fatalf("got %d candidates, want 2", len(cands))
	}
	if cands[0].Text != "a/b.go" {
		t.Errorf("Text = %q, want the relative path", cands[0].Text)
	}
	if !strings.HasSuffix(cands[0].Path, filepath.FromSlash("a/b.go")) {
		t.Errorf("Path = %q, want it rooted", cands[0].Path)
	}
}

func TestFilesPropagatesListerError(t *testing.T) {
	want := errors.New("boom")
	src := FilesWith("/root", func(string) ([]string, error) { return nil, want })
	if _, err := src.Candidates(); !errors.Is(err, want) {
		t.Errorf("got %v, want %v", err, want)
	}
}

func TestLinesSourceSkipsBlanksAndNumbers(t *testing.T) {
	cands, err := Lines([]string{"first", "", "   ", "third"}).Candidates()
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 {
		t.Fatalf("got %d candidates, want 2 (blank lines skipped): %v", len(cands), cands)
	}
	if !strings.HasPrefix(cands[0].Text, "1: ") {
		t.Errorf("Text = %q, want a 1-based line number prefix", cands[0].Text)
	}
	if cands[1].Row != 3 {
		t.Errorf("Row = %d, want 3 (0-based index of %q)", cands[1].Row, "third")
	}
}

// Lines must snapshot, not alias: the buffer keeps being edited on the UI thread.
func TestLinesSnapshots(t *testing.T) {
	lines := []string{"original"}
	src := Lines(lines)
	lines[0] = "mutated"

	cands, _ := src.Candidates()
	if !strings.Contains(cands[0].Text, "original") {
		t.Errorf("Text = %q, want the snapshot taken at construction", cands[0].Text)
	}
}

// --- engine ---

func testCands(names ...string) []Candidate {
	out := make([]Candidate, len(names))
	for i, n := range names {
		out[i] = Candidate{Text: n, Path: n}
	}
	return out
}

type staticSource struct {
	cands []Candidate
	err   error
}

func (s staticSource) Title() string                    { return "static" }
func (s staticSource) Candidates() ([]Candidate, error) { return s.cands, s.err }

// waitResult reads the next result, failing on timeout.
func waitResult(t *testing.T, e *Engine) Result {
	t.Helper()
	select {
	case r, ok := <-e.Results():
		if !ok {
			t.Fatal("Results channel closed")
		}
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a result")
		return Result{}
	}
}

// waitFor reads results until pred is satisfied, so a debounced intermediate
// pass cannot make the test flaky.
func waitFor(t *testing.T, e *Engine, pred func(Result) bool) Result {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case r, ok := <-e.Results():
			if !ok {
				t.Fatal("Results channel closed")
			}
			if pred(r) {
				return r
			}
		case <-deadline:
			t.Fatal("timed out waiting for the expected result")
			return Result{}
		}
	}
}

// Opening emits the full, unfiltered list. fuzzy.Find("") returns nothing, so
// this is the special case the engine has to handle explicitly.
func TestEngineOpenEmitsAllCandidates(t *testing.T) {
	e := NewEngine(WithDebounce(time.Millisecond))
	defer e.Close()

	e.Open(1, staticSource{cands: testCands("a.go", "b.go", "c.go")})
	r := waitResult(t, e)

	if r.Total != 3 || len(r.Rows) != 3 {
		t.Errorf("Total=%d Rows=%d, want 3 and 3", r.Total, len(r.Rows))
	}
	if r.Query != "" {
		t.Errorf("Query = %q, want empty", r.Query)
	}
}

func TestEngineRanksAndReportsMatchedOffsets(t *testing.T) {
	e := NewEngine(WithDebounce(time.Millisecond))
	defer e.Close()

	e.Open(1, staticSource{cands: testCands(
		"internal/highlight/highlight.go",
		"internal/lsp/client.go",
		"README.md",
	)})
	waitResult(t, e) // the open result

	e.Query("ihl")
	r := waitFor(t, e, func(r Result) bool { return r.Query == "ihl" })

	if r.Total == 0 {
		t.Fatal("no matches for a query that should match")
	}
	if !strings.Contains(r.Rows[0].Cand.Text, "highlight") {
		t.Errorf("top row = %q, want a highlight path", r.Rows[0].Cand.Text)
	}
	if len(r.Rows[0].Matched) == 0 {
		t.Error("Matched is empty; the picker needs offsets to highlight")
	}
	for _, off := range r.Rows[0].Matched {
		if off < 0 || off >= len(r.Rows[0].Cand.Text) {
			t.Errorf("matched offset %d out of range for %q", off, r.Rows[0].Cand.Text)
		}
	}
}

func TestEngineEmptyQueryAfterFilterRestoresAll(t *testing.T) {
	e := NewEngine(WithDebounce(time.Millisecond))
	defer e.Close()

	e.Open(1, staticSource{cands: testCands("aaa.go", "bbb.go", "ccc.go")})
	waitResult(t, e)

	e.Query("aaa")
	waitFor(t, e, func(r Result) bool { return r.Query == "aaa" })

	e.Query("")
	r := waitFor(t, e, func(r Result) bool { return r.Query == "" })
	if r.Total != 3 {
		t.Errorf("Total = %d after clearing the query, want 3", r.Total)
	}
}

// A burst of queries must collapse into far fewer ranking passes than keystrokes.
func TestEngineDebouncesBurst(t *testing.T) {
	e := NewEngine(WithDebounce(60 * time.Millisecond))
	defer e.Close()

	e.Open(1, staticSource{cands: testCands("alpha.go", "beta.go")})
	waitResult(t, e)

	for _, q := range []string{"a", "al", "alp", "alph", "alpha"} {
		e.Query(q)
	}
	final := waitFor(t, e, func(r Result) bool { return r.Query == "alpha" })
	if final.Query != "alpha" {
		t.Fatalf("final query = %q, want %q", final.Query, "alpha")
	}

	// Nothing further should arrive: the intermediate queries were superseded.
	select {
	case extra := <-e.Results():
		t.Errorf("unexpected extra result after the final one: %+v", extra)
	case <-time.After(200 * time.Millisecond):
	}
}

// The generation is caller-owned: every result echoes whatever Open was given,
// so the editor can drop results belonging to a picker it already replaced.
func TestEngineEchoesCallerGeneration(t *testing.T) {
	e := NewEngine(WithDebounce(time.Millisecond))
	defer e.Close()

	e.Open(7, staticSource{cands: testCands("a.go")})
	if got := waitResult(t, e); got.Gen != 7 {
		t.Errorf("open result Gen = %d, want 7", got.Gen)
	}

	e.Open(8, staticSource{cands: testCands("b.go", "c.go")})
	second := waitFor(t, e, func(r Result) bool { return r.Total == 2 })
	if second.Gen != 8 {
		t.Errorf("reopen result Gen = %d, want 8", second.Gen)
	}

	// Queries after an Open keep carrying that Open's generation.
	e.Query("b")
	q := waitFor(t, e, func(r Result) bool { return r.Query == "b" })
	if q.Gen != 8 {
		t.Errorf("query result Gen = %d, want 8", q.Gen)
	}
}

func TestEngineSurfacesLoadError(t *testing.T) {
	e := NewEngine(WithDebounce(time.Millisecond))
	defer e.Close()

	e.Open(1, staticSource{err: errors.New("cannot read root")})
	r := waitResult(t, e)

	if r.Err == nil {
		t.Fatal("expected the load error to be reported")
	}
	if len(r.Rows) != 0 {
		t.Errorf("got %d rows alongside an error, want 0", len(r.Rows))
	}
}

func TestEngineTruncatesToMaxRows(t *testing.T) {
	names := make([]string, MaxRows+50)
	for i := range names {
		names[i] = fmt.Sprintf("file%03d.go", i)
	}
	e := NewEngine(WithDebounce(time.Millisecond))
	defer e.Close()

	e.Open(1, staticSource{cands: testCands(names...)})
	r := waitResult(t, e)

	if len(r.Rows) != MaxRows {
		t.Errorf("Rows = %d, want %d", len(r.Rows), MaxRows)
	}
	if r.Total != len(names) {
		t.Errorf("Total = %d, want the untruncated %d", r.Total, len(names))
	}
}

// The property test guarding incremental narrowing: for every query, narrowing
// from ANY shorter prefix's survivors must select the same candidates, with the
// same scores, as a full rescan. Debounce can collapse several keystrokes into
// one pass, so the engine may narrow from a prefix more than one character
// shorter; that whole space has to hold, not just the single-character step.
//
// What is deliberately NOT asserted is the order of equally-scored candidates.
// fuzzy sorts with Less(i,j) = Score >= Score under sort.Stable, so ties keep
// their *input* order - and narrowing feeds candidates in the previous ranking's
// order rather than natural order. The match set and the score sequence are
// identical either way; only the arrangement within a run of equal scores can
// differ, which is the same tie instability fzf and telescope have.
func TestNarrowingMatchesFullRescan(t *testing.T) {
	cands := testCands(genPaths(3000)...)

	sortedCopy := func(in []int) []int {
		out := append([]int(nil), in...)
		sort.Ints(out)
		return out
	}
	scoresOf := func(rows []Row) []int {
		out := make([]int, len(rows))
		for i, r := range rows {
			out[i] = r.Score
		}
		return out
	}

	for _, typed := range []string{"render", "buffergo", "intbuf", "cmdmain", "zz", "e"} {
		for k := 1; k <= len(typed); k++ {
			query := typed[:k]
			fullRows, fullIdx := rank(cands, query, nil)

			for j := 1; j < k; j++ { // every strictly shorter, non-empty prefix
				_, prevIdx := rank(cands, typed[:j], nil)
				gotRows, gotIdx := rank(cands, query, prevIdx)

				if !reflect.DeepEqual(sortedCopy(gotIdx), sortedCopy(fullIdx)) {
					t.Fatalf("query %q narrowed from %q: matched a different set (%d vs %d)",
						query, typed[:j], len(gotIdx), len(fullIdx))
				}
				if !reflect.DeepEqual(scoresOf(gotRows), scoresOf(fullRows)) {
					t.Fatalf("query %q narrowed from %q: score sequence differs\n got %v\nwant %v",
						query, typed[:j], scoresOf(gotRows), scoresOf(fullRows))
				}
			}
		}
	}
}

// genPaths builds deterministic, diverse repo-like paths so result sets shrink
// the way they do in a real project.
func genPaths(n int) []string {
	dirs := []string{"internal", "cmd", "pkg", "web", "api", "docs", "test", "scripts"}
	mids := []string{"buffer", "render", "editor", "client", "server", "parser",
		"router", "store", "cache", "worker", "auth", "metrics", "schema", "view"}
	leaves := []string{"main", "util", "types", "errors", "options", "run", "parse",
		"format", "encode", "validate", "dispatch"}
	exts := []string{".go", "_test.go", ".md", ".json", ".ts"}

	out := make([]string, n)
	seed := uint64(12345)
	next := func(m int) int {
		seed = seed*6364136223846793005 + 1442695040888963407
		return int((seed >> 33) % uint64(m))
	}
	for i := range out {
		out[i] = fmt.Sprintf("%s/%s/%s%s",
			dirs[next(len(dirs))], mids[next(len(mids))],
			leaves[next(len(leaves))], exts[next(len(exts))])
	}
	return out
}

func TestEngineCloseClosesResults(t *testing.T) {
	e := NewEngine(WithDebounce(time.Millisecond))
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, ok := <-e.Results(); ok {
		t.Error("Results should be closed after Close")
	}
}
