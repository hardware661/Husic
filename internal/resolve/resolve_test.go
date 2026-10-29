package resolve

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hardware661/husic/internal/source"
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

type slowProvider struct{ name string }

func (p slowProvider) Name() string { return p.name }
func (p slowProvider) Search(ctx context.Context, _ string, _ int) ([]source.Track, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (p slowProvider) Stream(ctx context.Context, _ source.Track) (source.Stream, error) {
	<-ctx.Done()
	return source.Stream{}, ctx.Err()
}

type marks struct {
	mu      sync.Mutex
	reasons map[string]string
}

func (m *marks) LinkTrack(context.Context, int64, source.Track) error { return nil }
func (m *marks) MarkSource(_ context.Context, src, id string, ok bool, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reasons[src+":"+id] = reason
	return nil
}

func TestUnreachablePlatformsAreBoundedAndNotBlamed(t *testing.T) {
	providers := []source.Provider{slowProvider{"netease"}, slowProvider{"qq"}, slowProvider{"kuwo"}, slowProvider{"kugou"}}
	m := &marks{reasons: map[string]string{}}
	r := New(providers, m)
	r.total = 300 * time.Millisecond
	want := source.Track{Title: "Song", Artists: []string{"Artist"}, Duration: 200}
	var copies []source.Track
	for _, p := range providers {
		c := want
		c.Source, c.ID = p.Name(), "1"
		copies = append(copies, c)
	}
	start := time.Now()
	_, _, err := r.Resolve(context.Background(), 0, want, copies)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("resolve took %s with a 300ms budget", elapsed)
	}
	if err == nil || IsUnavailable(err) {
		t.Fatalf("timeouts must not read as 'no platform has it': %v", err)
	}
	for ref, reason := range m.reasons {
		if reason != "" {
			t.Errorf("%s was marked broken (%q) for a timeout", ref, reason)
		}
	}
}
