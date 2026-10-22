package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skip2/go-qrcode"
)

// ptqrlogin can issue the qq.com login cookies separately from the
// graph.qq.com service ticket. Both must survive within the same attempt.
func TestQQConfirmationCarriesLoginCookiesToGraph(t *testing.T) {
	c, _ := New("qq", "")
	c.direct.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		x := reply("")
		switch r.URL.Host + r.URL.Path {
		case "ssl.ptlogin2.qq.com/ptqrlogin":
			x = reply(`ptuiCB('0','0','https://ssl.ptlogin2.graph.qq.com/check_sig?uin=42&ptsigx=fixture-sig','0','ok','Listener');`)
			x.Header.Add("Set-Cookie", "uin=o0000000042; Domain=.qq.com; Path=/; Secure")
			x.Header.Add("Set-Cookie", "skey=fixture-login; Domain=.qq.com; Path=/; Secure")
			x.Header.Add("Set-Cookie", "ptcz=do-not-forward; Domain=.qq.com; Path=/; Secure")
		case "ssl.ptlogin2.graph.qq.com/check_sig":
			if v, err := r.Cookie("skey"); err != nil || v.Value != "fixture-login" {
				t.Error("confirmation lost QQ login cookie")
			}
			x.StatusCode = 302
			x.Header.Add("Set-Cookie", "p_skey=fixture-graph; Domain=.graph.qq.com; Path=/; Secure")
			x.Header.Add("Set-Cookie", "p_uin=o0000000042; Domain=.graph.qq.com; Path=/; Secure")
		case "graph.qq.com/oauth2.0/authorize":
			_ = r.ParseForm()
			for name, want := range map[string]string{"skey": "fixture-login", "uin": "o0000000042", "p_skey": "fixture-graph"} {
				if v, err := r.Cookie(name); err != nil || v.Value != want {
					t.Errorf("Graph lost %s", name)
				}
			}
			if _, err := r.Cookie("ptcz"); err == nil {
				t.Error("unneeded cookie forwarded")
			}
			x.StatusCode = 302
			x.Header.Set("Location", "https://y.qq.com/portal/wx_redirect.html?code=fixture-code&state="+r.Form.Get("state"))
		default:
			t.Fatal("unexpected request", r.URL.Host)
		}
		return x, nil
	})
	c.music.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		return reply(`{"code":0,"req_0":{"code":0,"data":{"musicid":42,"musickey":"fixture-music"}}}`), nil
	})
	r, err := c.Poll(context.Background(), "fixture-qr")
	if err != nil || r.State != "confirmed" {
		t.Fatal(r.State, err)
	}
}

func TestQQGraphErrorIsNotMisreportedAsCallbackFormat(t *testing.T) {
	_, err := qqAuthorizationCode("https://graph.qq.com/oauth2.0/show?error=100035&which=error&state=nonce", "nonce")
	var f *Failure
	if !errors.As(err, &f) || f.Code != 100035 || strings.Contains(err.Error(), "固定流程") {
		t.Fatalf("lost actual platform reason: %v", err)
	}
}

func TestQQLoginExchangesOnlyCompiledEndpoints(t *testing.T) {
	c, _ := New("qq", "")
	image, _ := qrcode.Encode("https://example.test/qr", qrcode.Medium, 100)
	transport := roundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host + r.URL.Path {
		case "ssl.ptlogin2.qq.com/ptqrshow":
			x := reply(string(image))
			x.Header.Add("Set-Cookie", "qrsig=fixture-qr; Domain=.ptlogin2.qq.com; Path=/")
			return x, nil
		case "ssl.ptlogin2.qq.com/ptqrlogin":
			return reply(`ptuiCB('0','0','https://ssl.ptlogin2.graph.qq.com/check_sig?uin=42&ptsigx=fixture-sig','0','ok','Listener');`), nil
		case "ssl.ptlogin2.graph.qq.com/check_sig":
			if r.URL.Query().Get("uin") != "42" {
				t.Fatal("confirmation identity lost")
			}
			x := reply("")
			x.StatusCode = 302
			x.Header.Add("Set-Cookie", "p_skey=fixture-graph; Domain=.graph.qq.com; Path=/")
			return x, nil
		case "graph.qq.com/oauth2.0/authorize":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("client_id") != "100497308" || r.Form.Get("state") == "" {
				t.Fatal("music authorization missing")
			}
			if len(r.Form.Get("ui")) != 36 || r.Form.Get("ui") == r.Form.Get("state") {
				t.Fatal("Graph ui must be a separate UUID")
			}
			x := reply("")
			x.StatusCode = 302
			x.Header.Set("Location", "https://y.qq.com/portal/wx_redirect.html?code=fixture-code&state="+url.QueryEscape(r.Form.Get("state")))
			return x, nil
		case "u.y.qq.com/cgi-bin/musicu.fcg":
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Fatal("bad music request")
			}
			rpc := object(body["req_0"])
			switch text(rpc["method"]) {
			case "QQLogin":
				return reply(`{"code":0,"req_0":{"code":0,"data":{"musicid":42,"musickey":"fixture-music"}}}`), nil
			case "GetLoginUserInfo":
				if object(body["comm"])["authst"] != "fixture-music" {
					t.Fatal("candidate not used for verification")
				}
				return reply(`{"code":0,"req_0":{"code":0,"data":{"userInfo":{"musicid":42,"nick":"Listener"}}}}`), nil
			default:
				t.Fatal("unexpected music method")
			}
		default:
			t.Fatal("uncompiled authorization endpoint", r.URL.Host)
		}
		return nil, ErrResponse
	})
	c.direct.Transport = transport
	c.music.Transport = transport
	qr, err := c.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.Poll(context.Background(), qr.Key)
	if err != nil || result.State != "confirmed" || result.Session == nil {
		t.Fatalf("%+v %v", result, err)
	}
	profile, err := c.Verify(context.Background(), *result.Session)
	if err != nil || profile.ID != "42" {
		t.Fatal("verified identity", err)
	}
	if c.Identity() != "" {
		t.Fatal("candidate promoted itself")
	}
}

func TestQQRejectedRedirectCanBeVerifiedWithoutNewScan(t *testing.T) {
	c, _ := New("qq", "")
	c.SetPendingDirectory(t.TempDir())
	c.rememberQQRedirect("nonce", "http://y.qq.com/portal/wx_redirect.html?code=fixture-code&state=nonce")
	info, err := os.Stat(filepath.Join(c.pendingDir, "qq-authorization-pending.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private redirect permissions", err)
	}
	c.music.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if object(object(body["req_0"])["param"])["code"] != "fixture-code" {
			t.Fatal("wrong continuation code")
		}
		return reply(`{"code":0,"req_0":{"code":0,"data":{"musicid":42,"musickey":"fixture-music"}}}`), nil
	})
	session, err := c.ResumeQQ(context.Background())
	if err != nil || session.ID != "42" {
		t.Fatal("private continuation", err)
	}
	if c.Identity() != "" {
		t.Fatal("unverified continuation installed itself")
	}
	body, _ := json.Marshal(pendingQQ{State: "nonce", Location: "https://y.qq.com/portal/wx_redirect.html?code=fixture-code&state=nonce", Expires: time.Now().Add(-time.Second)})
	_ = os.WriteFile(filepath.Join(c.pendingDir, "qq-authorization-pending.json"), body, 0600)
	if _, err := c.ResumeQQ(context.Background()); err == nil {
		t.Fatal("expired continuation accepted")
	}
}
