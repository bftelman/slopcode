package picker

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// slowSource blocks for delay before returning, standing in for an external
// lister that shells out. It counts how many times it was asked.
type slowSource struct {
	cands []Candidate
	err   error
	key   string
	delay time.Duration
	calls *int32
}

func (s slowSource) Title() string { return "slow" }
func (s slowSource) Key() string   { return s.key }
func (s slowSource) Candidates() ([]Candidate, error) {
	atomic.AddInt32(s.calls, 1)
	time.Sleep(s.delay)
	return s.cands, s.err
}

// The overlay must be able to appear before a slow listing finishes, so Open
// emits a Loading result immediately rather than blocking on the subprocess.
func TestOpenEmitsLoadingBeforeListingCompletes(t *testing.T) {
	var calls int32
	e := NewEngine(WithDebounce(time.Millisecond))
	defer e.Close()

	src := slowSource{cands: testCands("a.go"), key: "", delay: 300 * time.Millisecond, calls: &calls}
	start := time.Now()
	e.Open(1, src)

	first := waitResult(t, e)
	elapsed := time.Since(start)

	if !first.Loading {
		t.Error("first result should be marked Loading")
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("first result took %v; it must not wait for the listing", elapsed)
	}
	// And the real candidates follow.
	loaded := waitFor(t, e, func(r Result) bool { return !r.Loading })
	if loaded.Total != 1 {
		t.Errorf("loaded result Total = %d, want 1", loaded.Total)
	}
}

// A slow listing must not block queries: the loop keeps handling commands.
func TestQueriesWorkWhileListing(t *testing.T) {
	var calls int32
	e := NewEngine(WithDebounce(time.Millisecond))
	defer e.Close()

	src := slowSource{
		cands: testCands("alpha.go", "beta.go"),
		delay: 250 * time.Millisecond, calls: &calls,
	}
	e.Open(1, src)
	waitResult(t, e) // the Loading placeholder

	// Send a query while the listing is still running; it must be applied to the
	// candidates once they arrive.
	e.Query("alpha")
	got := waitFor(t, e, func(r Result) bool { return !r.Loading && r.Total > 0 })
	if len(got.Rows) == 0 || got.Rows[0].Cand.Text != "alpha.go" {
		t.Errorf("query issued during listing was not honored: %+v", got.Rows)
	}
}

// Reopening the same keyed source serves the previous candidates immediately,
// which is what makes a second Ctrl+P instant instead of paying the listing cost.
func TestReopenServesCachedCandidatesImmediately(t *testing.T) {
	var calls int32
	e := NewEngine(WithDebounce(time.Millisecond))
	defer e.Close()

	newSrc := func() slowSource {
		return slowSource{
			cands: testCands("a.go", "b.go", "c.go"),
			key:   "files:/project", delay: 200 * time.Millisecond, calls: &calls,
		}
	}

	e.Open(1, newSrc())
	waitFor(t, e, func(r Result) bool { return !r.Loading }) // first listing done

	e.Open(2, newSrc())
	first := waitResult(t, e)
	if first.Gen != 2 {
		t.Fatalf("Gen = %d, want 2", first.Gen)
	}
	if first.Total != 3 {
		t.Errorf("reopen served %d candidates immediately, want the 3 cached", first.Total)
	}
	if !first.Loading {
		t.Error("a cached serve should still report Loading while it refreshes")
	}
}

// An unkeyed source (buffer lines) is never cached: it changes with every edit.
func TestUnkeyedSourceIsNotCached(t *testing.T) {
	var calls int32
	e := NewEngine(WithDebounce(time.Millisecond))
	defer e.Close()

	e.Open(1, slowSource{cands: testCands("old.go"), key: "", delay: 0, calls: &calls})
	waitFor(t, e, func(r Result) bool { return !r.Loading })

	e.Open(2, slowSource{cands: testCands("new.go"), key: "", delay: 0, calls: &calls})
	first := waitResult(t, e)
	if first.Total != 0 {
		t.Errorf("unkeyed reopen served %d cached candidates, want 0", first.Total)
	}
	loaded := waitFor(t, e, func(r Result) bool { return !r.Loading })
	if len(loaded.Rows) != 1 || loaded.Rows[0].Cand.Text != "new.go" {
		t.Errorf("loaded rows = %+v, want the fresh candidate", loaded.Rows)
	}
}

// A refresh that fails must not replace a good cached list with an error.
func TestFailedRefreshKeepsCachedCandidates(t *testing.T) {
	var calls int32
	e := NewEngine(WithDebounce(time.Millisecond))
	defer e.Close()

	e.Open(1, slowSource{cands: testCands("a.go", "b.go"), key: "files:/p", calls: &calls})
	waitFor(t, e, func(r Result) bool { return !r.Loading })

	// Reopen with a source that fails; the cached list should survive.
	e.Open(2, slowSource{err: errors.New("rg vanished"), key: "files:/p", calls: &calls})
	first := waitResult(t, e)
	if first.Total != 2 {
		t.Fatalf("cached serve gave %d candidates, want 2", first.Total)
	}

	// Give the failing refresh time to land; rows must be unchanged and no error
	// surfaced over good data.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case r := <-e.Results():
			if r.Err != nil {
				t.Errorf("failed refresh surfaced an error over cached data: %v", r.Err)
			}
			if r.Total != 2 {
				t.Errorf("failed refresh changed the list to %d candidates", r.Total)
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Closing while a listing is in flight must not leak the loader goroutine on a
// send nobody will ever read.
func TestCloseDuringListingDoesNotBlock(t *testing.T) {
	var calls int32
	e := NewEngine(WithDebounce(time.Millisecond))

	e.Open(1, slowSource{cands: testCands("a.go"), delay: 300 * time.Millisecond, calls: &calls})
	waitResult(t, e) // Loading placeholder

	done := make(chan error, 1)
	go func() { done <- e.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked while a listing was in flight")
	}
	// Let the loader finish and hit the quit path.
	time.Sleep(400 * time.Millisecond)
}

// The tool lookup is memoized, so a missing binary is not re-probed per open.
func TestLookToolMemoizes(t *testing.T) {
	toolLookup.Delete("definitely-not-a-real-tool-xyz")

	start := time.Now()
	_, err1 := lookTool("definitely-not-a-real-tool-xyz")
	first := time.Since(start)

	start = time.Now()
	_, err2 := lookTool("definitely-not-a-real-tool-xyz")
	second := time.Since(start)

	if err1 == nil || err2 == nil {
		t.Fatal("expected a lookup error for a nonexistent tool")
	}
	if second > first && second > time.Millisecond {
		t.Errorf("second lookup took %v vs %v; it should be memoized", second, first)
	}
}
