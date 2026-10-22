package resolve

import (
	"context"
	"testing"

	"music/internal/source"
)

type testProvider struct {
	name   string
	calls  int
	tracks []source.Track
}

func (p *testProvider) Name() string { return p.name }
func (p *testProvider) Search(context.Context, string, int) ([]source.Track, error) {
	return p.tracks, nil
}
func (p *testProvider) Stream(context.Context, source.Track) (source.Stream, error) {
	p.calls++
	return source.Stream{URL: "https://fixture.invalid/" + p.name, Format: "mp3"}, nil
}

func TestRejectedCopyIsNotRetriedOrRediscovered(t *testing.T) {
	a := source.Track{Source: "netease", ID: "bad", Title: "Song", Artists: []string{"Artist"}, Duration: 180, Pinned: true}
	b := a
	b.Source, b.ID, b.Pinned = "kuwo", "good", false
	bad := &testProvider{name: "netease", tracks: []source.Track{a}}
	good := &testProvider{name: "kuwo", tracks: []source.Track{b}}
	r := New([]source.Provider{bad, good}, nil)
	// API used to move bad to the back, then Resolve sorted it first again.
	got, _, err := r.Resolve(context.Background(), 0, a, []source.Track{b, a}, a.Ref())
	if err != nil || got.Ref() != b.Ref() || bad.calls != 0 {
		t.Fatalf("wanted %s without retrying rejected copy; got %s, bad calls=%d, err=%v", b.Ref(), got.Ref(), bad.calls, err)
	}
	// The rejected copy must also be excluded from cross-platform discovery.
	_, _, err = r.Resolve(context.Background(), 0, a, nil, a.Ref(), b.Ref())
	if err == nil || bad.calls != 0 {
		t.Fatalf("rejected copies were rediscovered: calls=%d err=%v", bad.calls, err)
	}
}
