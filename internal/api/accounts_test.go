package api

import (
	"github.com/hardware661/husic/internal/accounts"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountMutationKeepsGatewayAndRejectsCrossSiteRequests(t *testing.T) {
	s := &Server{GatewayHeader: "X-Proof", GatewayKey: "fixture", Accounts: &accounts.Manager{}}
	h := s.Handler()
	for _, test := range []struct {
		proof, origin, site, kind string
		want                      int
	}{
		{"", "http://music.test", "same-origin", "application/json", 403},
		{"fixture", "https://unrelated.test", "cross-site", "application/json", 403},
		{"fixture", "https://music.test", "same-site", "application/json", 403},
		{"fixture", "http://music.test", "same-origin", "text/plain", 403},
		{"fixture", "http://music.test", "same-origin", "application/json", 503},
	} {
		r := httptest.NewRequest("POST", "http://music.test/api/accounts/youtube/login", strings.NewReader("{}"))
		r.Header.Set("X-Proof", test.proof)
		r.Header.Set("Origin", test.origin)
		r.Header.Set("Sec-Fetch-Site", test.site)
		r.Header.Set("Content-Type", test.kind)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != test.want {
			t.Fatalf("status %d want %d", w.Code, test.want)
		}
	}
}
