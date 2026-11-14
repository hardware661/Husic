package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A service key (e.g. a speaker app on the same host) may read the library
// and resolve a stream, nothing else; it never needs the edge's gateway proof.
func TestServiceKeyIsLimitedToReadAndStream(t *testing.T) {
	s := &Server{GatewayHeader: "X-Proof", GatewayKey: "edge-proof", ServiceKey: "svc-key"}
	h := s.gate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(299) }))
	cases := []struct {
		method, path, auth string
		want               int
	}{
		{"GET", "/api/library", "Bearer svc-key", 299},
		{"GET", "/api/playlists/7", "Bearer svc-key", 299},
		{"POST", "/api/stream", "Bearer svc-key", 299},
		{"GET", "/api/search", "Bearer svc-key", 299},
		{"POST", "/api/likes", "Bearer svc-key", 403},
		{"DELETE", "/api/playlists/7", "Bearer svc-key", 403},
		{"POST", "/api/playlists/7/songs", "Bearer svc-key", 403},
		{"GET", "/api/accounts", "Bearer svc-key", 403},
		{"GET", "/", "Bearer svc-key", 403},
		{"GET", "/api/library", "Bearer wrong", 403},
		{"GET", "/api/library", "", 403},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.path, nil)
		if c.auth != "" {
			r.Header.Set("Authorization", c.auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s %s %q = %d, want %d", c.method, c.path, c.auth, w.Code, c.want)
		}
	}
	// The edge proof still opens everything, as before.
	r := httptest.NewRequest("POST", "/api/likes", nil)
	r.Header.Set("X-Proof", "edge-proof")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 299 {
		t.Fatalf("edge proof = %d", w.Code)
	}
}

func TestNoServiceKeyConfiguredMeansBearerIsIgnored(t *testing.T) {
	s := &Server{GatewayHeader: "X-Proof", GatewayKey: "edge-proof"}
	h := s.gate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(299) }))
	r := httptest.NewRequest("GET", "/api/library", nil)
	r.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("empty key = %d", w.Code)
	}
}
