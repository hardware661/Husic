package accounts

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hardware661/husic/internal/netease"
	"github.com/hardware661/husic/internal/youtube"
)

func TestGuestSeedCanReconnectAndRestore(t *testing.T) {
	dir := t.TempDir()
	seed := []byte("{}")
	session, err := RestoreNetEase(seed, dir)
	if err != nil || session.MusicU != "" {
		t.Fatal("guest startup must not require a login", err)
	}
	saved, _ := json.Marshal(savedNetEase{SeedHash: SeedHash(seed), Session: netease.Session{MusicU: "fixture-reconnected", UserID: 7}})
	if err = os.WriteFile(filepath.Join(dir, "netease-session.json"), saved, 0600); err != nil {
		t.Fatal(err)
	}
	session, err = RestoreNetEase(seed, dir)
	if err != nil || session.MusicU != "fixture-reconnected" {
		t.Fatal("guest QR connection must persist", err)
	}
}

type fixtureNetEase struct {
	id        int64
	confirmed bool
	used      int
	session   netease.Session
}

func (f *fixtureNetEase) QRKey(context.Context) (string, error) { return "private-qr-fixture", nil }
func (f *fixtureNetEase) QRCheck(context.Context, string) (int, string, []*http.Cookie, error) {
	return netease.QRConfirmed, "", []*http.Cookie{{Name: "MUSIC_U", Value: "private-session-fixture"}, {Name: "__csrf", Value: "private-csrf-fixture"}}, nil
}
func (f *fixtureNetEase) Account(context.Context) (netease.Account, error) {
	return netease.Account{UserID: f.id, Nickname: "fixture"}, nil
}
func (f *fixtureNetEase) VerifySession(context.Context, netease.Session) (netease.Account, error) {
	id := f.id
	if !f.confirmed {
		id++
	}
	return netease.Account{UserID: id, Nickname: "fixture"}, nil
}
func (f *fixtureNetEase) UseSession(s netease.Session) { f.used++; f.session = s }
func (f *fixtureNetEase) UserID() int64                { return f.id }

func TestNetEaseReconnectIsPrivatePersistentAndOneUse(t *testing.T) {
	dir := t.TempDir()
	client := &fixtureNetEase{id: 7, confirmed: true}
	m := &Manager{Dir: dir, SeedHash: "seed", NetEase: client}
	a, err := m.StartNetEase(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	qr, _ := m.NetEaseQR(a.ID)
	if !strings.HasPrefix(qr, "https://music.163.com/login?") {
		t.Fatal("bad QR")
	}
	a, err = m.CheckNetEase(context.Background(), a.ID)
	if err != nil || a.State != "connected" {
		t.Fatal("reconnect failed", err)
	}
	_, _ = m.CheckNetEase(context.Background(), a.ID)
	if client.used != 1 {
		t.Fatal("QR completion replayed")
	}
	info, err := os.Stat(filepath.Join(dir, "netease-session.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("session not private")
	}
	public, _ := json.Marshal(a)
	if strings.Contains(string(public), "private-") {
		t.Fatal("credential appeared in public result")
	}
}

func TestWrongNetEaseAccountAndCancelledQRNeverReplaceCurrent(t *testing.T) {
	f := &fixtureNetEase{id: 7}
	m := &Manager{Dir: t.TempDir(), NetEase: f}
	a, _ := m.StartNetEase(context.Background())
	a, _ = m.CheckNetEase(context.Background(), a.ID)
	if a.State != "failed" || f.used != 0 {
		t.Fatal("switched account without approval")
	}
	a, _ = m.StartNetEase(context.Background())
	m.Cancel(a.ID)
	if _, err := m.CheckNetEase(context.Background(), a.ID); err == nil || f.used != 0 {
		t.Fatal("cancelled QR was accepted")
	}
}

func TestSavedNetEaseFollowsOriginalImportVersion(t *testing.T) {
	dir := t.TempDir()
	seed, _ := json.Marshal(netease.Session{MusicU: "original", UserID: 7})
	saved, _ := json.Marshal(savedNetEase{SeedHash: SeedHash(seed), Session: netease.Session{MusicU: "reconnected", UserID: 7}})
	_ = os.WriteFile(filepath.Join(dir, "netease-session.json"), saved, 0600)
	session, _ := RestoreNetEase(seed, dir)
	if session.MusicU != "reconnected" {
		t.Fatal("reconnect lost on restart")
	}
	newSeed, _ := json.Marshal(netease.Session{MusicU: "admin-new-import", UserID: 7})
	session, _ = RestoreNetEase(newSeed, dir)
	if session.MusicU != "admin-new-import" {
		t.Fatal("new import was shadowed")
	}
}

type fixtureYouTube struct{ calls int }

func (f *fixtureYouTube) Details() youtube.ConnectionStatus {
	return youtube.ConnectionStatus{State: "configured"}
}

type delayedYouTube struct {
	started, release chan struct{}
	committed        int
}

func (f *delayedYouTube) Details() youtube.ConnectionStatus {
	return youtube.ConnectionStatus{State: "configured"}
}
func (f *delayedYouTube) Verify(context.Context) error { return nil }
func (f *delayedYouTube) ConnectGuarded(_ context.Context, _ []byte, guard youtube.PromotionGuard) error {
	close(f.started)
	<-f.release
	return guard(func() error { f.committed++; return nil })
}
func TestCancelledYouTubeAttemptCannotPromoteLateResult(t *testing.T) {
	f := &delayedYouTube{started: make(chan struct{}), release: make(chan struct{})}
	m := &Manager{YouTube: f}
	a, _ := m.StartYouTube()
	done := make(chan struct{})
	go func() { defer close(done); _, _ = m.CompleteYouTube(context.Background(), a.ID, []byte("fixture")) }()
	<-f.started
	m.Cancel(a.ID)
	close(f.release)
	<-done
	if f.committed != 0 {
		t.Fatal("cancelled connection replaced active credentials")
	}
}
func (f *fixtureYouTube) ConnectGuarded(context.Context, []byte, youtube.PromotionGuard) error {
	f.calls++
	return youtube.ErrVerification
}
func (f *fixtureYouTube) Verify(context.Context) error { return youtube.ErrVerification }
func TestYouTubeAttemptIsOneUseAndDoesNotPretendValidationPassed(t *testing.T) {
	f := &fixtureYouTube{}
	m := &Manager{YouTube: f}
	a, _ := m.StartYouTube()
	a, err := m.CompleteYouTube(context.Background(), a.ID, []byte("fixture"))
	if err != nil || a.State != "verification_required" {
		t.Fatal("verification failure hidden")
	}
	_, err = m.CompleteYouTube(context.Background(), a.ID, []byte("fixture"))
	if err == nil || f.calls != 1 {
		t.Fatal("reused attempt")
	}
}
