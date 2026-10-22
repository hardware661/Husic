package platform

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"music/internal/source"
)

func kugouFixtureSession() Session {
	return Session{ID: "42", Cookies: map[string]string{"userid": "42", "token": "fixture-token", "KUGOU_API_MID": "123456789", "KUGOU_API_DEV": "MUSIC12345", "dfid": "fixture-dfid"}}
}

func TestKugouStandardSignaturesUseDecodedParametersAndExactBody(t *testing.T) {
	q := url.Values{"appid": {"1005"}, "clienttime": {"123"}, "mid": {"456"}, "keyword": {"喜欢 A&B"}}
	body := []byte(`{"userid":"42"}`)
	if got := kugouSignature(q, body, true); got != "c69951eff4eef72d78fa4ca2f8073552" {
		t.Fatal("web protocol signature does not match upstream fixture")
	}
	if got := kugouSignature(q, body, false); got != "0563c8a8097dbe6369b4c933a815cdd6" {
		t.Fatal("standard app signature does not match upstream fixture")
	}
}

func TestKugouQRNeedsConfirmationAndSeparateIdentityCheck(t *testing.T) {
	state, wrongIdentity := 1, false
	c := &Client{kind: "kugou", direct: &http.Client{}}
	c.direct.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "login-user.kugou.com" && r.URL.Host != "gateway.kugou.com" && r.URL.Host != "userservice.kugou.com" {
			t.Fatal("credential sent to an uncompiled origin")
		}
		if r.URL.Query().Get("signature") == "" || r.Header.Get("mid") == "" {
			t.Fatal("device-bound signature missing")
		}
		switch r.URL.Path {
		case "/v2/qrcode":
			if r.URL.Query().Get("appid") != "1001" || !strings.Contains(r.URL.Query().Get("qrcode_txt"), "appid=1005") {
				t.Fatal("standard and lite QR protocols mixed")
			}
			return reply(`{"status":1,"error_code":0,"data":{"qrcode":"fixture-qr"}}`), nil
		case "/v2/get_userinfo_qrcode":
			if r.URL.Query().Get("appid") != "1005" || r.URL.Query().Get("qrcode") != "fixture-qr" {
				t.Fatal("poll lost standard-app QR binding")
			}
			body, _ := json.Marshal(map[string]any{"status": 1, "error_code": 0, "data": map[string]any{"status": state, "userid": 42, "token": "fixture-token", "nickname": "Listener"}})
			return reply(string(body)), nil
		case "/risk/v2/r_register_dev":
			return nil, errors.New("device registration deliberately unavailable")
		case "/v3/get_my_info":
			if r.URL.Query().Get("token") != "fixture-token" || r.Header.Get("x-router") != "usercenter.kugou.com" {
				t.Fatal("candidate credentials not verified")
			}
			if wrongIdentity {
				return reply(`{"status":1,"data":{"userid":99,"nickname":"Wrong"}}`), nil
			}
			return reply(`{"status":1,"data":{"userid":42,"nickname":"Listener"}}`), nil
		default:
			t.Fatal("unexpected control path")
			return nil, ErrResponse
		}
	})
	login, err := c.kugouStart(context.Background())
	if err != nil || len(login.Image) < 100 || login.Key == "" {
		t.Fatal("QR generation", err)
	}
	for _, want := range []string{"waiting", "scanned", "confirmed"} {
		result, err := c.kugouPoll(context.Background(), login.Key)
		if err != nil || result.State != want {
			t.Fatal("QR state", result.State, err)
		}
		if want != "confirmed" && result.Session != nil {
			t.Fatal("unconfirmed QR promoted credentials")
		}
		if want == "confirmed" {
			if result.Session == nil {
				t.Fatal("confirmed QR missing candidate")
			}
			profile, err := c.kugouVerify(context.Background(), *result.Session)
			if err != nil || profile.ID != "42" || profile.Name != "Listener" {
				t.Fatal("actual identity not verified", err)
			}
			wrongIdentity = true
			if _, err := c.kugouVerify(context.Background(), *result.Session); !errors.Is(err, ErrExpired) {
				t.Fatal("a different upstream identity was accepted")
			}
		}
		if c.Identity() != "" {
			t.Fatal("candidate installed before account manager commit")
		}
		state = map[int]int{1: 2, 2: 4}[state]
	}
}

func TestKugouExpiredQRMakesNoNetworkRequest(t *testing.T) {
	c := &Client{kind: "kugou", direct: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("expired QR made a network request")
		return nil, ErrResponse
	})}}
	encoded, _ := json.Marshal(kugouQR{Code: "fixture-qr", Cookies: map[string]string{"KUGOU_API_MID": "123", "KUGOU_API_DEV": "MUSIC12345"}, Expires: time.Now().Add(-time.Second).Unix()})
	result, err := c.kugouPoll(context.Background(), base64.RawURLEncoding.EncodeToString(encoded))
	if err != nil || result.State != "expired" || result.Session != nil {
		t.Fatal("expired QR accepted", err)
	}
}

func TestKugouPrivateCollectionUsesOwnedListTypeAndPagination(t *testing.T) {
	c := &Client{kind: "kugou", session: kugouFixtureSession(), direct: &http.Client{}}
	fileCalls := 0
	c.direct.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "gateway.kugou.com" || r.URL.Query().Get("token") != "fixture-token" {
			t.Fatal("private collection route or token changed")
		}
		switch r.URL.Path {
		case "/v7/get_all_list":
			return reply(`{"status":1,"data":{"info":[{"listid":7,"name":"Collected","type":1,"count":101}],"total":1}}`), nil
		case "/v4/get_list_all_file_v3":
			fileCalls++
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if integer(body["type"]) != 1 || integer(body["page"]) != 1 || text(body["listid"]) != "7" {
				t.Fatal("collected list read as a created list")
			}
			return reply(`{"status":1,"data":{"count":101,"info":[{"hash":"0123456789abcdef0123456789abcdef","filename":"Singer - Song","timelen":210000,"album_sizable_cover":"https://example.test/{size}.jpg"},{"hash":"not-a-hash","filename":"Singer - Broken"}]}}`), nil
		default:
			t.Fatal("unexpected collection endpoint")
			return nil, ErrResponse
		}
	})
	page, err := c.kugouCollection(context.Background(), "7", 1)
	if err != nil || page.Next != 2 || page.Total != 101 || len(page.Tracks) != 1 || page.Tracks[0].Duration != 210 || page.Tracks[0].Title != "Song" || !strings.Contains(page.Tracks[0].Cover, "400.jpg") {
		t.Fatal("collection parsing or pagination failed", err)
	}
	if _, err := c.kugouCollection(context.Background(), "99", 1); !errors.Is(err, source.ErrUnavailable) || fileCalls != 1 {
		t.Fatal("unowned list queried as private collection", err)
	}
}

func TestKugouStreamRejectsPreviewAndLeavesMediaDirect(t *testing.T) {
	c := &Client{kind: "kugou", session: kugouFixtureSession(), direct: &http.Client{}}
	requests := 0
	c.direct.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Host != "gateway.kugou.com" || r.URL.Path != "/v5/url" || r.URL.Query().Get("IsFreePart") != "0" || r.URL.Query().Get("key") == "" {
			t.Fatal("unexpected playback request")
		}
		if requests == 1 {
			return reply(`{"status":1,"is_free_part":1,"url":["https://fixture.kugou.com/preview.mp3"],"timeLength":210000}`), nil
		}
		return reply(`{"status":1,"url":["http://fixture.kugou.com/full.mp3"],"timeLength":210000,"bitRate":128000,"extName":"mp3"}`), nil
	})
	stream, err := c.kugouStream(context.Background(), source.Track{Source: "kugou", ID: "0123456789abcdef0123456789abcdef", Duration: 210})
	if err != nil || requests != 2 || stream.URL != "https://fixture.kugou.com/full.mp3" || stream.Seconds != 210 || stream.Bitrate != 128000 {
		t.Fatal("preview accepted or CDN was proxied", err)
	}
	bad := kugouFixtureSession()
	bad.Cookies["unexpected"] = "credential"
	if err := kugouValidate(bad); err == nil {
		t.Fatal("unlisted credential was accepted")
	}
	if _, err := kugouAESDecrypt([]byte{1, 2, 3}, "fixture"); err == nil {
		t.Fatal("invalid encrypted device response accepted")
	}
}
