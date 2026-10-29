package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hardware661/husic/internal/platform"
	"github.com/hardware661/husic/internal/privatefile"
)

func platformKind(kind string) bool {
	return kind == "bilibili"
}
func (m *Manager) RestorePlatform(kind string) error {
	if !platformKind(kind) || m.Platforms[kind] == nil || m.Dir == "" {
		return errors.New("账号连接尚未配置")
	}
	body, err := os.ReadFile(filepath.Join(m.Dir, kind+"-session.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("账号连接无法读取")
	}
	var session platform.Session
	if len(body) > 64<<10 || json.Unmarshal(body, &session) != nil {
		return errors.New("账号连接文件不完整")
	}
	return m.Platforms[kind].UseSession(session)
}

func (m *Manager) platformStatuses(ctx context.Context) map[string]Status {
	var wg sync.WaitGroup
	var lock sync.Mutex
	out := map[string]Status{}
	for _, kind := range []string{"bilibili"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status := Status{State: "not_connected"}
			client := m.Platforms[kind]
			if client != nil && client.Identity() != "" {
				m.mu.Lock()
				if m.platformStatus == nil {
					m.platformStatus = map[string]Status{}
					m.platformChecked = map[string]time.Time{}
				}
				status = m.platformStatus[kind]
				refresh := time.Since(m.platformChecked[kind]) > time.Minute
				if refresh {
					m.platformChecked[kind] = time.Now()
				}
				m.mu.Unlock()
				if refresh {
					profile, err := client.Account(ctx)
					status = Status{State: "connected", Name: profile.Name}
					if errors.Is(err, platform.ErrExpired) {
						status = Status{State: "needs_login"}
					} else if err != nil {
						status = Status{State: "unavailable"}
					}
					m.mu.Lock()
					m.platformStatus[kind] = status
					m.mu.Unlock()
				}
				if status.State == "" {
					status.State = "configured"
				}
			}
			lock.Lock()
			out[kind] = status
			lock.Unlock()
		}()
	}
	wg.Wait()
	return out
}

func (m *Manager) StartPlatform(ctx context.Context, kind string) (Attempt, error) {
	if !platformKind(kind) || m.Platforms[kind] == nil {
		return Attempt{}, errors.New("账号连接尚未配置")
	}
	pub, err := m.newAttempt(kind, "")
	if err != nil {
		return Attempt{}, err
	}
	a, err := m.get(pub.ID, kind)
	if err != nil {
		return Attempt{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.ctx, cancel)
	defer stop()
	defer cancel()
	login, err := m.Platforms[kind].Start(ctx)
	if err != nil {
		m.Cancel(a.ID)
		return Attempt{}, err
	}
	config, err := png.DecodeConfig(bytes.NewReader(login.Image))
	if err != nil || len(login.Image) > 512<<10 || config.Width < 32 || config.Height < 32 || config.Width > 1024 || config.Height > 1024 || login.Key == "" {
		m.Cancel(a.ID)
		return Attempt{}, errors.New("二维码未准备好，请重试")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !m.stillCurrent(a) {
		return Attempt{}, errors.New("连接已取消")
	}
	a.key, a.image = login.Key, login.Image
	return a.public(), nil
}
func (m *Manager) PlatformQR(kind, id string) ([]byte, error) {
	a, err := m.get(id, kind)
	if err != nil || !platformKind(kind) {
		return nil, errors.New("二维码已过期，请重新开始")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.image) == 0 || a.consumed {
		return nil, errors.New("二维码已结束，请重新开始")
	}
	return bytes.Clone(a.image), nil
}
func (m *Manager) CheckPlatform(ctx context.Context, kind, id string) (Attempt, error) {
	if !platformKind(kind) || m.Platforms[kind] == nil {
		return Attempt{}, errors.New("账号连接尚未配置")
	}
	a, err := m.get(id, kind)
	if err != nil {
		return Attempt{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.consumed || time.Since(a.checked) < 2*time.Second {
		return a.public(), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	stop := context.AfterFunc(a.ctx, cancel)
	defer stop()
	defer cancel()
	a.checked = time.Now()
	result, err := m.Platforms[kind].Poll(ctx, a.key)
	if !m.stillCurrent(a) {
		return Attempt{}, errors.New("连接已取消")
	}
	if err != nil {
		m.reportPlatformError(a, kind, "poll", err)
		if result.State == "failed" {
			a.State = "failed"
			a.Message = err.Error()
			a.consumed = true
			a.image = nil
			return a.public(), nil
		}
		return a.public(), err
	}
	if result.State != "confirmed" {
		switch result.State {
		case "waiting", "scanned":
			a.State = result.State
		case "expired", "failed":
			a.State = result.State
			a.consumed = true
			a.image = nil
		default:
			return a.public(), errors.New("登录状态暂时无法确认")
		}
		return a.public(), nil
	}
	a.consumed = true
	a.image = nil
	if result.Session == nil {
		a.State = "failed"
		a.Message = "登录信息不完整，请重新扫码"
		return a.public(), nil
	}
	return m.completePlatformSession(ctx, kind, a, *result.Session)
}

// Caller holds the attempt lock; promotion rechecks cancellation after network I/O.
func (m *Manager) completePlatformSession(ctx context.Context, kind string, a *Attempt, session platform.Session) (Attempt, error) {
	profile, err := m.Platforms[kind].Verify(ctx, session)
	if err != nil || ctx.Err() != nil || profile.ID == "" || profile.ID != session.ID {
		if err != nil {
			m.reportPlatformError(a, kind, "verify", err)
		}
		a.State = "failed"
		a.Message = "账号尚未核实，原连接没有改变"
		return a.public(), nil
	}
	if old := m.Platforms[kind].Identity(); old != "" && old != profile.ID {
		a.State = "failed"
		a.Message = "请使用当前连接的账号，避免误切到其它账号"
		return a.public(), nil
	}
	session.Name = profile.Name
	err = m.promote(a, func() error {
		if m.Dir == "" {
			return errors.New("账号连接无法保存")
		}
		body, err := json.Marshal(session)
		if err != nil {
			return err
		}
		if err = privatefile.Write(filepath.Join(m.Dir, kind+"-session.json"), body); err != nil {
			return err
		}
		if err = m.Platforms[kind].UseSession(session); err != nil {
			return err
		}
		if m.platformStatus == nil {
			m.platformStatus = map[string]Status{}
			m.platformChecked = map[string]time.Time{}
		}
		m.platformStatus[kind] = Status{State: "connected", Name: profile.Name}
		m.platformChecked[kind] = time.Now()
		return nil
	})
	if err != nil {
		a.State = "failed"
		a.Message = "连接没有保存，原连接保持不变"
		return a.public(), nil
	}
	a.State = "connected"
	return a.public(), nil
}

func (m *Manager) reportPlatformError(a *Attempt, kind, stage string, err error) {
	if m.Log == nil {
		return
	}
	code := 0
	var detail *platform.Failure
	if errors.As(err, &detail) {
		stage, code = detail.Stage, detail.Code
	}
	stamp := fmt.Sprintf("%s:%d", stage, code)
	if a.reported == stamp {
		return
	}
	a.reported = stamp
	detailText := ""
	if detail != nil {
		detailText = detail.Detail
	}
	m.Log.Warn("account connection step failed", "platform", kind, "stage", stage, "code", code, "detail", detailText)
}
