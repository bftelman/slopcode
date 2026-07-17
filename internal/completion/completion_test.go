package completion

import (
	"context"
	"testing"
)

func TestFakeProviderReturnsItems(t *testing.T) {
	f := &fakeProvider{items: []Item{{Label: "foo", Insert: "foo"}}}
	got, err := f.Complete(context.Background(), Document{}, Position{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(got) != 1 || got[0].Label != "foo" {
		t.Fatalf("got %+v", got)
	}
}
