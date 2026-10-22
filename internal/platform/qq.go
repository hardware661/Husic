package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"time"
)

const qqMusicRedirect = "https://y.qq.com/portal/wx_redirect.html?login_type=1&surl=https://y.qq.com/"

func (c *Client) qqStart(ctx context.Context) (Login, error) {
	r, image, err := c.request(ctx, "ssl.ptlogin2.qq.com", "/ptqrshow", url.Values{
		"appid": {"716027609"}, "e": {"2"}, "l": {"M"}, "s": {"3"}, "d": {"72"}, "v": {"4"}, "daid": {"383"}, "pt_3rd_aid": {"100497308"}, "t": {strconv.FormatInt(time.Now().UnixMilli(), 10)},
	}, nil, nil, nil)
	if err != nil {
		return Login{}, err
	}
	sig, err := qqReadQRSignature(r, time.Now())
	if err != nil {
		return Login{}, err
	}
	config, err := png.DecodeConfig(bytes.NewReader(image))
	if err != nil || config.Width < 32 || config.Height < 32 || config.Width > 1024 || config.Height > 1024 {
		return Login{}, ErrResponse
	}
	decoded, err := png.Decode(bytes.NewReader(image))
	if err != nil {
		return Login{}, ErrResponse
	}
	var normalized bytes.Buffer
	if png.Encode(&normalized, decoded) != nil {
		return Login{}, ErrResponse
	}
	return Login{Key: sig, Image: normalized.Bytes()}, nil
}
func (c *Client) qqPoll(ctx context.Context, key string) (LoginResult, error) {
	if !validCookie(key) {
		return LoginResult{}, ErrResponse
	}
	r, raw, err := c.request(ctx, "ssl.ptlogin2.qq.com", "/ptqrlogin", url.Values{
		"u1": {"https://graph.qq.com/oauth2.0/login_jump"}, "ptqrtoken": {strconv.FormatUint(uint64(qqHash33(key, 0)), 10)},
		"ptredirect": {"0"}, "h": {"1"}, "t": {"1"}, "g": {"1"}, "from_ui": {"1"}, "ptlang": {"2052"},
		"action": {"0-0-" + strconv.FormatInt(time.Now().UnixMilli(), 10)}, "js_ver": {"20102616"}, "js_type": {"1"},
		"pt_uistyle": {"40"}, "aid": {"716027609"}, "daid": {"383"}, "pt_3rd_aid": {"100497308"}, "has_onekey": {"1"},
	}, nil, nil, map[string]string{"qrsig": key})
	if err != nil {
		return LoginResult{}, err
	}
	args, err := qqParseCallback(raw)
	if err != nil {
		return LoginResult{}, failure("qq.poll.callback", 0, err)
	}
	switch args[0] {
	case "66":
		return LoginResult{State: "waiting"}, nil
	case "67":
		return LoginResult{State: "scanned"}, nil
	case "65":
		return LoginResult{State: "expired"}, nil
	case "68":
		return LoginResult{State: "failed"}, errors.New("已取消 QQ 音乐授权")
	case "0":
		jar, _ := cookiejar.New(nil)
		qqKeepLoginCookies(jar, "ssl.ptlogin2.qq.com", r)
		s, err := c.qqAuthorize(ctx, args[2], jar)
		if err != nil {
			return LoginResult{State: "failed"}, err
		}
		return LoginResult{State: "confirmed", Session: &s}, nil
	default:
		return LoginResult{}, errors.New("QQ 暂未接受这次登录，请在 QQ 中检查")
	}
}
func (c *Client) qqAuthorize(ctx context.Context, confirmation string, jar *cookiejar.Jar) (Session, error) {
	uin, sig, err := qqSuccessIdentity(confirmation)
	if err != nil {
		return Session{}, failure("qq.confirmation.url", 0, err)
	}
	c.rememberQQConfirmation(confirmation, qqCookiesAt(jar, "ssl.ptlogin2.graph.qq.com"))
	r, _, err := c.request(ctx, "ssl.ptlogin2.graph.qq.com", "/check_sig", url.Values{
		"uin": {uin}, "pttype": {"1"}, "service": {"ptqrlogin"}, "nodirect": {"0"}, "ptsigx": {sig}, "s_url": {"https://graph.qq.com/oauth2.0/login_jump"},
		"ptlang": {"2052"}, "ptredirect": {"100"}, "aid": {"716027609"}, "daid": {"383"}, "j_later": {"0"}, "low_login_hour": {"0"},
		"regmaster": {"0"}, "pt_login_type": {"3"}, "pt_aid": {"0"}, "pt_aaid": {"16"}, "pt_light": {"0"}, "pt_3rd_aid": {"100497308"},
	}, nil, nil, qqCookiesAt(jar, "ssl.ptlogin2.graph.qq.com"))
	if err != nil {
		return Session{}, err
	}
	_, err = qqGraphCookies(r, time.Now())
	if err != nil {
		return Session{}, failure("qq.graph.cookies", r.StatusCode, err)
	}
	qqKeepLoginCookies(jar, "ssl.ptlogin2.graph.qq.com", r)
	cookies := qqCookiesAt(jar, "graph.qq.com")
	return c.qqGraphAuthorize(ctx, cookies)
}

func (c *Client) qqGraphAuthorize(ctx context.Context, cookies map[string]string) (Session, error) {
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return Session{}, ErrUnavailable
	}
	state := hex.EncodeToString(nonce)
	uiBytes := make([]byte, 16)
	if _, err := rand.Read(uiBytes); err != nil {
		return Session{}, ErrUnavailable
	}
	uiBytes[6] = (uiBytes[6] & 15) | 64
	uiBytes[8] = (uiBytes[8] & 63) | 128
	ui := fmt.Sprintf("%x-%x-%x-%x-%x", uiBytes[:4], uiBytes[4:6], uiBytes[6:8], uiBytes[8:10], uiBytes[10:])
	r, _, err := c.request(ctx, "graph.qq.com", "/oauth2.0/authorize", nil, url.Values{
		"response_type": {"code"}, "client_id": {"100497308"}, "redirect_uri": {qqMusicRedirect}, "scope": {"get_user_info,get_app_friends"}, "state": {state},
		"switch": {""}, "from_ptlogin": {"1"}, "src": {"1"}, "update_auth": {"1"}, "openapi": {"1010_1030"},
		"g_tk": {strconv.FormatUint(uint64(qqHash33(cookies["p_skey"], 5381)), 10)}, "auth_time": {strconv.FormatInt(time.Now().UnixMilli(), 10)}, "ui": {ui},
	}, nil, cookies)
	if err != nil {
		return Session{}, err
	}
	code, err := qqAuthorizationCode(r.Header.Get("Location"), state)
	if err != nil {
		c.rememberQQAuthorization(state, r.Header.Get("Location"), cookies)
		status := r.StatusCode
		var f *Failure
		if errors.As(err, &f) {
			status = f.Code
		}
		return Session{}, &Failure{Stage: "qq.graph.authorization", Code: status, Cause: err, Detail: fmt.Sprintf("%s login=%t identity=%t ticket=%t", redirectDiagnostic(r.Header.Get("Location"), state), cookies["skey"] != "", cookies["uin"] != "" || cookies["p_uin"] != "", cookies["p_skey"] != "")}
	}
	return c.qqExchange(ctx, code)
}
func (c *Client) qqExchange(ctx context.Context, code string) (Session, error) {
	d, err := c.qqCall(ctx, "QQConnectLogin.LoginServer", "QQLogin", map[string]any{"code": code}, nil)
	if err != nil {
		return Session{}, err
	}
	id, key := text(d["musicid"]), text(d["musickey"])
	s := Session{ID: id, Cookies: map[string]string{"uin": id, "qqmusic_uin": id, "qqmusic_key": key, "qm_keyst": key}}
	if err = c.validate(s); err != nil {
		return Session{}, err
	}
	return s, nil
}
func (c *Client) qqCall(ctx context.Context, module, method string, param map[string]any, s *Session) (map[string]any, error) {
	comm := map[string]any{"ct": 24, "cv": 0, "format": "json", "tmeLoginType": 2}
	var cookies map[string]string
	if s != nil {
		if err := c.validate(*s); err != nil {
			return nil, err
		}
		cookies = s.Cookies
		comm["uin"], comm["qq"], comm["authst"], comm["g_tk"] = s.ID, s.ID, cookies["qqmusic_key"], qqHash33(cookies["qqmusic_key"], 5381)
	}
	if param == nil {
		param = map[string]any{}
	}
	_, raw, err := c.request(ctx, "u.y.qq.com", "/cgi-bin/musicu.fcg", nil, nil, map[string]any{"comm": comm, "req_0": map[string]any{"module": module, "method": method, "param": param}}, cookies)
	if err != nil {
		return nil, err
	}
	d, err := decode(raw)
	if err != nil {
		return nil, err
	}
	for _, envelope := range []map[string]any{d, object(d["req_0"])} {
		if envelope["code"] == nil {
			return nil, failure("qq.rpc."+method, -2, ErrResponse)
		}
		if code := integer(envelope["code"]); code != 0 {
			if code == 1000 || code == 104401 || code == 104400 {
				return nil, failure("qq.rpc."+method, code, ErrExpired)
			}
			return nil, failure("qq.rpc."+method, code, errors.New("QQ 音乐暂不允许此操作"))
		}
	}
	out := object(object(d["req_0"])["data"])
	if out == nil {
		return nil, ErrResponse
	}
	return out, nil
}
func (c *Client) qqVerify(ctx context.Context, s Session) (Profile, error) {
	d, err := c.qqCall(ctx, "music.UserInfo.userInfoServer", "GetLoginUserInfo", nil, &s)
	if err != nil {
		return Profile{}, err
	}
	objects := []map[string]any{d}
	for _, k := range []string{"userInfo", "user_info", "info", "baseInfo", "base_info"} {
		if row := object(d[k]); row != nil {
			objects = append(objects, row)
		}
	}
	id, name := "", ""
	for _, row := range objects {
		for _, k := range []string{"musicid", "musicId", "musicUin", "uin"} {
			if v := text(row[k]); v != "" {
				if !numericID.MatchString(v) || id != "" && id != v {
					return Profile{}, ErrResponse
				}
				id = v
			}
		}
		for _, k := range []string{"nick", "nickname", "nickName", "name"} {
			if name == "" {
				name = text(row[k])
			}
		}
	}
	if id != s.ID {
		return Profile{}, failure("qq.identity", 0, errors.New("QQ 音乐当前账号尚未核实"))
	}
	if name == "" {
		name = "QQ 音乐账号"
	}
	return Profile{ID: id, Name: name}, nil
}
