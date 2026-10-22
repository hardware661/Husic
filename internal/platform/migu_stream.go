package platform

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"music/internal/source"
)

// Migu's web listen endpoint wraps its JSON in a versioned binary envelope.
// This decodes that response only; it does not alter the server's rights,
// requested quality, returned path, expiry, signature, or preview status.
func miguDecodeListen(raw []byte) (map[string]any, error) {
	if len(raw) >= 4 && raw[0] == 171 && raw[1] == 205 {
		if raw[2] != 1 {
			return nil, ErrResponse
		}
		const key = "Jk8qzuePiJ1qE3mDYhLQ3T73DtDoAhLP"
		plain := make([]byte, len(raw)-4)
		for i, b := range raw[4:] {
			plain[i] = b + raw[3] - key[i%len(key)]
		}
		raw = plain
	}
	return miguBody(raw, "migu.stream")
}

func miguStreamResult(d map[string]any, t source.Track) (source.Stream, error) {
	data := object(d["data"])
	song := object(data["song"])
	if text(song["contentId"]) != t.ID || text(data["freeListenType"]) != "0" {
		return source.Stream{}, source.ErrUnavailable
	}
	if code := text(data["cannotCode"]); code != "" && code != "0" {
		return source.Stream{}, source.ErrUnavailable
	}
	seconds := integer(song["duration"])
	if seconds <= 0 || t.Duration > 0 && seconds+6 < t.Duration {
		return source.Stream{}, source.ErrUnavailable
	}
	bitrate := 0
	switch text(data["audioFormatType"]) {
	case "PQ":
		bitrate = 128000
	case "HQ":
		bitrate = 320000
	default:
		return source.Stream{}, source.ErrUnavailable
	}
	u, err := url.Parse(text(data["url"]))
	if err != nil || u.User != nil || u.Fragment != "" || u.Path == "" || u.Scheme != "https" && u.Scheme != "http" {
		return source.Stream{}, source.ErrUnavailable
	}
	host := strings.ToLower(u.Hostname())
	if host != "migu.cn" && !strings.HasSuffix(host, ".migu.cn") || u.Port() != "" && u.Port() != "443" && u.Port() != "80" {
		return source.Stream{}, source.ErrUnavailable
	}
	if u.Scheme == "http" {
		u.Scheme = "https"
		if u.Port() == "80" {
			u.Host = host
		}
	}
	return source.Stream{URL: u.String(), Bitrate: bitrate, Format: "mp3", Seconds: seconds}, nil
}

func (c *Client) miguStream(ctx context.Context, t source.Track) (source.Stream, error) {
	s := c.sessionCopy()
	if s.ID == "" {
		return source.Stream{}, ErrNotConnected
	}
	if err := MiguValidateSession(s); err != nil {
		return source.Stream{}, err
	}
	if t.Source != "migu" || !miguContentID.MatchString(t.ID) {
		return source.Stream{}, source.ErrUnavailable
	}
	query := url.Values{"contentId": {t.ID}, "resourceType": {"2"}, "toneFlag": {"PQ"}, "scene": {""}, "netType": {"01"}}
	if copyright := t.Extra["copyrightId"]; copyright != "" {
		if !miguAccountID.MatchString(copyright) {
			return source.Stream{}, source.ErrUnavailable
		}
		query.Set("copyrightId", copyright)
	}
	_, raw, err := c.miguRequest(ctx, "app.c.nf.migu.cn", "/strategy/pc/listen/v2.0", query, nil, map[string]string{
		"subchannel": "014X031", "pacmtoken": s.Cookies["pacmtoken"], "deviceId": "music-personal-web",
		"ua": "Android_migu", "version": "6.8.8", "activityId": "MUSIC-WWW", "birth": "h5page",
		"signature": "1", "timestamp": strconv.FormatInt(time.Now().UnixMilli(), 10),
	}, nil)
	if err != nil {
		return source.Stream{}, err
	}
	d, err := miguDecodeListen(raw)
	if err != nil {
		return source.Stream{}, err
	}
	// The audio URL is returned directly. The Huawei transport never fetches
	// media bytes, and the account credential is never attached to that URL.
	return miguStreamResult(d, t)
}
