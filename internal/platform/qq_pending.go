package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"music/internal/privatefile"
)

// Rejected redirects stay private and are never automatically installed.
type pendingQQ struct {
	State        string
	Location     string
	Expires      time.Time
	Cookies      map[string]string `json:",omitempty"`
	Confirmation string            `json:",omitempty"`
}

func (c *Client) SetPendingDirectory(dir string) { c.pendingDir = dir }
func (c *Client) rememberQQConfirmation(confirmation string, cookies map[string]string) {
	if c.pendingDir == "" {
		return
	}
	body, _ := json.Marshal(pendingQQ{Confirmation: confirmation, Cookies: cookies, Expires: time.Now().Add(5 * time.Minute)})
	_ = privatefile.Write(filepath.Join(c.pendingDir, "qq-authorization-pending.json"), body)
}
func (c *Client) rememberQQRedirect(state, location string) {
	c.rememberQQAuthorization(state, location, nil)
}
func (c *Client) rememberQQAuthorization(state, location string, cookies map[string]string) {
	if c.pendingDir == "" {
		return
	}
	body, _ := json.Marshal(pendingQQ{State: state, Location: location, Expires: time.Now().Add(5 * time.Minute), Cookies: cookies})
	_ = privatefile.Write(filepath.Join(c.pendingDir, "qq-authorization-pending.json"), body)
}
func (c *Client) ResumeQQ(ctx context.Context) (Session, error) {
	if c.pendingDir == "" {
		return Session{}, ErrNotConnected
	}
	body, err := os.ReadFile(filepath.Join(c.pendingDir, "qq-authorization-pending.json"))
	if err != nil {
		return Session{}, ErrNotConnected
	}
	var pending pendingQQ
	if len(body) > 65536 || json.Unmarshal(body, &pending) != nil || time.Now().After(pending.Expires) {
		return Session{}, errors.New("待验证授权已结束，需要重新连接")
	}
	if len(pending.Cookies) > 6 {
		return Session{}, ErrExpired
	}
	for name, value := range pending.Cookies {
		if !qqCookieNameAllowed("authorize", name) || !validCookie(value) {
			return Session{}, ErrExpired
		}
	}
	if pending.Confirmation != "" {
		jar, _ := cookiejar.New(nil)
		u := &url.URL{Scheme: "https", Host: "ssl.ptlogin2.qq.com", Path: "/"}
		for name, value := range pending.Cookies {
			jar.SetCookies(u, []*http.Cookie{{Name: name, Value: value, Domain: "qq.com", Path: "/", Secure: true}})
		}
		return c.qqAuthorize(ctx, pending.Confirmation, jar)
	}
	code, err := qqAuthorizationCode(pending.Location, pending.State)
	if err != nil {
		var upstream *Failure
		if errors.As(err, &upstream) && upstream.Code == 100035 && len(pending.Cookies) > 0 && len(pending.Cookies) <= 6 && pending.Cookies["p_skey"] != "" {
			for name, value := range pending.Cookies {
				if !qqCookieNameAllowed("authorize", name) || !validCookie(value) {
					return Session{}, ErrExpired
				}
			}
			return c.qqGraphAuthorize(ctx, pending.Cookies)
		}
		return Session{}, err
	}
	return c.qqExchange(ctx, code)
}
func redirectDiagnostic(raw, state string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "invalid_url"
	}
	host := "other"
	if u.Hostname() == "y.qq.com" || u.Hostname() == "graph.qq.com" {
		host = u.Hostname()
	}
	scheme := u.Scheme
	if scheme != "http" && scheme != "https" && scheme != "" {
		scheme = "other"
	}
	return fmt.Sprintf("scheme=%s host=%s callbackPath=%t code=%t statePresent=%t stateMatches=%t fragment=%t", scheme, host, u.Path == "/portal/wx_redirect.html", u.Query().Get("code") != "", u.Query().Get("state") != "", u.Query().Get("state") == state, strings.TrimSpace(u.Fragment) != "")
}
