package platform

// The authentication protocol follows the current web client, independently
// implemented from Domdkw/miguMusic-api-enhanced (MIT), snapshot
// 87a4c2ffd2e35a878d2d60894893e35a00074afb. No password or SMS is retained.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	miguAccountID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	miguPhone     = regexp.MustCompile(`^1[3-9][0-9]{9}$`)
	miguSMSCode   = regexp.MustCompile(`^[0-9]{4,8}$`)
	miguContentID = regexp.MustCompile(`^[1-9][0-9]{0,23}$`)
)

// MiguValidateSession accepts the opaque/UUID user identities used by Migu,
// not QQ's numeric identity. Only its one music-scoped credential is stored.
func MiguValidateSession(s Session) error {
	if !miguAccountID.MatchString(s.ID) || len(s.Cookies) != 1 || !validCookie(s.Cookies["pacmtoken"]) {
		return ErrExpired
	}
	return nil
}

// miguRequest always uses the Music domestic control transport (Huawei when
// configured). Neither a response URL nor an upstream redirect is followed.
func (c *Client) miguRequest(ctx context.Context, host, path string, query, form url.Values, headers map[string]string, cookies map[string]string) (*http.Response, []byte, error) {
	if host != "passport.migu.cn" && host != "c.musicapp.migu.cn" && host != "app.c.nf.migu.cn" {
		return nil, nil, ErrResponse
	}
	method := http.MethodGet
	var reader io.Reader
	if form != nil {
		method, reader = http.MethodPost, strings.NewReader(form.Encode())
	}
	u := url.URL{Scheme: "https", Host: host, Path: path, RawQuery: query.Encode()}
	r, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, nil, ErrResponse
	}
	r.Header.Set("User-Agent", "Mozilla/5.0")
	r.Header.Set("Accept", "application/json, text/plain, */*")
	r.Header.Set("Referer", "https://music.migu.cn/")
	r.Header.Set("channel", "014X031")
	if host == "passport.migu.cn" {
		r.Header.Set("Referer", "https://passport.migu.cn/login")
		r.Header.Set("Origin", "https://passport.migu.cn")
	}
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	for k, v := range cookies {
		if k != "pacmtoken" && k != "idmpauth" || !validCookie(v) {
			return nil, nil, ErrExpired
		}
		r.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	response, err := c.direct.Do(r)
	if err != nil {
		return nil, nil, failure("migu"+path, -1, ErrUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return nil, nil, failure("migu"+path, response.StatusCode, ErrExpired)
	}
	if response.StatusCode != http.StatusOK {
		return nil, nil, failure("migu"+path, response.StatusCode, ErrUnavailable)
	}
	const limit = 4 << 20
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(raw) > limit {
		return nil, nil, ErrResponse
	}
	return response, raw, nil
}

func miguBody(raw []byte, stage string) (map[string]any, error) {
	d, err := decode(raw)
	if err != nil {
		return nil, failure(stage, -2, err)
	}
	code := text(d["code"])
	if code != "000000" && code != "0" {
		cause := ErrUnavailable
		// Observed official unauthenticated responses, not inferred from HTTP 200.
		if code == "850001" || code == "299999" || code == "200001" {
			cause = ErrExpired
		}
		return nil, failure(stage, integer(d["code"]), cause)
	}
	return d, nil
}

func (c *Client) miguPublicKey(ctx context.Context) (*rsa.PublicKey, error) {
	_, raw, err := c.miguRequest(ctx, "passport.migu.cn", "/password/publickey", nil, url.Values{}, nil, nil)
	if err != nil {
		return nil, err
	}
	d, err := decode(raw)
	if err != nil || integer(d["status"]) != 2000 {
		return nil, failure("migu.publickey", integer(d["status"]), ErrResponse)
	}
	result := object(d["result"])
	n, ok := new(big.Int).SetString(text(result["modulus"]), 16)
	e, eerr := strconv.ParseUint(text(result["publicExponent"]), 16, 31)
	if !ok || eerr != nil || e < 3 || e%2 == 0 || n.BitLen() < 1024 || n.BitLen() > 4096 {
		return nil, failure("migu.publickey", -2, ErrResponse)
	}
	return &rsa.PublicKey{N: n, E: int(e)}, nil
}

// PrepareMigu checks the real authentication transport without sending SMS.
func (c *Client) PrepareMigu(ctx context.Context) error {
	_, err := c.miguPublicKey(ctx)
	return err
}

func miguEncrypt(key *rsa.PublicKey, value string) (string, error) {
	b, err := rsa.EncryptPKCS1v15(rand.Reader, key, []byte(value))
	if err != nil {
		return "", ErrResponse
	}
	return hex.EncodeToString(b), nil
}

func miguAuthForm(key *rsa.PublicKey, phone, code string) (url.Values, error) {
	encryptedPhone, err := miguEncrypt(key, phone)
	if err != nil {
		return nil, err
	}
	fingerprint := make([]byte, 16)
	if _, err := rand.Read(fingerprint); err != nil {
		return nil, ErrUnavailable
	}
	fp := hex.EncodeToString(fingerprint)
	form := url.Values{"isAsync": {"true"}, "msisdn": {encryptedPhone}, "captcha": {""}, "sourceID": {"220029"}, "imgcodeType": {"2"}, "fingerPrint": {fp}, "fingerPrintDetail": {fp}}
	if code != "" {
		encryptedCode, err := miguEncrypt(key, code)
		if err != nil {
			return nil, err
		}
		form.Set("dynamicPassword", encryptedCode)
		form.Set("appType", "")
		form.Set("securityCode", "7619")
	}
	return form, nil
}

func miguAuthStatus(raw []byte, stage string) (map[string]any, error) {
	d, err := decode(raw)
	if err != nil {
		return nil, failure(stage, -2, err)
	}
	if integer(d["status"]) != 2000 {
		message := "咪咕未接受这次验证码，请检查后重试"
		if stage == "migu.sms.send" {
			message = "咪咕暂未发送验证码，请稍后再试"
		}
		// Never forward the upstream message: it may echo the phone number.
		return nil, failure(stage, integer(d["status"]), errors.New(message))
	}
	return d, nil
}

// MiguSendCode sends exactly one SMS request. It must only be called from an
// explicit user submission; the account manager owns cooldown/cancellation.
// The upstream web flow does not use a cookie jar between these requests.
func (c *Client) MiguSendCode(ctx context.Context, phone string) error {
	if !miguPhone.MatchString(phone) {
		return errors.New("请输入正确的手机号")
	}
	key, err := c.miguPublicKey(ctx)
	if err != nil {
		return err
	}
	query, err := miguAuthForm(key, phone, "")
	if err != nil {
		return err
	}
	query.Set("_", strconv.FormatInt(time.Now().UnixMilli(), 10))
	_, raw, err := c.miguRequest(ctx, "passport.migu.cn", "/login/dynamicpassword", query, nil, nil, nil)
	if err != nil {
		return err
	}
	_, err = miguAuthStatus(raw, "migu.sms.send")
	return err
}

// MiguLogin returns a verified candidate only. It never installs it, persists
// the phone/SMS code, follows redirectURL, or replaces an existing account.
func (c *Client) MiguLogin(ctx context.Context, phone, code string) (Session, error) {
	if !miguPhone.MatchString(phone) || !miguSMSCode.MatchString(code) {
		return Session{}, errors.New("请检查手机号和验证码")
	}
	key, err := c.miguPublicKey(ctx)
	if err != nil {
		return Session{}, err
	}
	form, err := miguAuthForm(key, phone, code)
	if err != nil {
		return Session{}, err
	}
	_, raw, err := c.miguRequest(ctx, "passport.migu.cn", "/authn/dynamicpassword", nil, form, nil, nil)
	if err != nil {
		return Session{}, err
	}
	d, err := miguAuthStatus(raw, "migu.sms.confirm")
	if err != nil {
		return Session{}, err
	}
	token := text(object(d["result"])["token"])
	if !validCookie(token) {
		return Session{}, failure("migu.sms.ticket", 0, ErrResponse)
	}
	r, raw, err := c.miguRequest(ctx, "c.musicapp.migu.cn", "/user/h5/token-validate/v3.0", url.Values{"token": {token}, "type": {"2"}, "sourceId": {"220029"}, "activityId": {"MUSIC-WWW"}}, nil, map[string]string{"Origin": "https://music.migu.cn", "Pragma": "no-cache"}, map[string]string{"idmpauth": "true@passport.migu.cn"})
	if err != nil {
		return Session{}, err
	}
	d, err = miguBody(raw, "migu.token.exchange")
	if err != nil {
		return Session{}, err
	}
	id := text(object(d["data"])["userId"])
	pacm := ""
	for _, cookie := range r.Cookies() {
		domain := strings.TrimPrefix(strings.ToLower(cookie.Domain), ".")
		if cookie.Name != "pacmtoken" || domain != "" && domain != "migu.cn" && domain != "musicapp.migu.cn" && domain != "c.musicapp.migu.cn" || cookie.Path != "" && cookie.Path != "/" || cookie.MaxAge < 0 || !cookie.Expires.IsZero() && cookie.Expires.Before(time.Now()) || !validCookie(cookie.Value) {
			continue
		}
		if pacm != "" && pacm != cookie.Value {
			return Session{}, failure("migu.token.cookie", 0, ErrResponse)
		}
		pacm = cookie.Value
	}
	s := Session{ID: id, Cookies: map[string]string{"pacmtoken": pacm}}
	if err := MiguValidateSession(s); err != nil {
		return Session{}, failure("migu.token.identity", 0, err)
	}
	profile, err := c.miguVerify(ctx, s)
	if err != nil {
		return Session{}, err
	}
	s.Name = profile.Name
	return s, nil
}

func (c *Client) miguVerify(ctx context.Context, s Session) (Profile, error) {
	if err := MiguValidateSession(s); err != nil {
		return Profile{}, err
	}
	_, raw, err := c.miguRequest(ctx, "app.c.nf.migu.cn", "/pc/user/h5/queryUserInfo/v1.0", nil, nil, nil, s.Cookies)
	if err != nil {
		return Profile{}, err
	}
	d, err := miguBody(raw, "migu.identity")
	if err != nil {
		return Profile{}, err
	}
	data := object(d["data"])
	rows := []map[string]any{data, object(data["userInfo"]), object(data["user"]), object(data["userInfoItem"])}
	id, name := "", ""
	for _, row := range rows {
		if v := first(row, "userId", "uid"); v != "" {
			if !miguAccountID.MatchString(v) || id != "" && id != v {
				return Profile{}, ErrResponse
			}
			id = v
		}
		if name == "" {
			name = first(row, "nickname", "nickName", "userName", "name")
		}
	}
	if id == "" || id != s.ID {
		return Profile{}, failure("migu.identity", 0, ErrExpired)
	}
	if name == "" {
		name = "咪咕音乐账号"
	}
	return Profile{ID: id, Name: name}, nil
}
