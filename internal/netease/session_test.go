package netease

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type sessionTransport func(*http.Request) (*http.Response, error)

func (f sessionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReconnectReplacesSessionWithoutRacingPlayback(t *testing.T) {
	c, err := New("", &Session{MusicU: "old-session", UserID: 7}, 0)
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = sessionTransport(func(r *http.Request) (*http.Response, error) {
		cookie := r.Header.Get("Cookie")
		if !strings.Contains(cookie, "MUSIC_U=old-session") && !strings.Contains(cookie, "MUSIC_U=new-session") {
			t.Error("missing active identity")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":200}`))}, nil
	})
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { _, _ = c.Weapi(context.Background(), "/fixture", nil, nil) })
	}
	c.UseSession(Session{MusicU: "new-session", UserID: 7})
	wg.Wait()
	if c.UserID() != 7 {
		t.Fatal("account identity changed")
	}
	_, _ = c.Weapi(context.Background(), "/fixture", nil, nil)
}

func TestQRCodeUsesFreshLoginWithoutLeakingActiveSession(t *testing.T) {
	c, _ := New("", &Session{MusicU: "active-fixture", UserID: 7}, 0)
	c.http.Transport = sessionTransport(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.Header.Get("Cookie"), "MUSIC_U=") {
			t.Error("QR flow reused active account cookies")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":200,"unikey":"qr-fixture"}`))}, nil
	})
	if _, err := c.QRKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, _, _, _ = c.QRCheck(context.Background(), "qr-fixture")
	if c.UserID() != 7 {
		t.Fatal("preparing QR logged out the active account")
	}
}
