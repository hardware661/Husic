// Package source puts every music platform behind one small interface:
// search for tracks, then ask for a full-length stream of one of them.
package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Track is a song as one platform knows it.
type Track struct {
	Source   string            `json:"source"`
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	Artists  []string          `json:"artists"`
	Album    string            `json:"album,omitempty"`
	Duration int               `json:"duration"` // seconds, 0 if unknown
	Cover    string            `json:"cover,omitempty"`
	Extra    map[string]string `json:"extra,omitempty"`
	Pinned   bool              `json:"pinned,omitempty"` // the listener chose this copy
}

// Ref is the "source:id" key of a track.
func (t Track) Ref() string { return t.Source + ":" + t.ID }

// Stream is a playable address for a Track.
type Stream struct {
	URL     string
	Bitrate int // bits per second, 0 if unknown
	Format  string
	Seconds int // length actually served, 0 if unknown
}

// ErrUnavailable means the platform has the track but will not give this
// caller full-length audio (paid, preview only, region, removed).
var ErrUnavailable = errors.New("no full-length audio for this caller")

type Provider interface {
	Name() string
	Search(ctx context.Context, query string, limit int) ([]Track, error)
	Stream(ctx context.Context, t Track) (Stream, error)
}

// HTTP is the transport the platform adapters share.
type HTTP struct {
	client *http.Client
}

// NewHTTP reaches platforms directly, or through proxyURL when set.
func NewHTTP(proxyURL string, timeout time.Duration) (*HTTP, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("proxy: %w", err)
		}
		transport.Proxy = http.ProxyURL(u)
	}
	return &HTTP{client: &http.Client{Transport: transport, Timeout: timeout}}, nil
}

func (h *HTTP) getJSON(ctx context.Context, rawURL string, query url.Values, headers map[string]string) (map[string]any, error) {
	if len(query) > 0 {
		rawURL += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	return h.doJSON(req, headers)
}

func (h *HTTP) postJSON(ctx context.Context, rawURL string, body any, headers map[string]string) (map[string]any, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return h.doJSON(req, headers)
}

func (h *HTTP) doJSON(req *http.Request, headers map[string]string) (map[string]any, error) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", req.URL.Host, resp.StatusCode)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("%s: not JSON", req.URL.Host)
	}
	return out, nil
}

// Loose accessors: these APIs mix numbers and numeric strings freely.

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func arr(v any) []any          { a, _ := v.([]any); return a }

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

func num(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	}
	return 0
}

func dig(v any, path ...string) any {
	for _, key := range path {
		v = obj(v)[key]
	}
	return v
}

var tags = regexp.MustCompile(`<[^>]*>`)

// clean drops highlight markup some search APIs wrap around matches.
func clean(s string) string { return strings.TrimSpace(tags.ReplaceAllString(s, "")) }

func names(list []any, key string) []string {
	var out []string
	for _, item := range list {
		if n := clean(str(obj(item)[key])); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func splitArtists(s string) []string {
	var out []string
	for _, part := range regexp.MustCompile(`\s*[、/&,，;]\s*`).Split(clean(s), -1) {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

var bracketed = regexp.MustCompile(`[(（\[【][^)）\]】]*[)）\]】]`)

// fold keeps only letters and digits, lower-cased, for loose comparison.
func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// versionWords mark a different recording of the same song.
var versionWords = []string{"live", "现场", "演唱会", "伴奏", "纯音乐", "instrumental", "karaoke", "ktv", "dj", "remix", "混音",
	"翻自", "cover", "翻唱", "加速", "降调", "升调", "慢速", "0.8x", "1.2x", "1.5x", "女声", "男声", "童声", "片段", "铃声",
	"demo", "acoustic", "不插电", "钢琴版", "吉他版", "抖音", "改编", "合唱版", "粤语版", "国语版", "英文版", "日文版"}

// Version lists the version markers in a title, e.g. "live" for "(Live)".
func Version(title string) string {
	lower := strings.ToLower(title)
	var found []string
	for _, w := range versionWords {
		if strings.Contains(lower, w) {
			found = append(found, w)
		}
	}
	return strings.Join(found, "+")
}

// SameSong decides whether two platform tracks are the same recording:
// same base title and version markers, a shared artist, and lengths within
// six seconds. A live cut and the studio take are different songs.
func SameSong(a, b Track) bool {
	ta, tb := fold(bracketed.ReplaceAllString(a.Title, "")), fold(bracketed.ReplaceAllString(b.Title, ""))
	if ta == "" || ta != tb || Version(a.Title) != Version(b.Title) {
		return false
	}
	if a.Duration > 0 && b.Duration > 0 && abs(a.Duration-b.Duration) > 6 {
		return false
	}
	joined := fold(strings.Join(b.Artists, " "))
	for _, artist := range a.Artists {
		if f := fold(artist); f != "" && (strings.Contains(joined, f) || strings.Contains(f, joined) && joined != "") {
			return true
		}
	}
	return false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// LyricsSource is a provider that can also return LRC lyrics for its tracks.
type LyricsSource interface {
	Lyrics(ctx context.Context, t Track) (string, error)
}
