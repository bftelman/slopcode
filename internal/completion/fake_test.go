package completion

import (
	"context"
	"sync"
)

// fakeProvider is a controllable in-package Provider for engine tests.
type fakeProvider struct {
	mu      sync.Mutex
	items   []Item
	err     error
	calls   int
	block   chan struct{} // if non-nil, Complete blocks until closed/received
	lastCtx context.Context
}

func (f *fakeProvider) Complete(ctx context.Context, _ Document, _ Position) ([]Item, error) {
	f.mu.Lock()
	f.calls++
	f.lastCtx = ctx
	block := f.block
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.items, f.err
}

func (f *fakeProvider) Close() error { return nil }

func (f *fakeProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}
