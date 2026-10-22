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
	c, err := New("bilibili", "")
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

func TestQQCallbackIsDataAndStateIsBound(t *testing.T) {
	args, err := qqParseCallback([]byte(`ptuiCB('0','0','https://evil.example/','0','ok','nick');`))
	if err != nil || len(args) != 6 {
		t.Fatal(err)
	}
	if _, _, err := qqSuccessIdentity(args[2]); err == nil {
		t.Fatal("arbitrary confirmation URL accepted")
	}
	if _, err := qqParseCallback([]byte(`ptuiCB('0','0','x','0','ok','nick');fetch('/steal')`)); err == nil {
		t.Fatal("JavaScript accepted")
	}
	if _, err := qqAuthorizationCode("https://y.qq.com/portal/wx_redirect.html?code=test&state=old", "new"); err == nil {
		t.Fatal("old OAuth state accepted")
	}
}

func TestQQCallbackAcceptsJavaScriptUnicodeStrings(t *testing.T) {
	args, err := qqParseCallback([]byte(`ptuiCB('0','0','https:\/\/ssl.ptlogin2.graph.qq.com/check_sig?uin=42&ptsigx=test','0','ok','\ud83d\udc31');`))
	if err != nil || args[5] != "🐱" {
		t.Fatalf("valid JavaScript nickname rejected: %v", err)
	}
	args, err = qqParseCallback([]byte(`ptuiCB('0','0','https://ssl.ptlogin2.graph.qq.com/check_sig?uin=42&ptsigx=test','0','ok','Listener','0');`))
	if err != nil || len(args) != 7 {
		t.Fatalf("bounded extra response field rejected: %v", err)
	}
}

func TestQQOfficialCallbackSpellingsRemainStateBound(t *testing.T) {
	for _, prefix := range []string{"https://y.qq.com", "http://y.qq.com:80", "//y.qq.com", "https://y.qq.com:443"} {
		code, err := qqAuthorizationCode(prefix+"/portal/wx_redirect.html?code=fixture&state=nonce", "nonce")
		if err != nil || code != "fixture" {
			t.Fatalf("official callback spelling: %v", err)
		}
		if _, err := qqAuthorizationCode(prefix+"/portal/wx_redirect.html?code=fixture&state=old", "nonce"); err == nil {
			t.Fatal("state mismatch accepted")
		}
	}
}

func TestSessionValidationAndCopies(t *testing.T) {
	c, _ := New("qq", "")
	s := Session{ID: "42", Cookies: map[string]string{"uin": "42", "qqmusic_uin": "42", "qqmusic_key": "fixture", "qm_keyst": "fixture"}}
	if err := c.UseSession(s); err != nil {
		t.Fatal(err)
	}
	s.Cookies["qqmusic_key"] = "changed"
	if c.sessionCopy().Cookies["qqmusic_key"] != "fixture" {
		t.Fatal("session map aliases caller")
	}
	bad := c.sessionCopy()
	bad.Cookies["qqmusic_key"] = "invalid; Domain=evil"
	if err := c.UseSession(bad); err == nil {
		t.Fatal("invalid cookie accepted")
	}
	encoded, _ := json.Marshal(c.sessionCopy())
	if !strings.Contains(string(encoded), "fixture") {
		t.Fatal("cannot persist owned session")
	}
}
