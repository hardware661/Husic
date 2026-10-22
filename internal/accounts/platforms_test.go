package accounts

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/skip2/go-qrcode"
	"music/internal/platform"
)

type fakePlatform struct {
	mu               sync.Mutex
	id               string
	used, polls      int
	entered, release chan struct{}
}

func (p *fakePlatform) Start(context.Context) (platform.Login, error) {
	png, _ := qrcode.Encode("https://example.test/login", qrcode.Medium, 320)
	return platform.Login{Key: "private-qr-key", Image: png}, nil
}
func (p *fakePlatform) Poll(context.Context, string) (platform.LoginResult, error) {
	p.mu.Lock()
	p.polls++
	p.mu.Unlock()
	if p.entered != nil {
		close(p.entered)
		<-p.release
	}
	return platform.LoginResult{State: "confirmed", Session: &platform.Session{ID: "42", Cookies: map[string]string{"fixture": "private-session-value"}}}, nil
}
func (p *fakePlatform) Verify(context.Context, platform.Session) (platform.Profile, error) {
	return platform.Profile{ID: "42", Name: "Listener"}, nil
}
func (p *fakePlatform) Account(context.Context) (platform.Profile, error) {
	return platform.Profile{ID: p.Identity(), Name: "Listener"}, nil
}
func (p *fakePlatform) UseSession(s platform.Session) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.id = s.ID
	p.used++
	return nil
}
func (p *fakePlatform) Identity() string { p.mu.Lock(); defer p.mu.Unlock(); return p.id }

func TestPlatformCancelledPollCannotReplaceSession(t *testing.T) {
	for _, kind := range []string{"qq", "bilibili"} {
		t.Run(kind, func(t *testing.T) {
			p := &fakePlatform{id: "42", entered: make(chan struct{}), release: make(chan struct{})}
			m := Manager{Dir: t.TempDir(), Platforms: map[string]Platform{kind: p}}
			a, err := m.StartPlatform(context.Background(), kind)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { defer close(done); _, _ = m.CheckPlatform(context.Background(), kind, a.ID) }()
			<-p.entered
			m.Cancel(a.ID)
			close(p.release)
			<-done
			if p.used != 0 {
				t.Fatal("cancelled candidate installed")
			}
			if _, err = os.Stat(filepath.Join(m.Dir, kind+"-session.json")); !os.IsNotExist(err) {
				t.Fatal("cancelled session persisted")
			}
		})
	}
}
func TestPlatformCandidateCommitAndRestore(t *testing.T) {
	p := &fakePlatform{}
	m := Manager{Dir: t.TempDir(), Platforms: map[string]Platform{"qq": p}}
	a, err := m.StartPlatform(context.Background(), "qq")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(a)
	if strings.Contains(string(body), "private-") {
		t.Fatal("private login state exposed")
	}
	if image, err := m.PlatformQR("qq", a.ID); err != nil || len(image) == 0 {
		t.Fatal("QR missing", err)
	}
	connected, err := m.CheckPlatform(context.Background(), "qq", a.ID)
	if err != nil || connected.State != "connected" {
		t.Fatalf("%+v %v", connected, err)
	}
	_, _ = m.CheckPlatform(context.Background(), "qq", a.ID)
	if p.used != 1 || p.polls != 1 {
		t.Fatal("candidate applied twice")
	}
	info, err := os.Stat(filepath.Join(m.Dir, "qq-session.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("session permissions", err)
	}
	if _, err = m.PlatformQR("qq", a.ID); err == nil {
		t.Fatal("consumed QR still exposed")
	}
	p2 := &fakePlatform{}
	restored := Manager{Dir: m.Dir, Platforms: map[string]Platform{"qq": p2}}
	if err := restored.RestorePlatform("qq"); err != nil || p2.Identity() != "42" {
		t.Fatal("restore", err)
	}
}
func TestPlatformDifferentAccountAndReplacedQR(t *testing.T) {
	p := &fakePlatform{id: "99"}
	m := Manager{Dir: t.TempDir(), Platforms: map[string]Platform{"bilibili": p}}
	a, _ := m.StartPlatform(context.Background(), "bilibili")
	b, _ := m.StartPlatform(context.Background(), "bilibili")
	if _, err := m.CheckPlatform(context.Background(), "bilibili", a.ID); err == nil {
		t.Fatal("old QR accepted")
	}
	result, err := m.CheckPlatform(context.Background(), "bilibili", b.ID)
	if err != nil || result.State != "failed" || p.Identity() != "99" || p.used != 0 {
		t.Fatalf("wrong account replaced: %+v %v", result, err)
	}
}
