package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hardware661/husic/internal/resolve"
)

const maxDownloadBytes int64 = 512 << 20

var errDownloadLimit = errors.New("audio exceeds download limit")

// Download media never inherits HTTP_PROXY or the domestic API proxy. The
// resolved address is validated both before HTTP and at the actual DNS dial.
var downloadTransport = newDownloadTransport(net.DefaultResolver.LookupNetIP, (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext)

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	s.downloadUsing(w, r, downloadTransport)
}

func (s *Server) downloadUsing(w http.ResponseWriter, r *http.Request, transport http.RoundTripper) {
	if !downloadSameOrigin(r) {
		fail(w, http.StatusForbidden, "请从 Husic 中下载")
		return
	}
	var req struct {
		Item Item `json:"item"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields() // No client-supplied media URL or local file.
	if decoder.Decode(&req) != nil || decoder.Decode(new(any)) != io.EOF || !downloadItemValid(req.Item) {
		fail(w, http.StatusBadRequest, "歌曲信息无效，本地文件无需下载")
		return
	}
	if s.Resolver == nil {
		fail(w, http.StatusServiceUnavailable, "下载暂不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	item := req.Item
	if item.SongID != 0 && s.Store != nil {
		if song, err := s.Store.Song(ctx, item.SongID); err == nil {
			item.Copies = append(song.Sources, item.Copies...)
		}
	}
	track, stream, err := s.Resolver.Resolve(ctx, item.SongID, item.want(), item.Copies)
	if err != nil {
		if ctx.Err() != nil {
			fail(w, http.StatusRequestTimeout, "下载已取消或超时")
			return
		}
		status := http.StatusBadGateway
		if resolve.IsUnavailable(err) {
			status = http.StatusNotFound
		}
		fail(w, status, "暂时没有可下载的完整音频")
		return
	}
	file, err := os.CreateTemp("", "husic-download-*")
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "下载暂不可用")
		return
	}
	defer os.Remove(file.Name())
	defer file.Close()
	capture := &downloadCapture{ctx: ctx, file: file, limit: maxDownloadBytes, header: make(http.Header)}
	if strings.HasPrefix(stream.URL, "/") {
		err = s.captureTokenAudio(ctx, track.Source, stream.URL, capture)
	} else {
		err = captureRemoteAudio(ctx, track.Source, stream.URL, capture, transport)
	}
	if err == nil {
		err = capture.complete()
	}
	if err != nil {
		if ctx.Err() != nil {
			fail(w, http.StatusRequestTimeout, "下载已取消或超时")
			return
		}
		if errors.Is(err, errDownloadLimit) {
			fail(w, http.StatusRequestEntityTooLarge, "音频超过 512 MB")
			return
		}
		fail(w, http.StatusBadGateway, "音频下载失败，请稍后重试")
		return
	}
	probe := make([]byte, min(capture.size, 4096))
	if _, err = file.ReadAt(probe, 0); err != nil && err != io.EOF {
		fail(w, http.StatusBadGateway, "音频读取失败")
		return
	}
	format, contentType := downloadAudioType(probe)
	if format == "" {
		fail(w, http.StatusBadGateway, "返回的内容不是支持的音频")
		return
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		fail(w, http.StatusBadGateway, "音频读取失败")
		return
	}
	name := strings.TrimSpace(strings.Join(item.Artists, ", ") + " - " + item.Title)
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	if len([]rune(name)) > 160 {
		name = string([]rune(name)[:160])
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(capture.size, 10))
	w.Header().Set("X-Husic-Audio-Format", format)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + "." + format}))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, file)
}

func downloadSameOrigin(r *http.Request) bool {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		if err != nil || u.Scheme != scheme || u.Host != r.Host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return false
		}
	}
	return true
}

func downloadItemValid(item Item) bool {
	if item.SongID < 0 || len(item.Copies) > 32 || len(item.Title) > 1000 || len(item.Artists) > 32 {
		return false
	}
	if item.SongID == 0 && len(item.Copies) == 0 {
		return false
	}
	for _, track := range item.Copies {
		switch track.Source {
		case "netease", "qq", "kuwo", "kugou", "migu", "bilibili", "youtube":
		default:
			return false
		}
		if track.ID == "" || len(track.ID) > 512 {
			return false
		}
	}
	return true
}

// CDN families are tied to the server's platform adapters, not supplied by
// the request. Bilibili/YouTube use their existing opaque-token handlers.
func downloadHost(kind, host string) bool {
	host = strings.ToLower(host)
	suffix := func(domain string) bool { return strings.HasSuffix(host, "."+domain) }
	switch kind {
	case "netease":
		return suffix("music.126.net") || suffix("music.163.com")
	case "qq":
		return suffix("qqmusic.qq.com")
	case "kuwo":
		return suffix("kuwo.cn")
	case "kugou":
		return suffix("kugou.com")
	case "migu":
		return suffix("migu.cn")
	}
	return false
}

func downloadURL(kind string, u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.User == nil && u.Fragment == "" && (u.Port() == "" || u.Port() == "443") && downloadHost(kind, u.Hostname())
}

var downloadNonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("2002::/16"),
}

func downloadPublicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range downloadNonPublic {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

type downloadLookup func(context.Context, string, string) ([]netip.Addr, error)
type downloadDial func(context.Context, string, string) (net.Conn, error)

func newDownloadTransport(lookup downloadLookup, dial downloadDial) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = 12 * time.Second
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		allowed := false
		for _, kind := range []string{"netease", "qq", "kuwo", "kugou", "migu"} {
			allowed = allowed || downloadHost(kind, host)
		}
		if err != nil || port != "443" || !allowed {
			return nil, errors.New("invalid audio destination")
		}
		ips, err := lookup(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("audio DNS lookup failed")
		}
		for _, ip := range ips {
			if !downloadPublicAddress(ip) {
				return nil, errors.New("invalid audio destination")
			}
		}
		for _, ip := range ips {
			var conn net.Conn
			conn, err = dial(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
		}
		return nil, err
	}
	return transport
}

func captureRemoteAudio(ctx context.Context, kind, raw string, capture *downloadCapture, transport http.RoundTripper) error {
	u, err := url.Parse(raw)
	if err != nil || !downloadURL(kind, u) {
		return errors.New("invalid audio address")
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || !downloadURL(kind, req.URL) {
			return errors.New("invalid audio redirect")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept-Encoding", "identity")
	referer := map[string]string{"netease": "https://music.163.com/", "qq": "https://y.qq.com/", "kuwo": "https://www.kuwo.cn/", "kugou": "https://www.kugou.com/", "migu": "https://music.migu.cn/"}
	req.Header.Set("Referer", referer[kind])
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	capture.header = resp.Header.Clone()
	capture.status = resp.StatusCode
	if resp.ContentLength >= 0 {
		capture.header.Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	if err = capture.checkHeaders(); err != nil {
		return err
	}
	_, err = io.Copy(capture, resp.Body)
	return err
}

func (s *Server) captureTokenAudio(ctx context.Context, kind, raw string, capture *downloadCapture) error {
	prefix := "/api/media/"
	var handler http.Handler
	switch kind {
	case "youtube":
		handler = s.YouTube
	case "bilibili":
		prefix, handler = "/api/bilibili/media/", s.Platforms["bilibili"]
	}
	if handler == nil || !strings.HasPrefix(raw, prefix) {
		return errors.New("invalid audio token")
	}
	token := strings.TrimPrefix(raw, prefix)
	if len(token) < 16 || len(token) > 128 || strings.ContainsAny(token, "/?%#\\") {
		return errors.New("invalid audio token")
	}
	for _, c := range token {
		if c != '-' && c != '_' && !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') {
			return errors.New("invalid audio token")
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	req.SetPathValue("token", token)
	handler.ServeHTTP(capture, req)
	// Existing media handlers cap their streams. Without an exact length a
	// silently capped/truncated response cannot safely become a saved file.
	if capture.header.Get("Content-Length") == "" {
		return errors.New("audio length unavailable")
	}
	return capture.err
}

// A bounded temporary file makes failures atomic: no successful HTTP audio
// response is sent until the whole download has passed size/type validation.
type downloadCapture struct {
	ctx         context.Context
	file        *os.File
	limit, size int64
	header      http.Header
	status      int
	err         error
}

func (c *downloadCapture) Header() http.Header { return c.header }
func (c *downloadCapture) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}
func (c *downloadCapture) checkHeaders() error {
	if c.status != http.StatusOK {
		return errors.New("audio upstream rejected download")
	}
	if value := c.header.Get("Content-Length"); value != "" {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n <= 0 {
			return errors.New("invalid audio length")
		}
		if n > c.limit {
			return errDownloadLimit
		}
	}
	kind, _, _ := mime.ParseMediaType(c.header.Get("Content-Type"))
	if kind != "" && kind != "application/octet-stream" && kind != "binary/octet-stream" && kind != "application/ogg" && !strings.HasPrefix(kind, "audio/") && kind != "video/mp4" {
		return errors.New("not audio")
	}
	return nil
}
func (c *downloadCapture) Write(data []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	if c.err == nil {
		c.err = c.ctx.Err()
	}
	if c.err == nil {
		c.err = c.checkHeaders()
	}
	if c.err == nil && int64(len(data)) > c.limit-c.size {
		c.err = errDownloadLimit
	}
	if c.err != nil {
		return 0, c.err
	}
	n, err := c.file.Write(data)
	c.size += int64(n)
	c.err = err
	return n, err
}
func (c *downloadCapture) complete() error {
	if c.err != nil {
		return c.err
	}
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if err := c.checkHeaders(); err != nil {
		return err
	}
	if c.size == 0 {
		return errors.New("empty audio")
	}
	if value := c.header.Get("Content-Length"); value != "" {
		n, _ := strconv.ParseInt(value, 10, 64)
		if n != c.size {
			return errors.New("incomplete audio")
		}
	}
	return nil
}

func downloadAudioType(data []byte) (string, string) {
	switch {
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WAVE")):
		return "wav", "audio/wav"
	case bytes.HasPrefix(data, []byte("fLaC")):
		return "flac", "audio/flac"
	case len(data) >= 12 && bytes.Equal(data[4:8], []byte("ftyp")):
		return "m4a", "audio/mp4"
	case bytes.HasPrefix(data, []byte("OggS")) && (bytes.Contains(data, []byte("OpusHead")) || bytes.Contains(data, []byte("vorbis"))):
		return "ogg", "audio/ogg"
	case len(data) >= 10 && bytes.Equal(data[:3], []byte("ID3")):
		return "mp3", "audio/mpeg"
	case len(data) >= 7 && data[0] == 0xff && data[1]&0xf6 == 0xf0:
		return "aac", "audio/aac"
	case len(data) >= 4 && data[0] == 0xff && data[1]&0xe0 == 0xe0 && data[1]&0x18 != 0x08 && data[1]&6 != 0 && data[2]&0xf0 != 0xf0 && data[2]&0x0c != 0x0c:
		return "mp3", "audio/mpeg"
	}
	return "", ""
}
