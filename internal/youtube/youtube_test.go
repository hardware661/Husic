package youtube

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hardware661/husic/internal/source"
)

func TestOnlyPublicGoogleVideoDestinations(t *testing.T) {
	for _, u := range []string{"http://r.googlevideo.com/a", "https://r.googlevideo.com.evil.test/a", "https://user@r.googlevideo.com/a", "https://r.googlevideo.com:8443/a", "https://127.0.0.1/a"} {
		if validMediaURL(u) {
			t.Errorf("accepted unsafe destination %s", u)
		}
	}
	if !validMediaURL("https://rr1---sn-example.googlevideo.com/videoplayback?expire=9999999999") {
		t.Fatal("rejected CDN")
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "192.0.2.1", "::1", "::ffff:127.0.0.1", "fd00::1"} {
		if publicAddress(netip.MustParseAddr(ip)) {
			t.Errorf("accepted internal IP %s", ip)
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMediaRangesHeadAndNoCredentialForwarding(t *testing.T) {
	e := entry{id: "abcdefghijk", token: "opaque", url: "https://rr.googlevideo.com/audio", until: time.Now().Add(time.Hour), userAgent: "extractor-agent"}
	p := &Provider{entries: map[string]entry{e.id: e}, tokens: map[string]string{e.token: e.id}}
	calls := 0
	p.http = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("User-Agent") != "extractor-agent" {
			t.Error("incorrect upstream credential/header handling")
		}
		if r.Header.Get("Range") != "bytes=1-3" {
			t.Error("Range was lost")
		}
		return &http.Response{StatusCode: 206, Header: http.Header{"Content-Range": {"bytes 1-3/5"}, "Content-Length": {"3"}, "Accept-Ranges": {"bytes"}}, Body: io.NopCloser(strings.NewReader("bcd")), ContentLength: 3}, nil
	})}
	for _, method := range []string{"GET", "HEAD"} {
		r := httptest.NewRequest(method, "/api/media/opaque", nil)
		r.SetPathValue("token", "opaque")
		r.Header.Set("Range", "bytes=1-3")
		r.Header.Set("Cookie", "do-not-forward=fixture")
		r.Header.Set("Authorization", "fixture")
		w := httptest.NewRecorder()
		p.ServeHTTP(w, r)
		if w.Code != 206 || w.Header().Get("Content-Range") != "bytes 1-3/5" {
			t.Fatalf("invalid range response: %d %v", w.Code, w.Header())
		}
		if method == "HEAD" && w.Body.Len() != 0 || method == "GET" && w.Body.String() != "bcd" {
			t.Fatal("incorrect media body")
		}
	}
	r := httptest.NewRequest("GET", "/api/media/opaque", nil)
	r.SetPathValue("token", "opaque")
	r.Header.Set("Range", "bytes=0-1,3-4")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 416 || calls != 2 {
		t.Fatal("multi-range reached upstream")
	}
}

func TestSafariAudioAdmission(t *testing.T) {
	valid := map[string]any{"url": "https://rr.googlevideo.com/audio?expire=9999999999", "protocol": "https", "acodec": "mp4a.40.2", "duration": 180, "abr": 256}
	for _, field := range []string{"valid", "acodec", "is_live", "has_drm", "protocol"} {
		data := make(map[string]any)
		for k, v := range valid {
			data[k] = v
		}
		switch field {
		case "acodec":
			data[field] = "opus"
		case "is_live", "has_drm":
			data[field] = true
		case "protocol":
			data[field] = "m3u8_native"
		}
		body, _ := json.Marshal(data)
		e, err := parseEntry("abcdefghijk", body)
		if field == "valid" {
			if err != nil || e.bitrate != 256000 || !strings.HasPrefix(e.stream().URL, "/api/media/") {
				t.Fatal("valid AAC was not admitted")
			}
		} else if err == nil {
			t.Errorf("accepted %s", field)
		}
	}
}

func TestCookieJarIsPrivateAndParallelResolveIsCoalesced(t *testing.T) {
	dir := t.TempDir()
	importFile := filepath.Join(dir, "import.txt")
	_ = os.WriteFile(importFile, []byte("# Netscape HTTP Cookie File\n"), 0600)
	tool := filepath.Join(dir, "extractor")
	script := "#!/bin/sh\nprintf x >> '" + filepath.Join(dir, "calls") + "'\nprintf '%s' '{\"url\":\"https://rr.googlevideo.com/audio?expire=9999999999\",\"protocol\":\"https\",\"acodec\":\"mp4a.40.2\",\"duration\":180,\"abr\":128}'\n"
	_ = os.WriteFile(tool, []byte(script), 0700)
	p, err := New(Config{Binary: tool, Deno: tool, Cookies: importFile, StateDir: filepath.Join(dir, "state")})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.Stream(context.Background(), source.Track{Source: "youtube", ID: "abcdefghijk"})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if string(calls) != "x" {
		t.Fatalf("extractor invoked %d times", len(calls))
	}
	info, _ := os.Stat(filepath.Join(dir, "state", "cookies.txt"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("derived cookie jar is not private")
	}
	data, _ := os.ReadFile(importFile)
	if string(data) != "# Netscape HTTP Cookie File\n" {
		t.Fatal("original cookie import changed")
	}
}
