package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"music/internal/platform"
)

type fakeMigu struct {
	fakePlatform
	sent    int
	sendErr error
}

func (p *fakeMigu) MiguSendCode(context.Context, string) error { p.sent++; return p.sendErr }
func (p *fakeMigu) MiguLogin(context.Context, string, string) (platform.Session, error) {
	if p.entered != nil {
		close(p.entered)
		<-p.release
	}
	return platform.Session{ID: "42", Cookies: map[string]string{"pacmtoken": "private-migu-session"}}, nil
}

func TestMiguExplicitSMSCooldownAndPrivateState(t *testing.T) {
	p := &fakeMigu{}
	m := Manager{Dir: t.TempDir(), Platforms: map[string]Platform{"migu": p}}
	// The status endpoint and opening an attempt elsewhere never send SMS.
	_ = m.Status(context.Background())
	if p.sent != 0 {
		t.Fatal("status sent SMS")
	}
	a, e := m.StartMigu(context.Background(), "13800000000")
	if e != nil || p.sent != 1 {
		t.Fatal("explicit send", e)
	}
	body, _ := json.Marshal(a)
	if strings.Contains(string(body), "13800000000") || strings.Contains(string(body), "private-") {
		t.Fatal("private login state exposed")
	}
	if _, e = m.StartMigu(context.Background(), "13800000000"); e == nil || p.sent != 1 {
		t.Fatal("duplicate SMS not blocked")
	}
	result, e := m.CompleteMigu(context.Background(), a.ID, "123456")
	if e != nil || result.State != "connected" {
		t.Fatal("verified promotion", e)
	}
	info, e := os.Stat(filepath.Join(m.Dir, "migu-session.json"))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private session", e)
	}
	_, _ = m.CompleteMigu(context.Background(), a.ID, "123456")
	if p.used != 1 {
		t.Fatal("replayed code promoted twice")
	}
}

func TestMiguTimeoutDoesNotAutomaticallyResend(t *testing.T) {
	p := &fakeMigu{sendErr: errors.New("network timeout")}
	m := Manager{Platforms: map[string]Platform{"migu": p}}
	_, _ = m.StartMigu(context.Background(), "13800000000")
	_, _ = m.StartMigu(context.Background(), "13800000000")
	if p.sent != 1 {
		t.Fatal("ambiguous SMS request duplicated")
	}
}

func TestMiguCancelledLoginCannotPromote(t *testing.T) {
	p := &fakeMigu{fakePlatform: fakePlatform{entered: make(chan struct{}), release: make(chan struct{})}}
	m := Manager{Dir: t.TempDir(), Platforms: map[string]Platform{"migu": p}}
	a, e := m.StartMigu(context.Background(), "13800000000")
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = m.CompleteMigu(context.Background(), a.ID, "123456") }()
	<-p.entered
	m.Cancel(a.ID)
	close(p.release)
	<-done
	if p.used != 0 {
		t.Fatal("cancelled login installed")
	}
	if _, e = os.Stat(filepath.Join(m.Dir, "migu-session.json")); !os.IsNotExist(e) {
		t.Fatal("cancelled session persisted")
	}
}
