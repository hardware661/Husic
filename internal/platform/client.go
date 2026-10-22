// Package platform owns QQ Music and Bilibili account sessions. Candidates
// remain separate from the active session until the account manager commits.
package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"music/internal/source"
)

var (
	ErrNotConnected = errors.New("请先连接账号")
	ErrExpired      = errors.New("账号需要重新登录")
	ErrUnavailable  = errors.New("平台暂时无法连接，请稍后重试")
	ErrResponse     = errors.New("平台返回的数据不完整，请稍后重试")
	numericID       = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
)

type Profile struct{ ID, Name string }
type Session struct {
	ID      string            `json:"id"`
	Name    string            `json:"name,omitempty"`
	Cookies map[string]string `json:"cookies"`
}
type Login struct {
	Key   string
	Image []byte
}
type LoginResult struct {
	State   string
	Session *Session
}
type Collection struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Cover string `json:"cover"`
	Count int    `json:"count"`
}
type CollectionPage struct {
	Collection  Collection
	Tracks      []source.Track
	Total, Next int
}

type Client struct {
	kind          string
	direct, music *http.Client
	mediaHTTP     *http.Client
	publicQQ      source.QQ
	publicKugou   source.Kugou
	publicMigu    source.Migu
	mu            sync.RWMutex
	session       Session
	media         map[string]mediaEntry
	wbiKey        string
	wbiUntil      time.Time
	guestCookies  map[string]string
	guestUntil    time.Time
	pendingDir    string
}

func New(kind, proxyURL string) (*Client, error) {
	if kind != "qq" && kind != "bilibili" && kind != "kugou" && kind != "migu" {
		return nil, errors.New("unknown music platform")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if proxyURL != "" {
		proxy, err := url.Parse(proxyURL)
		if err != nil || proxy.Host == "" {
			return nil, errors.New("invalid music proxy")
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	musicTransport := transport.Clone()
	noRedirect := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	direct := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: noRedirect}
	music := &http.Client{Transport: musicTransport, Timeout: 15 * time.Second, CheckRedirect: noRedirect}
	public, err := source.NewHTTP(proxyURL, 12*time.Second)
	if err != nil {
		return nil, err
	}
	return &Client{kind: kind, direct: direct, music: music, mediaHTTP: mediaClient(), publicQQ: source.QQ{HTTP: public}, publicKugou: source.Kugou{HTTP: public}, publicMigu: source.Migu{HTTP: public, RecommendHTTP: public}, media: map[string]mediaEntry{}}, nil
}

func (c *Client) Name() string     { return c.kind }
func (c *Client) Identity() string { c.mu.RLock(); defer c.mu.RUnlock(); return c.session.ID }
func (c *Client) sessionCopy() Session {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s := c.session
	s.Cookies = cloneCookies(s.Cookies)
	return s
}
func cloneCookies(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func validCookie(s string) bool {
	if s == "" || len(s) > 8192 {
		return false
	}
	for _, r := range s {
		if r < 0x21 || r > 0x7e || strings.ContainsRune("\";\\,", r) {
			return false
		}
	}
	return true
}
func (c *Client) validate(s Session) error {
	if c.kind == "kugou" {
		return kugouValidate(s)
	}
	if c.kind == "migu" {
		return MiguValidateSession(s)
	}
	if !numericID.MatchString(s.ID) || len(s.Cookies) > 12 {
		return ErrExpired
	}
	for k, v := range s.Cookies {
		if !validCookie(v) {
			return ErrExpired
		}
		if c.kind == "qq" {
			if k != "uin" && k != "qqmusic_uin" && k != "qqmusic_key" && k != "qm_keyst" {
				return ErrExpired
			}
		} else if k != "SESSDATA" && k != "bili_jct" && k != "DedeUserID" && k != "DedeUserID__ckMd5" && k != "buvid3" && k != "buvid4" {
			return ErrExpired
		}
	}
	if c.kind == "qq" && (s.Cookies["uin"] != s.ID || s.Cookies["qqmusic_uin"] != s.ID || s.Cookies["qqmusic_key"] == "" || s.Cookies["qm_keyst"] != s.Cookies["qqmusic_key"]) {
		return ErrExpired
	}
	if c.kind == "bilibili" && (s.Cookies["SESSDATA"] == "" || s.Cookies["DedeUserID"] != s.ID) {
		return ErrExpired
	}
	return nil
}
func (c *Client) UseSession(s Session) error {
	if err := c.validate(s); err != nil {
		return err
	}
	s.Cookies = cloneCookies(s.Cookies)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.session = s
	return nil
}
func (c *Client) Restore(body []byte) error {
	var s Session
	if json.Unmarshal(body, &s) != nil {
		return ErrResponse
	}
	return c.UseSession(s)
}
func (c *Client) Account(ctx context.Context) (Profile, error) {
	s := c.sessionCopy()
	if s.ID == "" {
		return Profile{}, ErrNotConnected
	}
	return c.Verify(ctx, s)
}
func (c *Client) Start(ctx context.Context) (Login, error) {
	if c.kind == "kugou" {
		return c.kugouStart(ctx)
	}
	if c.kind == "migu" {
		return Login{}, errors.New("请使用手机验证码连接咪咕")
	}
	if c.kind == "qq" {
		return c.qqStart(ctx)
	}
	return c.biliStart(ctx)
}
func (c *Client) Poll(ctx context.Context, key string) (LoginResult, error) {
	if c.kind == "kugou" {
		return c.kugouPoll(ctx, key)
	}
	if c.kind == "migu" {
		return LoginResult{}, ErrResponse
	}
	if c.kind == "qq" {
		return c.qqPoll(ctx, key)
	}
	return c.biliPoll(ctx, key)
}
func (c *Client) Verify(ctx context.Context, s Session) (Profile, error) {
	if err := c.validate(s); err != nil {
		return Profile{}, err
	}
	if c.kind == "kugou" {
		return c.kugouVerify(ctx, s)
	}
	if c.kind == "migu" {
		return c.miguVerify(ctx, s)
	}
	if c.kind == "qq" {
		return c.qqVerify(ctx, s)
	}
	return c.biliVerify(ctx, s)
}
