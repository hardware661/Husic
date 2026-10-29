// Package platform owns the Bilibili account session, favourites and media.
// Candidates remain separate from the active session until the account
// manager commits them.
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

	"github.com/hardware661/husic/internal/source"
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

// Client is the Bilibili adapter: anonymous search and playback, plus the
// account's favourites once one is connected.
type Client struct {
	direct       *http.Client
	mediaHTTP    *http.Client
	mu           sync.RWMutex
	session      Session
	media        map[string]mediaEntry
	wbiKey       string
	wbiUntil     time.Time
	guestCookies map[string]string
	guestUntil   time.Time
}

func New(proxyURL string) (*Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if proxyURL != "" {
		proxy, err := url.Parse(proxyURL)
		if err != nil || proxy.Host == "" {
			return nil, errors.New("invalid music proxy")
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	noRedirect := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	direct := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: noRedirect}
	return &Client{direct: direct, mediaHTTP: mediaClient(), media: map[string]mediaEntry{}}, nil
}

func (c *Client) Name() string     { return "bilibili" }
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
	if !numericID.MatchString(s.ID) || len(s.Cookies) > 12 {
		return ErrExpired
	}
	for k, v := range s.Cookies {
		if !validCookie(v) {
			return ErrExpired
		}
		if k != "SESSDATA" && k != "bili_jct" && k != "DedeUserID" && k != "DedeUserID__ckMd5" && k != "buvid3" && k != "buvid4" {
			return ErrExpired
		}
	}
	if s.Cookies["SESSDATA"] == "" || s.Cookies["DedeUserID"] != s.ID {
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
func (c *Client) Start(ctx context.Context) (Login, error) { return c.biliStart(ctx) }
func (c *Client) Poll(ctx context.Context, key string) (LoginResult, error) {
	return c.biliPoll(ctx, key)
}
func (c *Client) Verify(ctx context.Context, s Session) (Profile, error) {
	if err := c.validate(s); err != nil {
		return Profile{}, err
	}
	return c.biliVerify(ctx, s)
}
