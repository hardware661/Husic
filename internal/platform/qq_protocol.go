// QQ protocol validation adapted from the prior Music account adapter.
// Callbacks remain data; no returned URL or script is executed.
package platform

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Keep cookies scoped to this one login, applying the browser's domain/path
// rules. Never share this jar with another attempt or a music/CDN request.
func qqKeepLoginCookies(jar *cookiejar.Jar, host string, response *http.Response) {
	u := &url.URL{Scheme: "https", Host: host, Path: "/"}
	for _, cookie := range response.Cookies() {
		if qqCookieNameAllowed("authorize", cookie.Name) && validCookie(cookie.Value) {
			jar.SetCookies(u, []*http.Cookie{cookie})
		}
	}
}
func qqCookiesAt(jar *cookiejar.Jar, host string) map[string]string {
	out := map[string]string{}
	for _, cookie := range jar.Cookies(&url.URL{Scheme: "https", Host: host, Path: "/"}) {
		out[cookie.Name] = cookie.Value
	}
	return out
}

func qqCookieNameAllowed(operation, name string) bool {
	switch operation {
	case "poll":
		return name == "qrsig"
	case "authorize":
		switch name {
		case "p_skey", "p_uin", "pt4_token", "uin", "skey", "pt2gguin":
			return true
		}
	case "music":
		switch name {
		case "uin", "qqmusic_uin", "qqmusic_key", "qm_keyst":
			return true
		}
	}
	return false
}

func qqHash33(value string, seed uint32) uint32 {
	hash := seed
	for _, b := range []byte(value) {
		hash = hash*33 + uint32(b)
	}
	return hash & 0x7fffffff
}

// Validates and normalizes the actual QR image before it reaches the client.

func qqReadQRSignature(response *http.Response, now time.Time) (string, error) {
	value := ""
	for _, cookie := range response.Cookies() {
		if cookie.Name != "qrsig" || cookie.MaxAge < 0 || !cookie.Expires.IsZero() && !now.Before(cookie.Expires) {
			continue
		}
		domainName := strings.TrimPrefix(strings.ToLower(cookie.Domain), ".")
		if domainName != "" && domainName != "ptlogin2.qq.com" && domainName != "ssl.ptlogin2.qq.com" && domainName != "qq.com" {
			continue
		}
		if cookie.Path != "" && cookie.Path != "/" || !validCookie(cookie.Value) || value != "" && value != cookie.Value {
			return "", errors.New("QQ 返回的二维码会话无效")
		}
		value = cookie.Value
	}
	if value == "" {
		return "", errors.New("QQ 未返回完整的二维码会话")
	}
	return value, nil
}

// Read bounded JavaScript string arguments as data, including UTF-16 pairs
// and escaped slashes. Neither JavaScript nor callback suffixes are executed.
func qqParseCallback(raw []byte) ([]string, error) {
	input := strings.TrimSpace(string(raw))
	if len(input) > 65536 || !strings.HasPrefix(input, "ptuiCB(") {
		return nil, errors.New("QQ 扫码响应格式无法识别")
	}
	input = strings.TrimSpace(strings.TrimPrefix(input, "ptuiCB("))
	args := make([]string, 0, 6)
	for len(args) < 10 {
		if len(input) == 0 || input[0] != '\'' {
			return nil, errors.New("QQ 扫码响应参数不完整")
		}
		input = input[1:]
		var encoded strings.Builder
		encoded.WriteByte('"')
		closed := false
		for len(input) > 0 {
			b := input[0]
			input = input[1:]
			if b == '\'' {
				closed = true
				break
			}
			if b < 0x20 || encoded.Len() > 32768 {
				return nil, errors.New("QQ 扫码响应包含无效参数")
			}
			if b == '"' {
				encoded.WriteString(`\"`)
				continue
			}
			if b != '\\' {
				encoded.WriteByte(b)
				continue
			}
			if len(input) == 0 {
				return nil, ErrResponse
			}
			next := input[0]
			input = input[1:]
			switch next {
			case '\'':
				encoded.WriteByte('\'')
			case 'x':
				if len(input) < 2 {
					return nil, ErrResponse
				}
				encoded.WriteString(`\u00`)
				encoded.WriteString(input[:2])
				input = input[2:]
			case 'v':
				encoded.WriteString(`\u000b`)
			default:
				encoded.WriteByte('\\')
				encoded.WriteByte(next)
			}
		}
		if !closed {
			return nil, errors.New("QQ 扫码响应参数未结束")
		}
		encoded.WriteByte('"')
		var value string
		if json.Unmarshal([]byte(encoded.String()), &value) != nil {
			return nil, errors.New("QQ 扫码响应包含无效参数")
		}
		args = append(args, value)
		input = strings.TrimSpace(input)
		if input == ")" || input == ");" {
			if len(args) < 3 {
				return nil, ErrResponse
			}
			return args, nil
		}
		if !strings.HasPrefix(input, ",") {
			return nil, errors.New("QQ 扫码响应包含多余内容")
		}
		input = strings.TrimSpace(input[1:])
	}
	return nil, errors.New("QQ 扫码响应参数过多")
}

func qqSuccessIdentity(value string) (string, string, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host != "ssl.ptlogin2.graph.qq.com" || u.User != nil || u.Path != "/check_sig" || u.Fragment != "" {
		return "", "", errors.New("QQ 登录确认地址不符合固定流程")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["uin"]) != 1 || len(q["ptsigx"]) != 1 || !numericID.MatchString(q.Get("uin")) || !validCookie(q.Get("ptsigx")) {
		return "", "", errors.New("QQ 登录确认缺少账号与签名")
	}
	return q.Get("uin"), q.Get("ptsigx"), nil
}

func qqGraphCookies(response *http.Response, now time.Time) (map[string]string, error) {
	cookies := map[string]string{}
	for _, cookie := range response.Cookies() {
		if !qqCookieNameAllowed("authorize", cookie.Name) || !validCookie(cookie.Value) || cookie.MaxAge < 0 || !cookie.Expires.IsZero() && !now.Before(cookie.Expires) {
			continue
		}
		domainName := strings.TrimPrefix(strings.ToLower(cookie.Domain), ".")
		// A host-only cookie from ssl.ptlogin2.graph.qq.com cannot be widened
		// to graph.qq.com. Only a domain explicitly valid for both is usable.
		if domainName != "graph.qq.com" && domainName != "qq.com" || cookie.Path != "" && cookie.Path != "/" {
			continue
		}
		if prior := cookies[cookie.Name]; prior != "" && prior != cookie.Value {
			return nil, errors.New("QQ 授权会话存在冲突")
		}
		cookies[cookie.Name] = cookie.Value
	}
	if cookies["p_skey"] == "" {
		return nil, errors.New("QQ 未返回可用于音乐授权的会话")
	}
	return cookies, nil
}

func qqAuthorizationCode(location, state string) (string, error) {
	u, err := url.Parse(location)
	if err == nil && u.Scheme == "https" && u.Host == "graph.qq.com" && u.Path == "/oauth2.0/show" && u.User == nil && u.Fragment == "" {
		q, parseErr := url.ParseQuery(u.RawQuery)
		if parseErr == nil && len(q["state"]) == 1 && q.Get("state") == state && state != "" && len(q["error"]) == 1 {
			if code, e := strconv.Atoi(q.Get("error")); e == nil && code >= 100000 && code <= 199999 {
				message := "QQ 未接受这次授权，原连接没有改变"
				if code == 100035 {
					message = "QQ 已确认，但授权会话没有被平台接受，原连接没有改变"
				}
				return "", failure("qq.graph.authorization", code, errors.New(message))
			}
		}
	}
	// This is data returned over the pinned HTTPS Graph request, never a
	// destination we navigate or fetch. Accept equivalent official callback
	// spellings while retaining the one-time state and exact callback host.
	if err == nil && u.Scheme == "" && u.Host != "" {
		u.Scheme = "https"
	}
	if err != nil || u.Scheme != "https" && u.Scheme != "http" || strings.ToLower(u.Hostname()) != "y.qq.com" || u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80") || u.User != nil || u.Path != "/portal/wx_redirect.html" || u.Fragment != "" {
		return "", errors.New("QQ 音乐授权回调不符合固定流程")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["state"]) != 1 || q.Get("state") != state || state == "" || len(q["code"]) != 1 || !validCookie(q.Get("code")) {
		return "", errors.New("QQ 音乐授权未通过一次性状态校验")
	}
	return q.Get("code"), nil
}
