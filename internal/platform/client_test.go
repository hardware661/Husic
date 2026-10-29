package platform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func reply(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestBilibiliQRAndIdentity(t *testing.T) {
	c, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	c.direct.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/x/frontend/finger/spi":
			return reply(`{"code":0,"data":{"b_3":"platform-issued-guest"}}`), nil
		case "/x/passport-login/web/qrcode/generate":
			return reply(`{"code":0,"data":{"url":"https://account.bilibili.com/h5/account-h5/auth/scan-web?key=fixture","qrcode_key":"fixture"}}`), nil
		case "/x/passport-login/web/qrcode/poll":
			x := reply(`{"code":0,"data":{"code":0}}`)
			x.Header.Add("Set-Cookie", "SESSDATA=fixture-session; Domain=.bilibili.com; Path=/; Secure")
			x.Header.Add("Set-Cookie", "DedeUserID=42; Domain=.bilibili.com; Path=/; Secure")
			return x, nil
		case "/x/web-interface/nav":
			if got, _ := r.Cookie("SESSDATA"); got == nil || got.Value != "fixture-session" {
				t.Fatal("verified without candidate cookie")
			}
			return reply(`{"code":0,"data":{"isLogin":true,"mid":42,"uname":"Listener"}}`), nil
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
			return nil, errors.New("unexpected")
		}
	})
	login, err := c.Start(context.Background())
	if err != nil || len(login.Image) < 100 || login.Key != "fixture" {
		t.Fatalf("QR: %v", err)
	}
	result, err := c.Poll(context.Background(), login.Key)
	if err != nil || result.State != "confirmed" || result.Session == nil {
		t.Fatalf("poll: %+v %v", result, err)
	}
	profile, err := c.Verify(context.Background(), *result.Session)
	if err != nil || profile.ID != "42" {
		t.Fatalf("verify: %+v %v", profile, err)
	}
	wrong := *result.Session
	wrong.ID = "99"
	if _, err = c.Verify(context.Background(), wrong); err == nil {
		t.Fatal("wrong account accepted")
	}
	if c.Identity() != "" {
		t.Fatal("candidate was installed before commit")
	}
}

func TestSessionValidationAndCopies(t *testing.T) {
	c, _ := New("")
	s := Session{ID: "42", Cookies: map[string]string{"DedeUserID": "42", "SESSDATA": "fixture"}}
	if err := c.UseSession(s); err != nil {
		t.Fatal(err)
	}
	s.Cookies["SESSDATA"] = "changed"
	if c.sessionCopy().Cookies["SESSDATA"] != "fixture" {
		t.Fatal("session map aliases caller")
	}
	bad := c.sessionCopy()
	bad.Cookies["SESSDATA"] = "invalid; Domain=evil"
	if err := c.UseSession(bad); err == nil {
		t.Fatal("invalid cookie accepted")
	}
	encoded, _ := json.Marshal(c.sessionCopy())
	if !strings.Contains(string(encoded), "fixture") {
		t.Fatal("cannot persist owned session")
	}
}
