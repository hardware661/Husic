package platform

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type mediaEntry struct {
	url   string
	until time.Time
}

func validMediaHost(host string) bool {
	host = strings.ToLower(host)
	for _, suffix := range []string{".bilivideo.com", ".bilivideo.cn", ".acgvideo.com"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}
func validMediaURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && u.Fragment == "" && (u.Port() == "" || u.Port() == "443") && validMediaHost(u.Hostname())
}

var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001:db8::/32"),
}

func publicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range nonPublic {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func mediaClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = 10 * time.Second
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" || !validMediaHost(host) {
			return nil, errors.New("invalid media destination")
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("media DNS lookup failed")
		}
		for _, ip := range ips {
			if !publicAddress(ip) {
				return nil, errors.New("invalid media destination")
			}
		}
		var last error
		for _, ip := range ips {
			conn, err := (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
	return &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || !validMediaURL(req.URL.String()) {
			return errors.New("invalid media redirect")
		}
		return nil
	}}
}

var singleRange = regexp.MustCompile(`^bytes=(?:[0-9]+-[0-9]*|-[0-9]+)$`)

func (c *Client) registerMedia(raw string) (string, error) {
	if !validMediaURL(raw) {
		return "", ErrResponse
	}
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return "", ErrUnavailable
	}
	token := base64.RawURLEncoding.EncodeToString(nonce)
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, e := range c.media {
		if time.Now().After(e.until) {
			delete(c.media, key)
		}
	}
	if len(c.media) > 1024 {
		return "", ErrUnavailable
	}
	c.media[token] = mediaEntry{url: raw, until: time.Now().Add(10 * time.Minute)}
	return "/api/bilibili/media/" + token, nil
}
func (c *Client) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(405)
		return
	}
	c.mu.RLock()
	e, ok := c.media[r.PathValue("token")]
	c.mu.RUnlock()
	if !ok || time.Now().After(e.until) {
		http.Error(w, "音频地址已过期，请重新播放", 404)
		return
	}
	if value := r.Header.Get("Range"); value != "" && !singleRange.MatchString(value) {
		w.WriteHeader(416)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, r.Method, e.url, nil)
	if err != nil || !validMediaURL(e.url) {
		http.Error(w, "音频暂不可用", 502)
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://www.bilibili.com/")
	for _, key := range []string{"Range", "If-Range"} {
		if value := r.Header.Get(key); value != "" {
			req.Header.Set(key, value)
		}
	}
	response, err := c.mediaHTTP.Do(req)
	if err != nil {
		http.Error(w, "音频暂时无法连接", 502)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 && response.StatusCode != 206 && response.StatusCode != 416 || response.ContentLength > 256<<20 || strings.Contains(response.Header.Get("Content-Type"), "text/html") || strings.Contains(response.Header.Get("Content-Type"), "json") {
		http.Error(w, "音频暂不可用", 502)
		return
	}
	for _, key := range []string{"Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if value := response.Header.Get(key); value != "" {
			w.Header().Set(key, value)
		}
	}
	w.Header().Set("Content-Type", "audio/mp4")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(response.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, io.LimitReader(response.Body, 256<<20))
	}
}
