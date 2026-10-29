package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hardware661/husic/internal/source"
)

func TestBilibiliPartCannotSilentlyBecomeP1(t *testing.T) {
	c, _ := New("")
	c.direct.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/x/frontend/finger/spi" {
			return reply(`{"code":0,"data":{"b_3":"platform-issued-guest"}}`), nil
		}
		if r.URL.Path != "/x/web-interface/view" {
			t.Fatalf("unselected part reached playback %s", r.URL.Path)
		}
		return reply(`{"code":0,"data":{"bvid":"BV1E54y177CQ","title":"Two parts","videos":2,"pages":[{"page":1,"cid":11,"part":"First","duration":180},{"page":2,"cid":22,"part":"Second","duration":200}]}}`), nil
	})
	_, err := c.Stream(context.Background(), source.Track{Source: "bilibili", ID: "BV1E54y177CQ"})
	if err == nil || !strings.Contains(err.Error(), "分P") {
		t.Fatal("multi-part video defaulted to P1", err)
	}
}
func TestBilibiliMediaRejectsOtherOriginsAndRanges(t *testing.T) {
	for _, raw := range []string{"https://127.0.0.1/a", "http://x.bilivideo.com/a", "https://x.bilivideo.com.evil.test/a", "https://user:pass@x.bilivideo.com/a"} {
		if validMediaURL(raw) {
			t.Fatal("unsafe URL accepted", raw)
		}
	}
	c, _ := New("")
	path, err := c.registerMedia("https://audio.bilivideo.com/audio.m4a?sign=fixture")
	if err != nil {
		t.Fatal(err)
	}
	c.mediaHTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "https://www.bilibili.com/" {
			t.Fatal("media leaked account or omitted referer")
		}
		x := reply(strings.Repeat("x", 64))
		x.StatusCode = 206
		x.Header.Set("Content-Range", "bytes 0-63/1024")
		return x, nil
	})
	mux := http.NewServeMux()
	mux.Handle("GET /api/bilibili/media/{token}", c)
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("Range", "bytes=0-63")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.Len() != 64 || w.Header().Get("Content-Range") == "" {
		t.Fatal("range not preserved", w.Code)
	}
	r = httptest.NewRequest("GET", path, nil)
	r.Header.Set("Range", "bytes=0-3,8-9")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 416 {
		t.Fatal("multi-range accepted")
	}
}
