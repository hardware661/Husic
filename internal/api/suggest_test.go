package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hardware661/husic/internal/source"
)

type suggestionProvider struct {
	name string
	slow bool
}

func (p suggestionProvider) Name() string { return p.name }
func (p suggestionProvider) Stream(context.Context, source.Track) (source.Stream, error) {
	return source.Stream{}, source.ErrUnavailable
}
func (p suggestionProvider) Search(ctx context.Context, q string, n int) ([]source.Track, error) {
	if p.slow {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return []source.Track{{Source: p.name, ID: "1", Title: q + "的歌", Artists: []string{"歌手"}}}, nil
}
func TestSuggestionsDoNotWaitForSlowSearchProvider(t *testing.T) {
	s := &Server{Providers: []source.Provider{suggestionProvider{"netease", true}, suggestionProvider{"qq", false}}}
	r := httptest.NewRequest("GET", "/api/suggestions?q=hello", nil)
	r.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	start := time.Now()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("suggestions: %d", w.Code)
	}
	var out struct {
		Songs []Item `json:"songs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Songs) != 1 || out.Songs[0].Title != "hello的歌" {
		t.Fatalf("missing suggestion: %s", w.Body.String())
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("suggestions waited for a slow provider")
	}
}
func TestSuggestionsValidateQueryAndHaveGuestEmptyState(t *testing.T) {
	for _, test := range []struct {
		path string
		code int
	}{{"/api/suggestions?q=", 400}, {"/api/suggestions?q=hello", 200}} {
		r := httptest.NewRequest("GET", test.path, nil)
		r.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		(&Server{}).Handler().ServeHTTP(w, r)
		if w.Code != test.code {
			t.Fatalf("%s: %d", test.path, w.Code)
		}
	}
}
