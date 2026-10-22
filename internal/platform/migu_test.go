package platform

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"music/internal/source"
)

func TestMiguSMSCandidateIsEncryptedVerifiedAndNotInstalled(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	const id = "dab35be9-bc34-43f0-8136-ccc948daed38"
	const phone, sms = "13900001234", "123456"
	var paths []string
	c := &Client{kind: "migu"}
	c.direct = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
		}
		if strings.Contains(r.URL.RawQuery, phone) || strings.Contains(string(body), phone) || strings.Contains(string(body), "dynamicPassword="+sms) {
			t.Fatal("phone or SMS was not encrypted")
		}
		decrypt := func(value, want string) {
			t.Helper()
			ciphertext, err := hex.DecodeString(value)
			if err != nil {
				t.Fatal(err)
			}
			plain, err := rsa.DecryptPKCS1v15(rand.Reader, key, ciphertext)
			if err != nil || string(plain) != want {
				t.Fatalf("encrypted field did not decrypt: %v", err)
			}
		}
		switch r.URL.Path {
		case "/password/publickey":
			if r.URL.Host != "passport.migu.cn" || r.Method != "POST" {
				t.Fatal("public key origin or method changed")
			}
			return reply(fmt.Sprintf(`{"status":2000,"result":{"publicExponent":"010001","modulus":"%x"}}`, key.N)), nil
		case "/login/dynamicpassword":
			if r.Method != "GET" || r.URL.Query().Get("sourceID") != "220029" {
				t.Fatal("unexpected send-code protocol")
			}
			decrypt(r.URL.Query().Get("msisdn"), phone)
			return reply(`{"status":2000}`), nil
		case "/authn/dynamicpassword":
			if r.Method != "POST" || r.Header.Get("Origin") != "https://passport.migu.cn" {
				t.Fatal("unexpected confirmation protocol")
			}
			form, _ := url.ParseQuery(string(body))
			decrypt(form.Get("msisdn"), phone)
			decrypt(form.Get("dynamicPassword"), sms)
			return reply(`{"status":2000,"result":{"token":"temporary-ticket","redirectURL":"https://evil.example/"}}`), nil
		case "/user/h5/token-validate/v3.0":
			if r.URL.Host != "c.musicapp.migu.cn" || r.URL.Query().Get("token") != "temporary-ticket" {
				t.Fatal("login ticket was not exchanged at compiled origin")
			}
			x := reply(`{"code":"000000","data":{"userId":"` + id + `"}}`)
			x.Header.Add("Set-Cookie", "unrelated=not-retained; Domain=.migu.cn; Path=/")
			x.Header.Add("Set-Cookie", "pacmtoken=verified-account-token; Domain=.migu.cn; Path=/; Secure")
			return x, nil
		case "/pc/user/h5/queryUserInfo/v1.0":
			cookie, err := r.Cookie("pacmtoken")
			if err != nil || cookie.Value != "verified-account-token" || r.URL.Host != "app.c.nf.migu.cn" {
				t.Fatal("identity was not verified with candidate credential")
			}
			return reply(`{"code":"000000","data":{"userId":"` + id + `","nickname":"Listener"}}`), nil
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
			return nil, errors.New("unexpected")
		}
	})}
	if err = c.MiguSendCode(context.Background(), phone); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatal("one user action must send only one SMS")
	}
	s, err := c.MiguLogin(context.Background(), phone, sms)
	if err != nil || s.ID != id || s.Name != "Listener" || len(s.Cookies) != 1 || len(paths) != 6 {
		t.Fatalf("candidate flow: id/name/cookie-count/request-count %v", err)
	}
	if c.Identity() != "" {
		t.Fatal("candidate was installed without manager commit")
	}
}

func TestMiguSessionIdentityAndCookieScope(t *testing.T) {
	for _, id := range []string{"15671224593300417251392", "dab35be9-bc34-43f0-8136-ccc948daed38"} {
		s := Session{ID: id, Cookies: map[string]string{"pacmtoken": "fixture-token"}}
		if err := MiguValidateSession(s); err != nil {
			t.Fatal("real Migu identity format was rejected")
		}
		s.Cookies["unrelated"] = "bad"
		if MiguValidateSession(s) == nil {
			t.Fatal("unrelated account cookie was accepted")
		}
	}
	c := &Client{kind: "migu", direct: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return reply(`{"code":"000000","data":{"userId":"another-account"}}`), nil
	})}}
	_, err := c.miguVerify(context.Background(), Session{ID: "own-account", Cookies: map[string]string{"pacmtoken": "fixture-token"}})
	if !errors.Is(err, ErrExpired) {
		t.Fatal("different account was accepted")
	}
	for _, raw := range []string{`{"code":"850001","info":"未登录"}`, `{"code":"299999","info":"uid不能为空"}`} {
		if _, err := miguBody([]byte(raw), "migu.identity"); !errors.Is(err, ErrExpired) {
			t.Fatal("HTTP 200 unauthenticated response was accepted")
		}
	}
}

func TestMiguInvalidInputDoesNotSendAndUpstreamCannotEchoPhone(t *testing.T) {
	c := &Client{kind: "migu", direct: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid form made an upstream request")
		return nil, errors.New("unexpected")
	})}}
	if c.MiguSendCode(context.Background(), "not-a-phone") == nil {
		t.Fatal("invalid phone accepted")
	}
	if _, err := c.MiguLogin(context.Background(), "13900001234", "x<script>"); err == nil {
		t.Fatal("invalid SMS accepted")
	}
	_, err := miguAuthStatus([]byte(`{"status":4001,"message":"13900001234 was blocked"}`), "migu.sms.send")
	if err == nil || strings.Contains(err.Error(), "13900001234") {
		t.Fatal("upstream phone echo reached public error")
	}
}

func TestMiguPrivateCollectionPaginationAndTrackShape(t *testing.T) {
	c := &Client{kind: "migu", session: Session{ID: "fixture-account", Cookies: map[string]string{"pacmtoken": "fixture-token"}}}
	c.direct = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "app.c.nf.migu.cn" || r.URL.Query().Get("playlistId") != "231760782" || r.URL.Query().Get("pageNo") != "1" {
			t.Fatal("wrong private library target")
		}
		if cookie, _ := r.Cookie("pacmtoken"); cookie == nil || cookie.Value != "fixture-token" {
			t.Fatal("private library did not use account")
		}
		return reply(`{"code":"000000","data":{"totalCount":51,"songList":[{"resourceType":"2","contentId":"600902000006889506","songName":"可爱女人","duration":239,"copyrightId":"60054701888","album":"Jay","img3":"/data/cover.webp","singerList":[{"name":"周杰伦"}]}]}}`), nil
	})}
	page, err := c.miguCollection(context.Background(), "231760782", 1)
	if err != nil || len(page.Tracks) != 1 || page.Total != 51 || page.Next != 2 {
		t.Fatalf("page: %v", err)
	}
	track := page.Tracks[0]
	if track.ID != "600902000006889506" || track.Source != "migu" || track.Cover != "https://d.musicapp.migu.cn/data/cover.webp" || track.Extra["copyrightId"] != "60054701888" {
		t.Fatal("playlist identifiers cannot reach player intact")
	}
}

func TestMiguUnknownLibraryShapeIsNotReportedAsEmpty(t *testing.T) {
	out := []Collection{}
	if miguCollectCollections(map[string]any{"newUpstreamShape": "unexpected"}, 0, &out, map[string]bool{}) {
		t.Fatal("unknown shape was accepted as empty library")
	}
	if !miguCollectCollections(map[string]any{"musicList": []any{}}, 0, &out, map[string]bool{}) {
		t.Fatal("explicit empty playlist list was rejected")
	}
}

func miguListenFixture(raw string) []byte {
	const key = "Jk8qzuePiJ1qE3mDYhLQ3T73DtDoAhLP"
	encoded := []byte{171, 205, 1, 37}
	for i, b := range []byte(raw) {
		encoded = append(encoded, b-37+key[i%len(key)])
	}
	return encoded
}

func TestMiguListenKeepsRightsSignatureAndMediaOffControlTransport(t *testing.T) {
	const raw = `{"code":"000000","data":{"url":"https://freetyst.nf.migu.cn/full/song.mp3?signature=keep%2Bthis&expires=123","audioFormatType":"PQ","freeListenType":"0","song":{"contentId":"600929000003297424","duration":136}}}`
	calls := 0
	c := &Client{kind: "migu", session: Session{ID: "fixture-account", Cookies: map[string]string{"pacmtoken": "fixture-token"}}}
	c.direct = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "app.c.nf.migu.cn" || r.URL.Path != "/strategy/pc/listen/v2.0" || r.Header.Get("pacmtoken") != "fixture-token" || r.URL.Query().Get("toneFlag") != "PQ" || strings.Contains(r.URL.String(), "fixture-token") {
			t.Fatal("account listen control protocol changed")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(miguListenFixture(raw))))}, nil
	})}
	track := source.Track{Source: "migu", ID: "600929000003297424", Duration: 136}
	stream, err := c.miguStream(context.Background(), track)
	if err != nil || calls != 1 || stream.Seconds != 136 || stream.Format != "mp3" || !strings.Contains(stream.URL, "signature=keep%2Bthis&expires=123") {
		t.Fatalf("stream: %v", err)
	}
	for _, changed := range []string{
		strings.Replace(raw, `"freeListenType":"0"`, `"freeListenType":"1"`, 1),
		strings.Replace(raw, `"freeListenType":"0",`, ``, 1),
		strings.Replace(raw, `"contentId":"600929000003297424"`, `"contentId":"99"`, 1),
		strings.Replace(raw, `freetyst.nf.migu.cn`, `migu.cn.evil.example`, 1),
		strings.Replace(raw, `"duration":136`, `"duration":30`, 1),
	} {
		d, err := miguDecodeListen(miguListenFixture(changed))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := miguStreamResult(d, track); !errors.Is(err, source.ErrUnavailable) {
			t.Fatal("preview, wrong track, unknown rights or non-Migu media accepted")
		}
	}
}
