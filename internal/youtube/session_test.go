package youtube

import (
	"context"
	"music/internal/source"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTemporaryVerificationIsNotRevokedAccount(t *testing.T) {
	dir := t.TempDir()
	seed := filepath.Join(dir, "seed")
	_ = os.WriteFile(seed, []byte("# Netscape HTTP Cookie File\n"), 0600)
	tool := filepath.Join(dir, "extractor")
	_ = os.WriteFile(tool, []byte("#!/bin/sh\necho 'Sign in to confirm you are not a bot' >&2\nexit 1\n"), 0700)
	p, err := New(Config{Binary: tool, Deno: tool, Cookies: seed, StateDir: filepath.Join(dir, "state")})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p.Stream(context.Background(), source.Track{ID: "abcdefghijk"})
	_, revoked := p.Status()
	if revoked {
		t.Fatal("temporary platform verification was classified as revoked cookies")
	}
}

func TestVerificationCooldownCanRecoverWithoutAnotherImport(t *testing.T) {
	dir := t.TempDir()
	seed := filepath.Join(dir, "seed")
	_ = os.WriteFile(seed, []byte("# Netscape HTTP Cookie File\n"), 0600)
	tool := filepath.Join(dir, "extractor")
	script := "#!/bin/sh\nif [ ! -f '" + filepath.Join(dir, "once") + "' ]; then touch '" + filepath.Join(dir, "once") + "'; echo 'Sign in to confirm you are not a bot' >&2; exit 1; fi\nprintf '%s' '{\"url\":\"https://rr.googlevideo.com/audio?expire=9999999999\",\"protocol\":\"https\",\"acodec\":\"mp4a.40.2\",\"duration\":180,\"abr\":128}'\n"
	_ = os.WriteFile(tool, []byte(script), 0700)
	p, err := New(Config{Binary: tool, Deno: tool, Cookies: seed, StateDir: filepath.Join(dir, "state")})
	if err != nil {
		t.Fatal(err)
	}
	track := source.Track{ID: "abcdefghijk"}
	if _, err = p.Stream(context.Background(), track); err != ErrVerification {
		t.Fatal("expected verification state")
	}
	if _, err = p.Stream(context.Background(), track); err != ErrVerification {
		t.Fatal("cooldown did not suppress repeated extraction")
	}
	p.mu.Lock()
	p.retryAfter = time.Now().Add(-time.Second)
	p.mu.Unlock()
	if _, err = p.Stream(context.Background(), track); err != nil {
		t.Fatal("could not recover after cooldown", err)
	}
	if p.Details().State != "connected" {
		t.Fatal("successful extraction did not clear verification state")
	}
}

func TestConnectionAcceptsOnlyYouTubeCookieDomains(t *testing.T) {
	for _, domain := range []string{".youtube.com", "music.youtube.com", "youtube.com.evil.test", "accounts.google.com"} {
		data := []byte("# Netscape HTTP Cookie File\n" + domain + "\tTRUE\t/\tTRUE\t9999999999\tSAPISID\tfixture\n")
		if validCookies(data) != (domain == ".youtube.com" || domain == "music.youtube.com") {
			t.Fatal("incorrect cookie domain admission", domain)
		}
	}
}

func TestRestoredObservationDoesNotPretendCurrentConnectionWasChecked(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "last-success.json"), []byte(`"2026-10-21T00:00:00Z"`), 0600)
	p := &Provider{cfg: Config{StateDir: dir, Cookies: filepath.Join(dir, "import")}}
	_ = os.WriteFile(p.cfg.Cookies, []byte("fixture"), 0600)
	p.restoreObservation()
	status := p.Details()
	if status.State != "configured" || status.LastSuccess == nil {
		t.Fatal("old observation was treated as a fresh account check")
	}
}

func TestFailedExtractionCannotOverwriteSavedSession(t *testing.T) {
	dir := t.TempDir()
	seed := filepath.Join(dir, "seed")
	old := "# Netscape HTTP Cookie File\n# original fixture\n"
	_ = os.WriteFile(seed, []byte(old), 0600)
	tool := filepath.Join(dir, "extractor")
	_ = os.WriteFile(tool, []byte("#!/bin/sh\nwhile [ $# -gt 0 ]; do if [ \"$1\" = --cookies ]; then shift; printf broken > \"$1\"; fi; shift; done\necho 'network failure' >&2\nexit 1\n"), 0700)
	p, err := New(Config{Binary: tool, Deno: tool, Cookies: seed, StateDir: filepath.Join(dir, "state")})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p.Stream(context.Background(), source.Track{ID: "abcdefghijk"})
	data, err := os.ReadFile(filepath.Join(dir, "state", "cookies.txt"))
	if err != nil || !strings.Contains(string(data), "original fixture") {
		t.Fatal("failed extraction corrupted the persisted jar")
	}
}
