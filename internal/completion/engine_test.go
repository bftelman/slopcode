package completion

import (
	"path/filepath"
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
