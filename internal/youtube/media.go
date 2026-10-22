package youtube

import (
	"context"
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

func validMediaURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && u.Fragment == "" && (u.Port() == "" || u.Port() == "443") && strings.HasSuffix(strings.ToLower(u.Hostname()), ".googlevideo.com")
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
		if err != nil || port != "443" || !strings.HasSuffix(strings.ToLower(host), ".googlevideo.com") {
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

// ServeHTTP is always behind the app's existing login gateway. Browsers see
// only an opaque same-origin URL, never a cookie or Google's signed address.
func (p *Provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	token := r.PathValue("token")
	p.mu.Lock()
	id, ok := p.tokens[token]
	e := p.entries[id]
	p.mu.Unlock()
	if !ok || e.token != token || time.Now().After(e.until) {
		http.Error(w, "audio address expired", http.StatusNotFound)
		return
	}
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" && !singleRange.MatchString(rangeHeader) {
		http.Error(w, "one byte range required", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	upstream, err := http.NewRequestWithContext(ctx, r.Method, e.url, nil)
	if err != nil || !validMediaURL(e.url) {
		http.Error(w, "audio unavailable", http.StatusBadGateway)
		return
	}
	if e.userAgent != "" {
		upstream.Header.Set("User-Agent", e.userAgent)
	}
	if e.referer != "" {
		upstream.Header.Set("Referer", e.referer)
	}
	if rangeHeader != "" {
		upstream.Header.Set("Range", rangeHeader)
	}
	if v := r.Header.Get("If-Range"); v != "" {
		upstream.Header.Set("If-Range", v)
	}
	response, err := p.http.Do(upstream)
	if err != nil {
		http.Error(w, "audio upstream unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusGone {
		p.Forget(id)
	}
	if response.StatusCode != 200 && response.StatusCode != 206 && response.StatusCode != 416 || response.ContentLength > 128<<20 {
		http.Error(w, "audio upstream unavailable", http.StatusBadGateway)
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
		_, _ = io.Copy(w, io.LimitReader(response.Body, 128<<20))
	}
}
