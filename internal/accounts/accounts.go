// Package accounts manages short-lived login attempts and private reconnects.
// Credentials never appear in status responses, URLs, logs, or the database.
package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"music/internal/netease"
	"music/internal/platform"
	"music/internal/privatefile"
	"music/internal/youtube"
)

type NetEase interface {
	QRKey(context.Context) (string, error)
	QRCheck(context.Context, string) (int, string, []*http.Cookie, error)
	Account(context.Context) (netease.Account, error)
	VerifySession(context.Context, netease.Session) (netease.Account, error)
	UseSession(netease.Session)
	UserID() int64
}

type YouTube interface {
	Details() youtube.ConnectionStatus
	ConnectGuarded(context.Context, []byte, youtube.PromotionGuard) error
	Verify(context.Context) error
}

type Status struct {
	State string `json:"state"`
	Name  string `json:"name,omitempty"`
}

var ErrAttemptGone = errors.New("这次连接已结束，请重新开始")

type Snapshot struct {
	NetEase  Status                   `json:"netease"`
	YouTube  youtube.ConnectionStatus `json:"youtube"`
	Bilibili Status                   `json:"bilibili"`
}

type Attempt struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	State    string    `json:"state"`
	Expires  time.Time `json:"expires"`
	Message  string    `json:"message,omitempty"`
	key      string
	image    []byte
	consumed bool
	checked  time.Time
	reported string
	mu       *sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
}

type Manager struct {
	Dir             string
	Log             *slog.Logger
	SeedHash        string
	NetEase         NetEase
	YouTube         YouTube
	Platforms       map[string]Platform
	mu              sync.Mutex
	attempts        map[string]*Attempt
	status          Status
	checked         time.Time
	platformStatus  map[string]Status
	platformChecked map[string]time.Time
}

type Platform interface {
	Start(context.Context) (platform.Login, error)
	Poll(context.Context, string) (platform.LoginResult, error)
	Verify(context.Context, platform.Session) (platform.Profile, error)
	Account(context.Context) (platform.Profile, error)
	UseSession(platform.Session) error
	Identity() string
}

type savedNetEase struct {
	SeedHash string          `json:"seedHash"`
	Session  netease.Session `json:"session"`
}

func SeedHash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func RestoreNetEase(seed []byte, dir string) (netease.Session, error) {
	var original netease.Session
	if json.Unmarshal(seed, &original) != nil {
		return original, errors.New("NetEase session file is not usable")
	}
	if dir != "" {
		data, err := os.ReadFile(filepath.Join(dir, "netease-session.json"))
		var saved savedNetEase
		if err == nil && json.Unmarshal(data, &saved) == nil && saved.SeedHash == SeedHash(seed) && saved.Session.MusicU != "" {
			return saved.Session, nil
		}
	}
	return original, nil
}

func (m *Manager) Status(ctx context.Context) Snapshot {
	m.mu.Lock()
	status := m.status
	refresh := m.NetEase != nil && time.Since(m.checked) > time.Minute
	if refresh {
		m.checked = time.Now()
	}
	m.mu.Unlock()
	guest, knowsSession := m.NetEase.(interface{ HasSession() bool })
	if m.NetEase == nil || knowsSession && !guest.HasSession() {
		status.State = "not_connected"
	} else if refresh {
		account, err := m.NetEase.Account(ctx)
		if err == nil {
			status = Status{State: "connected", Name: account.Nickname}
		} else {
			var platformError *netease.Error
			if errors.As(err, &platformError) && (platformError.Code == 301 || platformError.Code == 302) {
				status = Status{State: "needs_login"}
			} else {
				status = Status{State: "unavailable"}
			}
		}
		m.mu.Lock()
		m.status = status
		m.mu.Unlock()
	}
	if status.State == "" {
		status.State = "configured"
	}
	yt := youtube.ConnectionStatus{State: "not_connected"}
	if m.YouTube != nil {
		yt = m.YouTube.Details()
	}
	other := m.platformStatuses(ctx)
	return Snapshot{NetEase: status, YouTube: yt, Bilibili: other["bilibili"]}
}

func (m *Manager) newAttempt(kind, key string) (Attempt, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return Attempt{}, err
	}
	expires := time.Now().Add(5 * time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), expires)
	a := &Attempt{ID: base64.RawURLEncoding.EncodeToString(bytes), Kind: kind, State: "waiting", Expires: expires, key: key, mu: &sync.Mutex{}, ctx: ctx, cancel: cancel}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.attempts == nil {
		m.attempts = map[string]*Attempt{}
	}
	for id, old := range m.attempts {
		// Keep at most one live attempt per provider. Replacing an old QR must
		// make its completion harmless even if an upstream reply arrives late.
		if time.Now().After(old.Expires) || old.Kind == kind {
			old.cancel()
			delete(m.attempts, id)
		}
	}
	m.attempts[a.ID] = a
	return a.public(), nil
}

func (a *Attempt) public() Attempt {
	return Attempt{ID: a.ID, Kind: a.Kind, State: a.State, Expires: a.Expires, Message: a.Message}
}

func (m *Manager) get(id, kind string) (*Attempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.attempts[id]
	if a == nil || a.Kind != kind {
		return nil, ErrAttemptGone
	}
	if time.Now().After(a.Expires) {
		a.cancel()
		delete(m.attempts, id)
		return nil, ErrAttemptGone
	}
	return a, nil
}

func (m *Manager) stillCurrent(a *Attempt) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.attempts[a.ID] == a && time.Now().Before(a.Expires)
}

func (m *Manager) Cancel(id string) {
	m.mu.Lock()
	if old := m.attempts[id]; old != nil {
		old.cancel()
	}
	delete(m.attempts, id)
	m.mu.Unlock()
}

func (m *Manager) StartNetEase(ctx context.Context) (Attempt, error) {
	if m.NetEase == nil {
		return Attempt{}, errors.New("网易云连接尚未配置")
	}
	key, err := m.NetEase.QRKey(ctx)
	if err != nil || key == "" {
		return Attempt{}, errors.New("暂时无法生成二维码，请稍后再试")
	}
	return m.newAttempt("netease", key)
}

func (m *Manager) NetEaseQR(id string) (string, error) {
	a, err := m.get(id, "netease")
	if err != nil {
		return "", err
	}
	return netease.QRLoginURL(a.key), nil
}

func (m *Manager) CheckNetEase(ctx context.Context, id string) (Attempt, error) {
	a, err := m.get(id, "netease")
	if err != nil {
		return Attempt{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.ctx, cancel)
	defer stop()
	defer cancel()
	if a.consumed || time.Since(a.checked) < 2*time.Second {
		return a.public(), nil
	}
	a.checked = time.Now()
	code, _, cookies, err := m.NetEase.QRCheck(ctx, a.key)
	if err != nil {
		return a.public(), errors.New("连接暂时不通，正在等待重试")
	}
	switch code {
	case netease.QRExpired:
		a.State = "expired"
		a.consumed = true
	case netease.QRWaiting:
		a.State = "waiting"
	case netease.QRScanned:
		a.State = "scanned"
	case netease.QRConfirmed:
		a.consumed = true
		session := netease.Session{DeviceID: netease.NewDeviceID(), Saved: time.Now().UTC()}
		for _, cookie := range cookies {
			switch cookie.Name {
			case "MUSIC_U":
				session.MusicU = cookie.Value
			case "__csrf":
				session.CSRF = cookie.Value
			}
		}
		if session.MusicU == "" {
			a.State = "failed"
			a.Message = "未收到完整登录，请重新扫码"
			break
		}
		account, err := m.NetEase.VerifySession(ctx, session)
		if err != nil {
			a.State = "failed"
			a.Message = "登录未验证成功，请重试"
			break
		}
		if current := m.NetEase.UserID(); current != 0 && account.UserID != current {
			a.State = "failed"
			a.Message = "请使用当前连接的网易云账号，避免切换到其它账号"
			break
		}
		session.UserID = account.UserID
		if err := m.promote(a, func() error {
			body, _ := json.Marshal(savedNetEase{SeedHash: m.SeedHash, Session: session})
			if m.Dir == "" {
				return errors.New("connection storage unavailable")
			}
			if err := privatefile.Write(filepath.Join(m.Dir, "netease-session.json"), body); err != nil {
				return err
			}
			m.NetEase.UseSession(session)
			m.status = Status{State: "connected", Name: account.Nickname}
			m.checked = time.Now()
			return nil
		}); err != nil {
			a.State = "failed"
			a.Message = "保存连接失败，原连接没有改变"
			break
		}
		a.State = "connected"
	default:
		a.State = "failed"
		a.Message = "二维码暂时不可用，请刷新后重试"
	}
	return a.public(), nil
}

func (m *Manager) StartYouTube() (Attempt, error) {
	if m.YouTube == nil {
		return Attempt{}, errors.New("YouTube 连接尚未配置")
	}
	return m.newAttempt("youtube", "")
}

func (m *Manager) CompleteYouTube(ctx context.Context, id string, cookies []byte) (Attempt, error) {
	a, err := m.get(id, "youtube")
	if err != nil {
		return Attempt{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.consumed {
		return Attempt{}, errors.New("这次连接已使用，请重新开始")
	}
	a.consumed = true
	if !m.stillCurrent(a) {
		return Attempt{}, errors.New("连接已取消")
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.ctx, cancel)
	defer stop()
	defer cancel()
	err = m.YouTube.ConnectGuarded(ctx, cookies, func(commit func() error) error { return m.promote(a, commit) })
	if err != nil {
		a.State = "failed"
		a.Message = "登录已接收，但音频验证未通过，原连接没有替换"
		if errors.Is(err, youtube.ErrVerification) {
			a.State = "verification_required"
			a.Message = "登录已接收，YouTube 仍要求访问验证，尚未恢复播放"
		}
		return a.public(), nil
	}
	a.State = "connected"
	return a.public(), nil
}

func (m *Manager) promote(a *Attempt, commit func() error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.attempts[a.ID] != a || time.Now().After(a.Expires) || a.ctx.Err() != nil {
		return errors.New("连接已取消或过期")
	}
	return commit()
}
