package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// listen opens the event stream as a player would and returns a channel of
// the data lines it gets.
func listen(t *testing.T, base, player string) (<-chan string, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/api/remote/listen?player="+player, nil)
	req.Header.Set("X-Proof", "edge-proof")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("listen = %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	out := make(chan string, 8)
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if d, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				out <- d
			}
		}
	}()
	return out, cancel
}

func post(t *testing.T, base, path, auth, body string) int {
	t.Helper()
	req, _ := http.NewRequest("POST", base+path, strings.NewReader(body))
	if auth == "proof" {
		req.Header.Set("X-Proof", "edge-proof")
	} else if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func waitConnected(t *testing.T, s *Server, n int) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if s.remote.count() == n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("players = %d, want %d", s.remote.count(), n)
}

// The speaker asks Husic on the owner's PC to play (owner 11-15): the
// command goes to the player that was used last, only to that one.
func TestRemoteCommandGoesToTheLastActivePlayer(t *testing.T) {
	s := &Server{GatewayHeader: "X-Proof", GatewayKey: "edge-proof", ServiceKey: "svc-key"}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	web, stopWeb := listen(t, srv.URL, "web-1")
	defer stopWeb()
	exe, stopExe := listen(t, srv.URL, "exe-1")
	defer stopExe()
	waitConnected(t, s, 2)
	if c := post(t, srv.URL, "/api/remote/active", "proof", `{"player":"exe-1"}`); c != 204 {
		t.Fatalf("active = %d", c)
	}
	if c := post(t, srv.URL, "/api/remote/command", "svc-key", `{"action":"play","what":"liked"}`); c != 202 {
		t.Fatalf("command = %d", c)
	}
	select {
	case got := <-exe:
		if !strings.Contains(got, `"action":"play"`) || !strings.Contains(got, `"what":"liked"`) {
			t.Fatalf("exe got %s", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("exe got nothing")
	}
	select {
	case got := <-web:
		t.Fatalf("web must not get it: %s", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRemoteCommandWithNoPlayerIsAConflict(t *testing.T) {
	s := &Server{GatewayHeader: "X-Proof", GatewayKey: "edge-proof", ServiceKey: "svc-key"}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	if c := post(t, srv.URL, "/api/remote/command", "svc-key", `{"action":"play","what":"liked"}`); c != http.StatusConflict {
		t.Fatalf("no player = %d, want 409", c)
	}
	if c := post(t, srv.URL, "/api/remote/command", "svc-key", `{"action":"rm -rf"}`); c != 400 {
		t.Fatalf("bad action = %d", c)
	}
}

// The service key reaches only the command; a player's own routes need the
// edge proof (the owner's login), and the command needs nothing more.
func TestRemoteRoutesAndTheServiceKey(t *testing.T) {
	s := &Server{GatewayHeader: "X-Proof", GatewayKey: "edge-proof", ServiceKey: "svc-key"}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	if c := post(t, srv.URL, "/api/remote/active", "svc-key", `{"player":"x"}`); c != 403 {
		t.Fatalf("service key on active = %d", c)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/api/remote/listen?player=x", nil)
	req.Header.Set("Authorization", "Bearer svc-key")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("service key on listen = %d", resp.StatusCode)
	}
}
