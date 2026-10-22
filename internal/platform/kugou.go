package platform

// Protocol adapted from MakcRe/KuGouMusicApi (MIT), snapshot
// 514f870db7e60ca37bc5535be5da36d32b571c6f. This is the standard app,
// not Kugou Lite: their account tokens and signing keys are not interchangeable.

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
)

const kugouAppID = "1005"
const kugouClientVersion = "20489"

var kugouMID = regexp.MustCompile(`^[1-9][0-9]{0,38}$`)
var kugouDevice = regexp.MustCompile(`^[A-Z0-9]{10}$`)
var kugouHash = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)

func kugouValidate(s Session) error {
	if !numericID.MatchString(s.ID) || s.Cookies["userid"] != s.ID || len(s.Cookies) > 5 || !kugouMID.MatchString(s.Cookies["KUGOU_API_MID"]) || !kugouDevice.MatchString(s.Cookies["KUGOU_API_DEV"]) || len(s.Cookies["token"]) > 512 || !validCookie(s.Cookies["token"]) {
		return ErrExpired
	}
	for name, value := range s.Cookies {
		switch name {
		case "token", "userid", "KUGOU_API_MID", "KUGOU_API_DEV", "dfid":
		default:
			return ErrExpired
		}
		if !validCookie(value) {
			return ErrExpired
		}
	}
	return nil
}

func kugouMD5(s string) string {
	d := md5.Sum([]byte(s))
	return hex.EncodeToString(d[:])
}

func kugouSignature(q url.Values, body []byte, web bool) string {
	salt := "OIlwieks28dk2k092lksi2UIkp"
	if web {
		salt = "NVPh5oo715z5DIWAeQlhMDsWXXQV4hwt"
	}
	keys := make([]string, 0, len(q))
	for key := range q {
		if key != "signature" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var value strings.Builder
	value.WriteString(salt)
	for _, key := range keys {
		value.WriteString(key)
		value.WriteByte('=')
		value.WriteString(q.Get(key))
	}
	value.Write(body)
	value.WriteString(salt)
	return kugouMD5(value.String())
}

// Only these compiled HTTPS origins may receive the account token. All control
// calls use the client's China-only transport; no redirects or CDN fetches.
func (c *Client) kugouRequest(ctx context.Context, host, path, router string, query url.Values, body []byte, cookies map[string]string, web bool) ([]byte, error) {
	if host != "login-user.kugou.com" && host != "gateway.kugou.com" && host != "userservice.kugou.com" {
		return nil, ErrResponse
	}
	q := url.Values{"dfid": {"-"}, "mid": {cookies["KUGOU_API_MID"]}, "uuid": {"-"}, "appid": {kugouAppID}, "clientver": {kugouClientVersion}, "clienttime": {strconv.FormatInt(time.Now().Unix(), 10)}}
	if cookies["dfid"] != "" {
		q.Set("dfid", cookies["dfid"])
	}
	for _, key := range []string{"token", "userid"} {
		if cookies[key] != "" {
			q.Set(key, cookies[key])
		}
	}
	for key, values := range query {
		if len(values) != 1 {
			return nil, ErrResponse
		}
		q.Set(key, values[0])
	}
	q.Set("signature", kugouSignature(q, body, web))
	u := url.URL{Scheme: "https", Host: host, Path: path, RawQuery: q.Encode()}
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("User-Agent", "Android15-1070-11083-46-0-DiscoveryDRADProtocol-wifi")
	req.Header.Set("Referer", "https://www.kugou.com/")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("dfid", q.Get("dfid"))
	req.Header.Set("mid", q.Get("mid"))
	req.Header.Set("clienttime", q.Get("clienttime"))
	req.Header.Set("kg-rc", "1")
	req.Header.Set("kg-thash", "5d816a0")
	req.Header.Set("kg-rec", "1")
	req.Header.Set("kg-rf", "B9EDA08A64250DEFFBCADDEE00F8F25F")
	if router != "" {
		req.Header.Set("x-router", router)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		if host == "userservice.kugou.com" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	response, err := c.direct.Do(req)
	if err != nil {
		return nil, failure("kugou"+path, -1, ErrUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		return nil, ErrExpired
	}
	if response.StatusCode != http.StatusOK {
		return nil, failure("kugou"+path, response.StatusCode, ErrUnavailable)
	}
	if response.Header.Get("ssa-code") != "" {
		return nil, errors.New("酷狗需要安全验证，请先在酷狗 App 中完成验证")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return nil, ErrResponse
	}
	return raw, nil
}

func (c *Client) kugouCall(ctx context.Context, host, path, router string, q url.Values, body any, cookies map[string]string, web bool) (map[string]any, error) {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return nil, ErrResponse
		}
	}
	raw, err := c.kugouRequest(ctx, host, path, router, q, encoded, cookies, web)
	if err != nil {
		return nil, err
	}
	d, err := decode(raw)
	if err != nil || d["status"] == nil {
		return nil, ErrResponse
	}
	if integer(d["status"]) != 1 || integer(d["error_code"]) != 0 {
		return nil, failure("kugou"+path, integer(d["error_code"]), errors.New("酷狗未接受这次请求，请检查账号连接"))
	}
	data := object(d["data"])
	if data == nil {
		return nil, ErrResponse
	}
	return data, nil
}

func kugouNewDevice() (map[string]string, error) {
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return nil, ErrUnavailable
	}
	digest := md5.Sum(random[:16])
	mid := new(big.Int).SetBytes(digest[:]).String()
	return map[string]string{"KUGOU_API_MID": mid, "KUGOU_API_DEV": strings.ToUpper(hex.EncodeToString(random[16:21]))}, nil
}

type kugouQR struct {
	Code    string            `json:"code"`
	Cookies map[string]string `json:"device"`
	Expires int64             `json:"expires"`
}

func (c *Client) kugouStart(ctx context.Context) (Login, error) {
	cookies, err := kugouNewDevice()
	if err != nil {
		return Login{}, err
	}
	d, err := c.kugouCall(ctx, "login-user.kugou.com", "/v2/qrcode", "", url.Values{"appid": {"1001"}, "type": {"1"}, "plat": {"4"}, "qrcode_txt": {"https://h5.kugou.com/apps/loginQRCode/html/index.html?appid=1005&"}, "srcappid": {"2919"}}, nil, cookies, true)
	if err != nil {
		return Login{}, err
	}
	code := text(d["qrcode"])
	if len(code) > 512 || !validCookie(code) {
		return Login{}, ErrResponse
	}
	link := "https://h5.kugou.com/apps/loginQRCode/html/index.html?qrcode=" + url.QueryEscape(code)
	image, err := qrcode.Encode(link, qrcode.Medium, 320)
	if err != nil {
		return Login{}, ErrResponse
	}
	key, _ := json.Marshal(kugouQR{Code: code, Cookies: cookies, Expires: time.Now().Add(3 * time.Minute).Unix()})
	return Login{Key: base64.RawURLEncoding.EncodeToString(key), Image: image}, nil
}

func (c *Client) kugouPoll(ctx context.Context, key string) (LoginResult, error) {
	if len(key) > 2048 {
		return LoginResult{}, ErrResponse
	}
	raw, err := base64.RawURLEncoding.DecodeString(key)
	var qr kugouQR
	if err != nil || json.Unmarshal(raw, &qr) != nil || !validCookie(qr.Code) || len(qr.Code) > 512 || len(qr.Cookies) != 2 || !kugouMID.MatchString(qr.Cookies["KUGOU_API_MID"]) || !kugouDevice.MatchString(qr.Cookies["KUGOU_API_DEV"]) {
		return LoginResult{}, ErrResponse
	}
	if qr.Expires <= time.Now().Unix() || qr.Expires > time.Now().Add(4*time.Minute).Unix() {
		return LoginResult{State: "expired"}, nil
	}
	d, err := c.kugouCall(ctx, "login-user.kugou.com", "/v2/get_userinfo_qrcode", "", url.Values{"plat": {"4"}, "srcappid": {"2919"}, "qrcode": {qr.Code}, "dev": {qr.Cookies["KUGOU_API_DEV"]}}, nil, qr.Cookies, true)
	if err != nil {
		return LoginResult{}, err
	}
	if d["status"] == nil {
		return LoginResult{}, ErrResponse
	}
	switch integer(d["status"]) {
	case 0:
		return LoginResult{State: "expired"}, nil
	case 1:
		return LoginResult{State: "waiting"}, nil
	case 2:
		return LoginResult{State: "scanned"}, nil
	case 4:
		s := Session{ID: text(d["userid"]), Name: first(d, "nickname", "nick_name"), Cookies: cloneCookies(qr.Cookies)}
		s.Cookies["userid"], s.Cookies["token"] = s.ID, text(d["token"])
		if err := kugouValidate(s); err != nil {
			return LoginResult{}, err
		}
		// A device registration failure does not turn a valid account into an
		// invalid login. Playback will retry this one prerequisite once.
		if dfid, err := c.kugouRegisterDevice(ctx, s); err == nil {
			s.Cookies["dfid"] = dfid
		}
		return LoginResult{State: "confirmed", Session: &s}, nil
	default:
		return LoginResult{}, errors.New("酷狗未接受这次授权，请在酷狗 App 中检查")
	}
}

func (c *Client) kugouVerify(ctx context.Context, s Session) (Profile, error) {
	if err := kugouValidate(s); err != nil {
		return Profile{}, err
	}
	now := time.Now().Unix()
	p, err := kugouRSA(map[string]any{"token": s.Cookies["token"], "clienttime": now}, false)
	if err != nil {
		return Profile{}, err
	}
	d, err := c.kugouCall(ctx, "gateway.kugou.com", "/v3/get_my_info", "usercenter.kugou.com", url.Values{"plat": {"1"}}, map[string]any{"visit_time": now, "usertype": 1, "p": p, "userid": json.Number(s.ID)}, s.Cookies, false)
	if err != nil {
		return Profile{}, err
	}
	if first(d, "userid", "user_id") != s.ID {
		return Profile{}, ErrExpired
	}
	return Profile{ID: s.ID, Name: first(d, "nickname", "nick_name")}, nil
}
