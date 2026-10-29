package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/hardware661/husic/internal/resolve"
	"github.com/hardware661/husic/internal/source"
)

type downloadProvider struct{ kind, address string }

func (p downloadProvider) Name() string { return p.kind }
func (p downloadProvider) Search(context.Context, string, int) ([]source.Track, error) {
	return nil, nil
}
func (p downloadProvider) Stream(context.Context, source.Track) (source.Stream, error) {
	return source.Stream{URL: p.address, Format: "mp3"}, nil
}

type downloadRoundTrip func(*http.Request) (*http.Response, error)

func (f downloadRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func downloadRequest(t *testing.T, item Item) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]any{"item": item})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "http://music.test/api/download", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://music.test")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("X-Proof", "fixture")
	return r
}
func downloadFixtureItem() Item {
	return Item{Title: "Song", Artists: []string{"Artist"}, Copies: []source.Track{{Source: "netease", ID: "1"}}}
}
func downloadFixtureServer(address string) *Server {
	return &Server{Resolver: resolve.New([]source.Provider{downloadProvider{"netease", address}}, nil), GatewayHeader: "X-Proof", GatewayKey: "fixture"}
}

var downloadMP3 = append([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0}, bytes.Repeat([]byte{0xff, 0xfb, 0x90, 0}, 32)...)

func downloadResponse(data []byte) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"audio/mpeg"}}, ContentLength: int64(len(data)), Body: io.NopCloser(bytes.NewReader(data))}
}

func TestDownloadRouteKeepsGatewayAndWriteProtection(t *testing.T) {
	for _, test := range []struct {
		name, proof, origin, site, kind string
		want                            int
	}{
		{"no gateway", "", "http://music.test", "same-origin", "application/json", 403},
		{"cross origin", "fixture", "https://evil.test", "cross-site", "application/json", 403},
		{"wrong scheme", "fixture", "https://music.test", "same-site", "application/json", 403},
		{"bad origin path", "fixture", "http://music.test/spoof", "same-origin", "application/json", 403},
		{"wrong type", "fixture", "http://music.test", "same-origin", "text/plain", 403},
		{"json lookalike", "fixture", "http://music.test", "same-origin", "application/jsonp", 403},
		{"valid guarded route", "fixture", "http://music.test", "same-origin", "application/json", 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &Server{GatewayHeader: "X-Proof", GatewayKey: "fixture"}
			r := downloadRequest(t, downloadFixtureItem())
			r.Header.Set("X-Proof", test.proof)
			r.Header.Set("Origin", test.origin)
			r.Header.Set("Sec-Fetch-Site", test.site)
			r.Header.Set("Content-Type", test.kind)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("status %d want %d: %s", w.Code, test.want, w.Body.String())
			}
		})
	}
}

func TestDownloadRejectsUserAddressesAndLocalFiles(t *testing.T) {
	for _, raw := range []string{
		`{"item":{"title":"Local","copies":[{"source":"local","id":"file:///secret"}]}}`,
		`{"item":{"title":"Local","localId":"file-1","copies":[]}}`,
		`{"item":{"title":"Song","copies":[{"source":"netease","id":"1"}]},"url":"http://127.0.0.1/secret"}`,
		`{"item":{"title":"Song","copies":[{"source":"netease","id":"1","url":"http://127.0.0.1/secret"}]}}`,
		`{"item":{}}`,
		`{"item":{"songId":-1}}`,
	} {
		r := downloadRequest(t, downloadFixtureItem())
		r.Body = io.NopCloser(strings.NewReader(raw))
		w := httptest.NewRecorder()
		(&Server{GatewayHeader: "X-Proof", GatewayKey: "fixture"}).Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("status %d for invalid request", w.Code)
		}
	}
}

func TestDownloadReturnsCompleteAudioAndRealFormat(t *testing.T) {
	s := downloadFixtureServer("https://m801.music.126.net/song.mp3")
	called := false
	transport := downloadRoundTrip(func(r *http.Request) (*http.Response, error) {
		called = true
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-Proof") != "" || r.Header.Get("Authorization") != "" {
			t.Fatal("account/gateway credential leaked to media")
		}
		if r.Header.Get("Referer") != "https://music.163.com/" || r.Header.Get("Accept-Encoding") != "identity" {
			t.Fatal("missing safe media headers")
		}
		return downloadResponse(downloadMP3), nil
	})
	w := httptest.NewRecorder()
	s.gate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.downloadUsing(w, r, transport) })).ServeHTTP(w, downloadRequest(t, downloadFixtureItem()))
	if !called || w.Code != 200 || !bytes.Equal(w.Body.Bytes(), downloadMP3) || w.Header().Get("Content-Type") != "audio/mpeg" || w.Header().Get("X-Husic-Audio-Format") != "mp3" {
		t.Fatalf("not a complete MP3 response: %d %v", w.Code, w.Header())
	}
	if !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal("not a downloadable attachment")
	}
}

func TestDownloadDoesNotReturnBadUpstreamAsSuccess(t *testing.T) {
	for _, test := range []struct {
		name     string
		response func() *http.Response
		want     int
	}{
		{"html", func() *http.Response {
			r := downloadResponse([]byte("<html>login</html>"))
			r.Header.Set("Content-Type", "text/html")
			return r
		}, 502},
		{"html labeled audio", func() *http.Response { return downloadResponse([]byte("<html>login</html>")) }, 502},
		{"partial response", func() *http.Response { r := downloadResponse(downloadMP3); r.StatusCode = 206; return r }, 502},
		{"truncated", func() *http.Response { r := downloadResponse(downloadMP3); r.ContentLength++; return r }, 502},
		{"too large", func() *http.Response { r := downloadResponse(nil); r.ContentLength = maxDownloadBytes + 1; return r }, 413},
		{"upstream error", func() *http.Response { r := downloadResponse(nil); r.StatusCode = 403; return r }, 502},
		{"empty", func() *http.Response { return downloadResponse(nil) }, 502},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := downloadFixtureServer("https://m801.music.126.net/song.mp3")
			w := httptest.NewRecorder()
			s.downloadUsing(w, downloadRequest(t, downloadFixtureItem()), downloadRoundTrip(func(*http.Request) (*http.Response, error) { return test.response(), nil }))
			if w.Code != test.want || strings.HasPrefix(w.Header().Get("Content-Type"), "audio/") {
				t.Fatalf("bad upstream became audio: status %d", w.Code)
			}
		})
	}
}

func TestDownloadDeniesUnknownDestinationBeforeTransport(t *testing.T) {
	for _, address := range []string{
		"http://m801.music.126.net/a.mp3", "https://127.0.0.1/a.mp3", "https://[::1]/a.mp3",
		"https://m801.music.126.net.evil.test/a.mp3", "https://evil.test/a.mp3", "https://user:pass@m801.music.126.net/a.mp3",
		"https://m801.music.126.net:8443/a.mp3", "https://m801.music.126.net/a.mp3#fragment",
		"https://ws.stream.qqmusic.qq.com/a.mp3", // cross-platform address
	} {
		s := downloadFixtureServer(address)
		called := false
		w := httptest.NewRecorder()
		s.downloadUsing(w, downloadRequest(t, downloadFixtureItem()), downloadRoundTrip(func(*http.Request) (*http.Response, error) { called = true; return downloadResponse(downloadMP3), nil }))
		if called || w.Code != 502 {
			t.Fatalf("untrusted destination reached transport: %s status %d", address, w.Code)
		}
	}
}

func TestDownloadDeniesRedirectEscape(t *testing.T) {
	for _, address := range []string{"https://evil.test/private", "http://m801.music.126.net/a.mp3", "https://127.0.0.1/secret", "https://m801.music.126.net:444/secret", "https://ws.stream.qqmusic.qq.com/secret"} {
		s := downloadFixtureServer("https://m801.music.126.net/song.mp3")
		calls := 0
		w := httptest.NewRecorder()
		s.downloadUsing(w, downloadRequest(t, downloadFixtureItem()), downloadRoundTrip(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {address}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		}))
		if calls != 1 || w.Code != 502 {
			t.Fatalf("redirect escaped: %s calls=%d status=%d", address, calls, w.Code)
		}
	}
}

func TestDownloadDNSRejectsPrivateAndPinsValidatedAddress(t *testing.T) {
	for _, addresses := range [][]string{{"127.0.0.1"}, {"10.0.0.1"}, {"169.254.169.254"}, {"100.64.0.1"}, {"::1"}, {"::ffff:127.0.0.1"}, {"fd00::1"}, {"64:ff9b::7f00:1"}, {"1.1.1.1", "192.168.1.1"}} {
		called := false
		transport := newDownloadTransport(func(context.Context, string, string) ([]netip.Addr, error) {
			out := make([]netip.Addr, 0, len(addresses))
			for _, address := range addresses {
				out = append(out, netip.MustParseAddr(address))
			}
			return out, nil
		}, func(context.Context, string, string) (net.Conn, error) {
			called = true
			return nil, errors.New("not a real dial")
		})
		if transport.Proxy != nil {
			t.Fatal("download inherited environment proxy")
		}
		_, err := transport.DialContext(context.Background(), "tcp", "m801.music.126.net:443")
		if err == nil || called {
			t.Fatalf("private/mixed DNS allowed: %v", addresses)
		}
	}
	lookups := 0
	dialed := ""
	transport := newDownloadTransport(func(context.Context, string, string) ([]netip.Addr, error) {
		lookups++
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	}, func(_ context.Context, _ string, address string) (net.Conn, error) {
		dialed = address
		return nil, errors.New("not a real dial")
	})
	_, _ = transport.DialContext(context.Background(), "tcp", "m801.music.126.net:443")
	if lookups != 1 || dialed != "1.1.1.1:443" {
		t.Fatalf("DNS not pinned at dial: %d %s", lookups, dialed)
	}
}

func TestDownloadCancellationAndUnknownLengthLimit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := downloadFixtureServer("https://m801.music.126.net/song.mp3")
	r := downloadRequest(t, downloadFixtureItem()).WithContext(ctx)
	w := httptest.NewRecorder()
	s.downloadUsing(w, r, downloadRoundTrip(func(r *http.Request) (*http.Response, error) { cancel(); return nil, r.Context().Err() }))
	if w.Code != 408 || strings.HasPrefix(w.Header().Get("Content-Type"), "audio/") {
		t.Fatalf("cancel became successful audio: %d", w.Code)
	}
	file, err := os.CreateTemp(t.TempDir(), "bounded-")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	capture := &downloadCapture{ctx: context.Background(), file: file, limit: 32, header: make(http.Header), status: 200}
	if _, err = capture.Write(bytes.Repeat([]byte{1}, 33)); !errors.Is(err, errDownloadLimit) {
		t.Fatalf("unknown length bypassed limit: %v", err)
	}
	info, _ := file.Stat()
	if info.Size() > 32 {
		t.Fatal("too much data reached temporary disk")
	}
}

type downloadTokenMedia struct {
	data   []byte
	length int
	called bool
}

func (m *downloadTokenMedia) Status() (bool, bool) { return true, false }
func (m *downloadTokenMedia) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.called = true
	if r.Method != "GET" || r.PathValue("token") != "abcdefghijklmnopqrstuvwx" || r.Header.Get("X-Proof") != "" {
		panic("invalid internal token request")
	}
	w.Header().Set("Content-Type", "audio/mp4")
	if m.length >= 0 {
		w.Header().Set("Content-Length", strconv.Itoa(m.length))
	}
	_, _ = w.Write(m.data)
}
func TestDownloadUsesOnlyControlledTokenHandlerAndChecksLength(t *testing.T) {
	for _, length := range []int{len(downloadMP3), len(downloadMP3) + 1, -1} {
		media := &downloadTokenMedia{data: downloadMP3, length: length}
		s := &Server{YouTube: media, Resolver: resolve.New([]source.Provider{downloadProvider{"youtube", "/api/media/abcdefghijklmnopqrstuvwx"}}, nil)}
		item := downloadFixtureItem()
		item.Copies[0].Source = "youtube"
		w := httptest.NewRecorder()
		s.downloadUsing(w, downloadRequest(t, item), downloadRoundTrip(func(*http.Request) (*http.Response, error) {
			t.Fatal("token used network loopback instead of controlled handler")
			return nil, nil
		}))
		want := 502
		if length == len(downloadMP3) {
			want = 200
		}
		if !media.called || w.Code != want {
			t.Fatalf("token response status %d want %d", w.Code, want)
		}
	}
}

type downloadBrokenBody struct {
	sent   bool
	cancel context.CancelFunc
}

func (b *downloadBrokenBody) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		n := copy(p, downloadMP3)
		if b.cancel != nil {
			b.cancel()
		}
		return n, nil
	}
	return 0, errors.New("fixture interrupted media body")
}
func (*downloadBrokenBody) Close() error { return nil }

func TestDownloadReadFailureAndCancellationRemoveTemporaryFiles(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(strconv.FormatBool(canceled), func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := &downloadBrokenBody{}
			if canceled {
				body.cancel = cancel
			}
			s := downloadFixtureServer("https://m801.music.126.net/song.mp3")
			w := httptest.NewRecorder()
			s.downloadUsing(w, downloadRequest(t, downloadFixtureItem()).WithContext(ctx), downloadRoundTrip(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"audio/mpeg"}}, ContentLength: -1, Body: body}, nil
			}))
			want := 502
			if canceled {
				want = 408
			}
			if w.Code != want {
				t.Fatalf("interrupted audio became success: %d", w.Code)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatal("download temporary file survived error/cancellation")
			}
		})
	}
}

func TestDownloadRejectsInvalidInternalTokens(t *testing.T) {
	for _, address := range []string{"//127.0.0.1/secret", "/api/media/../secret", "/api/media/abcdefghijklmnopqrstuvwx?url=secret", "/api/media/abcdefghijklmnopqrstuvwx%2Fsecret", "/api/bilibili/media/abcdefghijklmnopqrstuvwx"} {
		media := &downloadTokenMedia{data: downloadMP3, length: len(downloadMP3)}
		s := &Server{YouTube: media, Resolver: resolve.New([]source.Provider{downloadProvider{"youtube", address}}, nil)}
		item := downloadFixtureItem()
		item.Copies[0].Source = "youtube"
		w := httptest.NewRecorder()
		s.downloadUsing(w, downloadRequest(t, item), downloadRoundTrip(func(*http.Request) (*http.Response, error) {
			t.Fatal("internal path reached transport")
			return nil, nil
		}))
		if media.called || w.Code != 502 {
			t.Fatalf("invalid token reached media: %s status %d", address, w.Code)
		}
	}
}
