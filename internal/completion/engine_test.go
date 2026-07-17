package completion

import (
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func regFor(p Provider) Registry {
	return Registry{Factory: func(ext, root string) (Provider, error) {
		if ext == ".go" {
			return p, nil
		}
		return nil, nil
	}}
}

func TestEngineDebounceCollapsesBurst(t *testing.T) {
	f := &fakeProvider{items: []Item{{Label: "x"}}}
	e := New(regFor(f), WithDebounce(40*time.Millisecond))
	defer e.Close()

	doc := Document{URI: "file:///a.go", Version: 1}
	for i := 0; i < 5; i++ {
		e.Update(doc, Position{})
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case <-e.Results():
	case <-time.After(time.Second):
		t.Fatal("no result")
	}
	if got := f.callCount(); got != 1 {
		t.Fatalf("Complete called %d times, want 1", got)
	}
}

func TestEngineNewUpdateCancelsInflight(t *testing.T) {
	block := make(chan struct{})
	f := &fakeProvider{block: block}
	e := New(regFor(f), WithDebounce(5*time.Millisecond))
	defer e.Close()

	doc := Document{URI: "file:///a.go", Version: 1}
	e.Update(doc, Position{})
	// Wait until the provider is inside Complete.
	deadline := time.After(time.Second)
	for f.callCount() == 0 {
		select {
		case <-deadline:
			t.Fatal("first Complete never started")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	firstCtx := f.lastCtx
	e.Update(Document{URI: "file:///a.go", Version: 2}, Position{}) // supersede
	select {
	case <-firstCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("first request context was not cancelled")
	}
	close(block)
}

// TestEngineCloseDuringDispatchNoPanic reproduces a race where Close() would
// cancel only the latest request, then immediately close(e.results) and the
// providers without waiting for outstanding dispatch goroutines. A dispatch
// goroutine using a fast, non-blocking provider could then be selecting on
// `e.results <- ...` / `<-ctx.Done()` at the exact moment cancel() and
// close(e.results) run concurrently on the loop goroutine; Go's select does
// not prefer the safe branch, so it can pick the send and panic.
//
// Each iteration fires Update, waits only until the provider's Complete has
// started (so a dispatch goroutine is definitely in flight, without ever
// draining Results()), then calls Close immediately — maximizing the odds of
// landing in that window. Running many parallel workers, each yielding via
// runtime.Gosched() while polling, produces exactly the scheduler
// interleaving needed to hit it reliably within a few seconds: on the old
// code this reliably panics with "send on closed channel"; on the fixed code
// it must never panic.
func TestEngineCloseDuringDispatchNoPanic(t *testing.T) {
	const workers = 16
	const perWorker = 800
	doc := Document{URI: "file:///a.go", Version: 1}

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				f := &fakeProvider{items: []Item{{Label: "x"}}}
				e := New(regFor(f), WithDebounce(time.Millisecond))
				e.Update(doc, Position{})
				deadline := time.Now().Add(15 * time.Millisecond)
				for f.callCount() == 0 && time.Now().Before(deadline) {
					runtime.Gosched() // yield so the dispatch/loop goroutines can run
				}
				if err := e.Close(); err != nil {
					t.Errorf("Close returned error: %v", err)
				}
			}
		}()
	}
	wg.Wait()
}

func TestEngineUnsupportedExtensionYieldsNothing(t *testing.T) {
	f := &fakeProvider{items: []Item{{Label: "x"}}}
	e := New(regFor(f), WithDebounce(10*time.Millisecond))
	defer e.Close()

	e.Update(Document{URI: "file:///a.txt", Version: 1}, Position{})
	select {
	case r := <-e.Results():
		t.Fatalf("unexpected result %+v", r)
	case <-time.After(80 * time.Millisecond):
	}
	_ = filepath.Ext // used indirectly by the engine
}
