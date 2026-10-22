package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"time"

	"music/internal/platform"
)

type miguLogin interface {
	MiguSendCode(context.Context, string) error
	MiguLogin(context.Context, string, string) (platform.Session, error)
}

var miguPhone = regexp.MustCompile(`^1[3-9][0-9]{9}$`)
var miguCode = regexp.MustCompile(`^[0-9]{4,8}$`)

// Sending is explicit, never triggered by opening Settings or by polling.
// Reserve the cooldown before I/O: a timeout must not cause duplicate SMS.
func (m *Manager) StartMigu(ctx context.Context, phone string) (Attempt, error) {
	client, ok := m.Platforms["migu"].(miguLogin)
	if !ok {
		return Attempt{}, errors.New("咪咕连接尚未配置")
	}
	if !miguPhone.MatchString(phone) {
		return Attempt{}, errors.New("请输入正确的11位手机号")
	}
	sum := sha256.Sum256([]byte(phone))
	key := hex.EncodeToString(sum[:])
	m.mu.Lock()
	if m.miguSent == nil {
		m.miguSent = map[string]time.Time{}
	}
	now := time.Now()
	for k, t := range m.miguSent {
		if now.Sub(t) > 10*time.Minute {
			delete(m.miguSent, k)
		}
	}
	if len(m.miguSent) >= 10 || now.Sub(m.miguSent[key]) < time.Minute {
		m.mu.Unlock()
		return Attempt{}, errors.New("请稍等一分钟再发送验证码")
	}
	m.miguSent[key] = now
	m.mu.Unlock()
	pub, err := m.newAttempt("migu", "")
	if err != nil {
		return Attempt{}, err
	}
	a, err := m.get(pub.ID, "migu")
	if err != nil {
		return Attempt{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	stop := context.AfterFunc(a.ctx, cancel)
	defer stop()
	defer cancel()
	if err := client.MiguSendCode(requestCtx, phone); err != nil {
		m.reportPlatformError(a, "migu", "sms.send", err)
		m.Cancel(a.ID)
		return Attempt{}, err
	}
	if !m.stillCurrent(a) {
		return Attempt{}, ErrAttemptGone
	}
	a.phone = phone
	a.Message = "验证码已发送，请在下方输入"
	return a.public(), nil
}

func (m *Manager) CompleteMigu(ctx context.Context, id, code string) (Attempt, error) {
	client, ok := m.Platforms["migu"].(miguLogin)
	if !ok {
		return Attempt{}, errors.New("咪咕连接尚未配置")
	}
	if !miguCode.MatchString(code) {
		return Attempt{}, errors.New("请输入短信中的数字验证码")
	}
	a, err := m.get(id, "migu")
	if err != nil {
		return Attempt{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.consumed || a.phone == "" {
		return Attempt{}, ErrAttemptGone
	}
	if a.tries >= 5 {
		a.consumed = true
		a.phone = ""
		return Attempt{}, errors.New("验证次数已用完，请重新开始")
	}
	a.tries++
	requestCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	stop := context.AfterFunc(a.ctx, cancel)
	defer stop()
	defer cancel()
	session, err := client.MiguLogin(requestCtx, a.phone, code)
	if !m.stillCurrent(a) {
		return Attempt{}, ErrAttemptGone
	}
	if err != nil {
		m.reportPlatformError(a, "migu", "sms.confirm", err)
		return a.public(), err
	}
	a.consumed = true
	a.phone = ""
	return m.completePlatformSession(requestCtx, "migu", a, session)
}
