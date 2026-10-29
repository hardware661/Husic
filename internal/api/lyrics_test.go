package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hardware661/husic/internal/source"
)

type lyricsProvider struct {
	calls atomic.Int32
	text  atomic.Value
}

func (p *lyricsProvider) Name() string { return "kuwo" }
func (p *lyricsProvider) Search(context.Context, string, int) ([]source.Track, error) {
	return nil, nil
}
func (p *lyricsProvider) Stream(context.Context, source.Track) (source.Stream, error) {
	return source.Stream{}, source.ErrUnavailable
}
func (p *lyricsProvider) Lyrics(context.Context, source.Track) (string, error) {
	p.calls.Add(1)
	text, _ := p.text.Load().(string)
	if text == "" {
		return "", source.ErrUnavailable
	}
	return text, nil
}

func TestLyricsMissIsRetriedButHitIsCached(t *testing.T) {
	p := &lyricsProvider{}
	p.text.Store("")
	s := &Server{Providers: []source.Provider{p}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ask := func() string {
		body := `{"item":{"title":"Song","artists":[],"duration":200,"copies":[{"source":"kuwo","id":"1","title":"Song"}]}}`
		r := httptest.NewRequest("POST", "/api/lyrics", strings.NewReader(body))
		w := httptest.NewRecorder()
		s.lyrics(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d", w.Code)
		}
		return w.Body.String()
	}
	ask()
	p.text.Store("[00:01.00]first line")
	if got := ask(); !strings.Contains(got, "first line") {
		t.Fatalf("a missed lookup was served from cache: %s", got)
	}
	calls := p.calls.Load()
	ask()
	if p.calls.Load() != calls {
		t.Fatal("found lyrics were not cached")
	}
}

func TestLongExpiredCacheEntriesAreDropped(t *testing.T) {
	s := &Server{}
	s.cache = map[string]cacheEntry{}
	old := time.Now().Add(-48 * time.Hour)
	for i := 0; i < cacheSweepSize+10; i++ {
		s.cache["old:"+strconv.Itoa(i)] = cacheEntry{value: i, until: old}
	}
	s.cache["recent"] = cacheEntry{value: "kept", until: time.Now().Add(-time.Minute)}
	if _, err := s.cached(context.Background(), "new", time.Hour, func(context.Context) (any, error) { return "v", nil }); err != nil {
		t.Fatal(err)
	}
	if len(s.cache) != 2 {
		t.Fatalf("cache kept %d entries", len(s.cache))
	}
	if _, ok := s.cache["recent"]; !ok {
		t.Fatal("a recently expired entry must stay available as a stale answer")
	}
}
