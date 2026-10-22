package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"music/internal/accounts"
	"music/internal/platform"
)

func TestExpiredPlatformAttemptIsGoneNotGatewayFailure(t *testing.T) {
	client, _ := platform.New("bilibili", "")
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

func TestUnusedAccountEndpointsCannotStartOrFinishLogin(t *testing.T) {
	s := Server{Accounts: &accounts.Manager{}}
	for _, kind := range []string{"qq", "kugou", "migu"} {
		for _, endpoint := range []struct{ method, suffix string }{{"POST", "login"}, {"POST", "login/old/check"}, {"POST", "login/old/complete"}, {"GET", "login/old/qr"}, {"GET", "collections"}, {"GET", "recommendations"}} {
			r := httptest.NewRequest(endpoint.method, "http://music.test/api/accounts/"+kind+"/"+endpoint.suffix, strings.NewReader("{}"))
			r.RemoteAddr = "127.0.0.1:12345"
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", "http://music.test")
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != 410 || !strings.Contains(w.Body.String(), "账号连接已停用") {
				t.Errorf("%s %s: got %d", kind, endpoint.suffix, w.Code)
			}
		}
	}
}
