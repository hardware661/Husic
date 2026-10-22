package platform

import (
	"context"
	"time"
)

// Bilibili gives each ordinary web client an anonymous guest identifier.
// Obtain it from the platform, never synthesize it or borrow browser data.
func (c *Client) biliCookies(ctx context.Context, session map[string]string) (map[string]string, error) {
	if session["buvid3"] != "" {
		return cloneCookies(session), nil
	}
	c.mu.RLock()
	guest, until := cloneCookies(c.guestCookies), c.guestUntil
	c.mu.RUnlock()
	if guest["buvid3"] == "" || time.Now().After(until) {
		_, raw, err := c.request(ctx, "api.bilibili.com", "/x/frontend/finger/spi", nil, nil, nil, nil)
		if err != nil {
			return nil, err
		}
		d, err := decode(raw)
		if err != nil || d["code"] == nil || integer(d["code"]) != 0 {
			return nil, ErrResponse
		}
		data := object(d["data"])
		if !validCookie(text(data["b_3"])) {
			return nil, ErrResponse
		}
		guest = map[string]string{"buvid3": text(data["b_3"])}
		if v := text(data["b_4"]); validCookie(v) {
			guest["buvid4"] = v
		}
		c.mu.Lock()
		c.guestCookies, c.guestUntil = cloneCookies(guest), time.Now().Add(12*time.Hour)
		c.mu.Unlock()
	}
	for k, v := range session {
		guest[k] = v
	}
	return guest, nil
}
