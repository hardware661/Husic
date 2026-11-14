package api

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlaybackEventsAreLoggedNotStored(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{Log: slog.New(slog.NewTextHandler(&buf, nil)), AllowRemote: true}
	h := s.Handler()
	body := `{"events":[{"at":1794671000123,"action":"pause","from":"mediasession","state":"playing"},{"at":1794671008456,"action":"play","from":"mediasession","state":"paused"}]}`
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/playback-events", strings.NewReader(body)))
	if w.Code != 204 {
		t.Fatalf("status %d", w.Code)
	}
	out := buf.String()
	if strings.Count(out, "playback") != 2 || !strings.Contains(out, "from=mediasession") || !strings.Contains(out, "action=play") {
		t.Fatalf("log = %s", out)
	}
}

func TestPlaybackEventsAreBounded(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{Log: slog.New(slog.NewTextHandler(&buf, nil)), AllowRemote: true}
	h := s.Handler()
	many := strings.Repeat(`{"at":1,"action":"play","from":"x","state":"y"},`, 60)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/playback-events", strings.NewReader(`{"events":[`+strings.TrimSuffix(many, ",")+`]}`)))
	if w.Code != 400 {
		t.Fatalf("60 events status %d", w.Code)
	}
	w = httptest.NewRecorder()
	long := `{"events":[{"at":1,"action":"` + strings.Repeat("a", 100) + `","from":"x","state":"y"}]}`
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/playback-events", strings.NewReader(long)))
	if w.Code != 400 || buf.Len() != 0 {
		t.Fatalf("oversized field status %d log %q", w.Code, buf.String())
	}
}
