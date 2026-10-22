package youtube

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGuestHasNoSessionButReconnectWorksWithEmptySeed(t *testing.T) {
	dir := t.TempDir()
	seed := filepath.Join(dir, "import")
	if err := os.WriteFile(seed, nil, 0600); err != nil {
		t.Fatal(err)
	}
	p := &Provider{cfg: Config{StateDir: dir, Cookies: seed}}
	if p.Details().State != "not_connected" {
		t.Fatal("empty import displayed as connected")
	}
	if ready, _ := p.Status(); ready {
		t.Fatal("guest import treated as playable")
	}
	if _, err := p.session(); err != ErrSession {
		t.Fatal("guest must not reuse a private jar")
	}
	saved, _ := json.Marshal(connectedSession{SeedHash: digest(nil), Cookies: "fixture-reconnected-cookies"})
	if err := os.WriteFile(filepath.Join(dir, "connected.json"), saved, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.session(); err != nil {
		t.Fatal("reconnect after guest mode failed", err)
	}
	if ready, _ := p.Status(); !ready {
		t.Fatal("new session not available")
	}
}
