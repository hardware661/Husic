package netease

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Session is a logged-in identity. MusicU is a long-lived secret: keep the
// file 0600 and never print it.
type Session struct {
	MusicU   string    `json:"music_u"`
	CSRF     string    `json:"csrf"`
	DeviceID string    `json:"device_id"`
	UserID   int64     `json:"user_id,omitempty"`
	Saved    time.Time `json:"saved"`
}

// Error is a NetEase business failure: HTTP was fine but "code" was not 200.
type Error struct {
	Path    string
	Code    int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("netease %s: code %d %s", e.Path, e.Code, e.Message)
}

// Result is the envelope every endpoint returns alongside its payload.
type Result struct {
	Code    int
	Message string
	Cookies []*http.Cookie
}

func (r Result) err(path string) error {
	if r.Code == 200 {
		return nil
	}
	return &Error{Path: path, Code: r.Code, Message: r.Message}
}

type Client struct {
	http     *http.Client
	mu       sync.RWMutex
	session  *Session
	deviceID string
}

const (
	webUA  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	appUA  = "NeteaseMusic 9.0.90/5038 (iPhone; iOS 16.2; zh_CN)"
	appVer = "9.0.90"
)

// New returns a client that reaches NetEase directly, or through proxyURL
// (http://, socks5:// or socks5h://) when it is set. session may be nil.
func New(proxyURL string, session *Session, timeout time.Duration) (*Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("proxy: %w", err)
		}
		transport.Proxy = http.ProxyURL(u)
	}
	deviceID := ""
	if session != nil {
		deviceID = session.DeviceID
	}
	if deviceID == "" {
		deviceID = NewDeviceID()
	}
	return &Client{http: &http.Client{Transport: transport, Timeout: timeout}, session: session, deviceID: deviceID}, nil
}

// NewDeviceID returns a random identifier in the shape the app sends.
func NewDeviceID() string {
	b := make([]byte, 26)
	_, _ = rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))
}

func (c *Client) csrf() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.session == nil {
		return ""
	}
	return c.session.CSRF
}

// UseSession changes only the account used for future requests. In-flight
// requests retain their snapshot; the shared HTTP transport stays in place.
func (c *Client) UseSession(session Session) {
	c.mu.Lock()
	defer c.mu.Unlock()
	copy := session
	c.session = &copy
	if session.DeviceID != "" {
		c.deviceID = session.DeviceID
	}
}

func (c *Client) HasSession() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.session != nil && c.session.MusicU != ""
}

func (c *Client) snapshot() (*Session, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.session == nil {
		return nil, c.deviceID
	}
	copy := *c.session
	return &copy, c.deviceID
}

func (c *Client) UserID() int64 {
	session, _ := c.snapshot()
	if session == nil {
		return 0
	}
	return session.UserID
}

func (c *Client) VerifySession(ctx context.Context, session Session) (Account, error) {
	_, device := c.snapshot()
	if session.DeviceID != "" {
		device = session.DeviceID
	}
	probe := &Client{http: c.http, session: &session, deviceID: device}
	return probe.Account(ctx)
}

func (c *Client) loginClient() *Client {
	_, device := c.snapshot()
	return &Client{http: c.http, deviceID: device}
}

// Weapi posts to the web endpoint, e.g. path "/login/qrcode/unikey".
func (c *Client) Weapi(ctx context.Context, path string, payload map[string]any, out any) (Result, error) {
	session, _ := c.snapshot()
	csrf := ""
	if session != nil {
		csrf = session.CSRF
	}
	form, err := weapiForm(payload, csrf)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://music.163.com/weapi"+path, strings.NewReader(form.Encode()))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "https://music.163.com/")
	req.Header.Set("Origin", "https://music.163.com")
	req.Header.Set("User-Agent", webUA)
	cookies := []string{"os=pc", "appver=3.0.18.203152"}
	if session != nil {
		cookies = append(cookies, "MUSIC_U="+session.MusicU, "__csrf="+session.CSRF)
	}
	req.Header.Set("Cookie", strings.Join(cookies, "; "))
	return c.do(req, path, out)
}

// Eapi posts to the app endpoint, e.g. path "/song/enhance/player/url/v1".
func (c *Client) Eapi(ctx context.Context, path string, payload map[string]any, out any) (Result, error) {
	session, device := c.snapshot()
	csrf := ""
	if session != nil {
		csrf = session.CSRF
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(1000))
	header := map[string]string{
		"osver": "16.2", "deviceId": device, "os": "iOS", "appver": appVer, "versioncode": "140",
		"mobilename": "", "buildver": strconv.FormatInt(time.Now().Unix(), 10), "resolution": "1920x1080",
		"__csrf": csrf, "channel": "distribution",
		"requestId": fmt.Sprintf("%d_%04d", time.Now().UnixMilli(), n.Int64()),
	}
	if session != nil {
		header["MUSIC_U"] = session.MusicU
	}
	body := make(map[string]any, len(payload)+1)
	for name, value := range payload {
		body[name] = value
	}
	body["header"] = header
	form, err := eapiForm("/api"+path, body)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://interface.music.163.com/eapi"+path, strings.NewReader(form.Encode()))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", appUA)
	cookies := make([]string, 0, len(header))
	for name, value := range header {
		cookies = append(cookies, url.QueryEscape(name)+"="+url.QueryEscape(value))
	}
	req.Header.Set("Cookie", strings.Join(cookies, "; "))
	return c.do(req, path, out)
}

func (c *Client) do(req *http.Request, path string, out any) (Result, error) {
	resp, err := c.http.Do(req)
	// NetEase's edge answers an occasional 502/503 under load; one calm retry
	// clears it. Bodies are strings.Reader, so GetBody can replay them.
	if err == nil && resp.StatusCode >= 502 && resp.StatusCode <= 504 && req.GetBody != nil {
		resp.Body.Close()
		time.Sleep(700 * time.Millisecond)
		if body, berr := req.GetBody(); berr == nil {
			req.Body = body
			resp, err = c.http.Do(req)
		}
	}
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, &Error{Path: path, Code: resp.StatusCode, Message: "HTTP status"}
	}
	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Result{}, fmt.Errorf("netease %s: unreadable response: %w", path, err)
	}
	result := Result{Code: envelope.Code, Message: envelope.Message, Cookies: resp.Cookies()}
	if result.Message == "" {
		result.Message = envelope.Msg
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return result, fmt.Errorf("netease %s: unexpected shape: %w", path, err)
		}
	}
	return result, nil
}
