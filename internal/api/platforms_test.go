package api

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"music/internal/accounts"
	"music/internal/platform"
)

func TestExpiredPlatformAttemptIsGoneNotGatewayFailure(t *testing.T) {
	client, _ := platform.New("")
	s := Server{Accounts: &accounts.Manager{Platforms: map[string]accounts.Platform{"bilibili": client}}}
	r := httptest.NewRequest("POST", "http://music.test/api/accounts/bilibili/login/absent/check", strings.NewReader("{}"))
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://music.test")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 410 {
		t.Fatalf("ended QR incorrectly became a gateway error: %d %s", w.Code, w.Body.String())
	}
}

func TestUnknownAPIPathsAreNotServedTheAppShell(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Server{Accounts: &accounts.Manager{}, WebDir: dir}
	for _, path := range []string{"/api/accounts/qq/login", "/api/accounts/kugou/collections", "/api/nothing"} {
		r := httptest.NewRequest("GET", "http://music.test"+path, nil)
		r.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 404 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s: got %d %q", path, w.Code, w.Header().Get("Content-Type"))
		}
	}
}
