package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type testMedia struct{ calls int }

func (m *testMedia) Status() (bool, bool)                             { return true, false }
func (m *testMedia) ServeHTTP(w http.ResponseWriter, r *http.Request) { m.calls++; w.WriteHeader(206) }

func TestYouTubeMediaUsesExistingGateway(t *testing.T) {
	media := &testMedia{}
	s := &Server{YouTube: media, GatewayHeader: "X-Test-Gateway", GatewayKey: "test-proof"}
	h := s.Handler()
	for _, authorized := range []bool{false, true} {
		r := httptest.NewRequest("GET", "/api/media/token", nil)
		if authorized {
			r.Header.Set("X-Test-Gateway", "test-proof")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if authorized && w.Code != 206 || !authorized && w.Code != 403 {
			t.Fatalf("gateway result %d authorized=%v", w.Code, authorized)
		}
	}
	if media.calls != 1 {
		t.Fatal("unauthenticated request reached media provider")
	}
}
