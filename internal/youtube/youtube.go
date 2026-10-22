// Package youtube resolves official YouTube Music audio and relays it from
// the same host (Google's signed media addresses are bound to that host's IP).
package youtube

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"music/internal/privatefile"
	"music/internal/source"
	"music/internal/ytmusic"
)

type Config struct {
	Binary, Deno, Cookies, StateDir string
}

type Provider struct {
	cfg         Config
	search      *ytmusic.Client
	http        *http.Client
	lane        chan struct{}
	mu          sync.Mutex
	entries     map[string]entry
	tokens      map[string]string
	expired     bool
	importHash  string
	failure     string
	retryAfter  time.Time
	lastSuccess time.Time
	verified    bool
}

type entry struct {
	id, token, url, userAgent, referer string
	until                              time.Time
	bitrate, seconds                   int
}

var videoID = regexp.MustCompile(`^[a-zA-Z0-9_-]{11}$`)
var ErrSession = errors.New("youtube login session needs attention")
var ErrVerification = errors.New("youtube playback requires verification")

func New(cfg Config) (*Provider, error) {
	for _, binary := range []string{cfg.Binary, cfg.Deno} {
		info, err := os.Stat(binary)
		if err != nil || !filepath.IsAbs(binary) || info.IsDir() || info.Mode()&0111 == 0 {
			return nil, errors.New("youtube private tool is unavailable")
		}
	}
	if !filepath.IsAbs(cfg.StateDir) {
		return nil, errors.New("youtube state directory must be absolute")
	}
	p := &Provider{cfg: cfg, search: ytmusic.New(4 * time.Second), http: mediaClient(), lane: make(chan struct{}, 1), entries: map[string]entry{}, tokens: map[string]string{}}
	p.restoreObservation()
	return p, nil
}

func (*Provider) Name() string          { return "youtube" }
func (*Provider) Budget() time.Duration { return 30 * time.Second }

func (p *Provider) Search(ctx context.Context, query string, limit int) ([]source.Track, error) {
	rows, err := p.search.SearchSongs(ctx, query)
	if err != nil {
		return nil, err
	}
	tracks := make([]source.Track, 0, min(limit, len(rows)))
	for _, r := range rows {
		if !videoID.MatchString(r.VideoID) {
			continue
		}
		parts := strings.Split(r.Subtitle, " • ")
		t := source.Track{Source: "youtube", ID: r.VideoID, Title: r.Title, Cover: "https://i.ytimg.com/vi/" + r.VideoID + "/hqdefault.jpg"}
		if len(parts) > 0 {
			t.Artists = []string{strings.TrimSpace(parts[0])}
		}
		if len(parts) > 1 {
			t.Duration = parseDuration(parts[len(parts)-1])
		}
		if len(parts) > 2 {
			t.Album = strings.TrimSpace(parts[1])
		}
		tracks = append(tracks, t)
		if len(tracks) == limit {
			break
		}
	}
	return tracks, nil
}

func parseDuration(s string) int {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	total := 0
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return 0
		}
		total = total*60 + n
	}
	return total
}

func (p *Provider) Stream(ctx context.Context, t source.Track) (source.Stream, error) {
	if !videoID.MatchString(t.ID) {
		return source.Stream{}, source.ErrUnavailable
	}
	select {
	case p.lane <- struct{}{}:
		defer func() { <-p.lane }()
	case <-ctx.Done():
		return source.Stream{}, ctx.Err()
	}
	jar, err := p.session()
	if err != nil {
		return source.Stream{}, err
	}
	p.mu.Lock()
	hit, ok := p.entries[t.ID]
	p.mu.Unlock()
	if ok && time.Now().Before(hit.until) {
		return hit.stream(), nil
	}
	// The subprocess only writes a disposable copy. A failed or interrupted
	// extraction must never truncate the last saved working session.
	data, err := os.ReadFile(jar)
	if err != nil {
		return source.Stream{}, ErrSession
	}
	work, err := os.CreateTemp(p.cfg.StateDir, ".extract-*.cookies")
	if err != nil {
		return source.Stream{}, source.ErrUnavailable
	}
	workingJar := work.Name()
	defer os.Remove(workingJar)
	if _, err = work.Write(data); err != nil {
		work.Close()
		return source.Stream{}, source.ErrUnavailable
	}
	if err = work.Close(); err != nil {
		return source.Stream{}, source.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	args := []string{"--ignore-config", "--no-plugin-dirs", "--no-remote-components", "--no-playlist", "--skip-download", "--no-warnings", "--no-progress",
		"--proxy", "", "--force-ipv4", "--socket-timeout", "8", "--retries", "0", "--extractor-retries", "0", "--js-runtimes", "deno:" + p.cfg.Deno,
		"--cache-dir", filepath.Join(p.cfg.StateDir, "cache"), "--cookies", workingJar,
		"-f", "bestaudio[acodec^=mp4a][protocol=https]/bestaudio[ext=m4a][protocol=https]", "--dump-single-json", "--", "https://music.youtube.com/watch?v=" + t.ID}
	cmd := exec.CommandContext(ctx, p.cfg.Binary, args...)
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "DENO_DIR=") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "DENO_DIR="+filepath.Join(p.cfg.StateDir, "deno"))
	cmd.WaitDelay = 2 * time.Second
	out, stderr := &boundedBuffer{limit: 2 << 20}, &boundedBuffer{limit: 32 << 10}
	cmd.Stdout, cmd.Stderr = out, stderr
	if err := cmd.Run(); err != nil {
		// Never expose extractor stderr: it can include signed URLs or cookies.
		lower := strings.ToLower(string(stderr.data))
		if failure := p.recordFailure(lower); failure != nil {
			return source.Stream{}, failure
		}
		if ctx.Err() != nil {
			return source.Stream{}, ctx.Err()
		}
		return source.Stream{}, source.ErrUnavailable
	}
	if out.overflow {
		return source.Stream{}, source.ErrUnavailable
	}
	e, err := parseEntry(t.ID, out.data)
	if err != nil {
		return source.Stream{}, err
	}
	updated, err := os.ReadFile(workingJar)
	if err != nil || len(updated) == 0 {
		return source.Stream{}, source.ErrUnavailable
	}
	if err = privatefile.Write(jar, updated); err != nil {
		return source.Stream{}, source.ErrUnavailable
	}
	p.recordSuccess()
	p.mu.Lock()
	for id, old := range p.entries {
		if time.Now().After(old.until) {
			delete(p.tokens, old.token)
			delete(p.entries, id)
		}
	}
	if old, ok := p.entries[t.ID]; ok {
		delete(p.tokens, old.token)
	}
	p.entries[t.ID] = e
	p.tokens[e.token] = t.ID
	p.mu.Unlock()
	return e.stream(), nil
}

func (e entry) stream() source.Stream {
	return source.Stream{URL: "/api/media/" + e.token, Bitrate: e.bitrate, Format: "m4a", Seconds: e.seconds}
}

func (p *Provider) Forget(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.entries[id]; ok {
		delete(p.tokens, e.token)
		delete(p.entries, id)
	}
}

func (p *Provider) Status() (bool, bool) {
	_, err := p.importedCookies()
	p.mu.Lock()
	defer p.mu.Unlock()
	return err == nil, p.expired
}

func (p *Provider) importedCookies() ([]byte, error) {
	data, err := os.ReadFile(p.cfg.Cookies)
	if err != nil {
		return nil, ErrSession
	}
	data = p.connectedImport(data)
	if len(data) == 0 {
		return nil, ErrSession
	}
	return data, nil
}

// Serialized with extraction, so yt-dlp may safely persist rotated cookies.
// A changed import replaces only this app's private jar, never the browser's.
func (p *Provider) session() (string, error) {
	data, err := p.importedCookies()
	if err != nil {
		return "", ErrSession
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	jar := filepath.Join(p.cfg.StateDir, "cookies.txt")
	if err = os.MkdirAll(p.cfg.StateDir, 0700); err != nil {
		return "", errors.New("youtube session storage unavailable")
	}
	stamp, _ := os.ReadFile(filepath.Join(p.cfg.StateDir, "import.sha256"))
	_, jarError := os.Stat(jar)
	if string(stamp) != hash || jarError != nil {
		if err = privatefile.Write(jar, data); err != nil {
			return "", errors.New("youtube session storage unavailable")
		}
		if err = os.Chmod(jar, 0600); err != nil {
			return "", errors.New("youtube session storage unavailable")
		}
		if err = privatefile.Write(filepath.Join(p.cfg.StateDir, "import.sha256"), []byte(hash)); err != nil {
			return "", errors.New("youtube session storage unavailable")
		}
	}
	p.mu.Lock()
	if p.importHash != hash {
		p.expired = false
		p.importHash = hash
		p.failure = ""
		p.retryAfter = time.Time{}
		p.verified = false
	}
	expired := p.expired
	blocked := time.Now().Before(p.retryAfter)
	p.mu.Unlock()
	if expired {
		return "", ErrSession
	}
	if blocked {
		return "", ErrVerification
	}
	return jar, nil
}

type boundedBuffer struct {
	data     []byte
	limit    int
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(b.data)+n > b.limit {
		b.overflow = true
	}
	b.data = append(b.data, p[:min(n, b.limit-len(b.data))]...)
	return n, nil
}

func parseEntry(id string, data []byte) (entry, error) {
	var raw struct {
		URL      string            `json:"url"`
		Protocol string            `json:"protocol"`
		Codec    string            `json:"acodec"`
		Live     bool              `json:"is_live"`
		DRM      bool              `json:"has_drm"`
		ABR      float64           `json:"abr"`
		Duration float64           `json:"duration"`
		Headers  map[string]string `json:"http_headers"`
	}
	if json.Unmarshal(data, &raw) != nil || raw.Live || raw.DRM || raw.Protocol != "https" || !strings.HasPrefix(raw.Codec, "mp4a") || !validMediaURL(raw.URL) {
		return entry{}, source.ErrUnavailable
	}
	u, _ := url.Parse(raw.URL)
	expiry, _ := strconv.ParseInt(u.Query().Get("expire"), 10, 64)
	until := time.Now().Add(10 * time.Minute)
	if expiry > 0 {
		until = time.Now().Add(5 * time.Hour)
		if signedUntil := time.Unix(expiry, 0).Add(-time.Minute); signedUntil.Before(until) {
			until = signedUntil
		}
	}
	if !until.After(time.Now()) {
		return entry{}, source.ErrUnavailable
	}
	token := make([]byte, 24)
	if _, err := io.ReadFull(rand.Reader, token); err != nil {
		return entry{}, err
	}
	return entry{id: id, token: base64.RawURLEncoding.EncodeToString(token), url: raw.URL, userAgent: raw.Headers["User-Agent"], referer: raw.Headers["Referer"], until: until, bitrate: int(raw.ABR * 1000), seconds: int(raw.Duration)}, nil
}
