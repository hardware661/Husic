package youtube

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"music/internal/privatefile"
	"music/internal/source"
)

type ConnectionStatus struct {
	State       string     `json:"state"`
	LastSuccess *time.Time `json:"lastSuccess,omitempty"`
}

type connectedSession struct {
	SeedHash string `json:"seedHash"`
	Cookies  string `json:"cookies"`
}

// PromotionGuard lets the owner of a login attempt atomically reject late
// results after cancellation or replacement, without holding locks during I/O.
type PromotionGuard func(func() error) error

func digest(data []byte) string { hash := sha256.Sum256(data); return hex.EncodeToString(hash[:]) }

func (p *Provider) connectedImport(seed []byte) []byte {
	data, err := os.ReadFile(filepath.Join(p.cfg.StateDir, "connected.json"))
	var saved connectedSession
	if err == nil && json.Unmarshal(data, &saved) == nil && saved.SeedHash == digest(seed) && saved.Cookies != "" {
		return []byte(saved.Cookies)
	}
	return seed
}

func (p *Provider) Details() ConnectionStatus {
	_, seedErr := p.importedCookies()
	configured := seedErr == nil
	p.mu.Lock()
	defer p.mu.Unlock()
	state := "configured"
	if !configured {
		state = "not_connected"
	}
	if p.verified {
		state = "connected"
	}
	if p.failure != "" {
		state = p.failure
	}
	if !configured {
		state = "not_connected"
	}
	var last *time.Time
	if configured && !p.lastSuccess.IsZero() {
		value := p.lastSuccess
		last = &value
	}
	return ConnectionStatus{State: state, LastSuccess: last}
}

func (p *Provider) recordFailure(stderr string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if strings.Contains(stderr, "cookies are no longer valid") {
		p.expired = true
		p.failure = "needs_login"
		return ErrSession
	}
	if strings.Contains(stderr, "sign in") || strings.Contains(stderr, "not a bot") {
		p.failure = "verification_required"
		p.retryAfter = time.Now().Add(2 * time.Minute)
		return ErrVerification
	}
	p.failure = "unavailable"
	return nil
}

func (p *Provider) restoreObservation() {
	data, err := os.ReadFile(filepath.Join(p.cfg.StateDir, "last-success.json"))
	if err == nil {
		_ = json.Unmarshal(data, &p.lastSuccess)
	}
}

func (p *Provider) recordSuccess() {
	p.mu.Lock()
	p.lastSuccess = time.Now().UTC()
	p.verified = true
	p.failure = ""
	p.expired = false
	p.retryAfter = time.Time{}
	data, _ := json.Marshal(p.lastSuccess)
	p.mu.Unlock()
	_ = privatefile.Write(filepath.Join(p.cfg.StateDir, "last-success.json"), data)
}

// Verify checks both extraction and real audio bytes, not just account cookies.
func (p *Provider) Verify(ctx context.Context) (result error) {
	defer func() {
		if result != nil && !errors.Is(result, ErrSession) && !errors.Is(result, ErrVerification) {
			p.recordFailure("media check failed")
		}
	}()
	_, err := p.Stream(ctx, source.Track{Source: "youtube", ID: "IslhmC1Sw-I"})
	if err != nil {
		return err
	}
	p.mu.Lock()
	entry := p.entries["IslhmC1Sw-I"]
	p.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.url, nil)
	if err != nil {
		return source.ErrUnavailable
	}
	req.Header.Set("Range", "bytes=0-65535")
	if entry.userAgent != "" {
		req.Header.Set("User-Agent", entry.userAgent)
	}
	if entry.referer != "" {
		req.Header.Set("Referer", entry.referer)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return source.ErrUnavailable
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || resp.StatusCode != 206 || len(data) != 65536 || !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "audio/") {
		return source.ErrUnavailable
	}
	return nil
}

func validCookies(data []byte) bool {
	if len(data) > 512<<10 || len(data) == 0 {
		return false
	}
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "#HttpOnly_") {
			line = strings.TrimPrefix(line, "#HttpOnly_")
		} else if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 7 {
			return false
		}
		domain := strings.TrimPrefix(parts[0], ".")
		if domain != "youtube.com" && !strings.HasSuffix(domain, ".youtube.com") {
			return false
		}
		expiry, err := strconv.ParseInt(parts[4], 10, 64)
		if err != nil {
			return false
		}
		if parts[5] == "SAPISID" && parts[6] != "" && (expiry == 0 || expiry > time.Now().Unix()) {
			found = true
		}
	}
	return found
}

// Connect promotes a new independent session only after it really plays.
// Failed attempts never replace the currently stored import or active jar.
func (p *Provider) Connect(ctx context.Context, data []byte) error {
	return p.ConnectGuarded(ctx, data, nil)
}

func (p *Provider) ConnectGuarded(ctx context.Context, data []byte, guard PromotionGuard) error {
	if !validCookies(data) {
		return errors.New("invalid YouTube session")
	}
	if err := os.MkdirAll(p.cfg.StateDir, 0700); err != nil {
		return source.ErrUnavailable
	}
	dir, err := os.MkdirTemp(p.cfg.StateDir, ".connect-*")
	if err != nil {
		return source.ErrUnavailable
	}
	defer os.RemoveAll(dir)
	input := filepath.Join(dir, "import.txt")
	if err = privatefile.Write(input, data); err != nil {
		return source.ErrUnavailable
	}
	probe, err := New(Config{Binary: p.cfg.Binary, Deno: p.cfg.Deno, Cookies: input, StateDir: filepath.Join(dir, "state")})
	if err != nil {
		return source.ErrUnavailable
	}
	if err = probe.Verify(ctx); err != nil {
		return err
	}
	updated, err := os.ReadFile(filepath.Join(dir, "state", "cookies.txt"))
	if err != nil || !validCookies(updated) {
		return source.ErrUnavailable
	}
	seed, err := os.ReadFile(p.cfg.Cookies)
	if err != nil {
		return ErrSession
	}
	select {
	case p.lane <- struct{}{}:
		defer func() { <-p.lane }()
	case <-ctx.Done():
		return ctx.Err()
	}
	promote := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		saved, _ := json.Marshal(connectedSession{SeedHash: digest(seed), Cookies: string(updated)})
		if err = privatefile.Write(filepath.Join(p.cfg.StateDir, "connected.json"), saved); err != nil {
			return source.ErrUnavailable
		}
		p.mu.Lock()
		p.importHash = ""
		p.mu.Unlock()
		if _, err = p.session(); err != nil {
			return err
		}
		// Existing signed addresses remain usable until their normal expiry,
		// so reconnecting does not invalidate a currently playing Range stream.
		p.recordSuccess()
		return nil
	}
	if guard != nil {
		return guard(promote)
	}
	return promote()
}
