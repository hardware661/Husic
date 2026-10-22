package platform

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"music/internal/source"
)

var qqMID = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)

func (c *Client) Recommend(ctx context.Context, limit int) ([]source.Track, error) {
	if c.kind == "kugou" {
		if c.Identity() != "" {
			if tracks, e := c.kugouRecommendations(ctx); e == nil && len(tracks) > 0 {
				return tracks[:min(limit, len(tracks))], nil
			}
		}
		tracks, _, err := c.publicKugou.Chart(ctx, "31308", max(limit, 60))
		return tracks, err
	}
	if c.kind == "migu" {
		if c.Identity() != "" {
			if tracks, e := c.miguRecommendations(ctx); e == nil && len(tracks) > 0 {
				return tracks[:min(limit, len(tracks))], nil
			}
		}
		return c.publicMigu.Recommend(ctx, limit)
	}
	if c.kind != "qq" {
		return nil, source.ErrUnavailable
	}
	if c.Identity() != "" {
		if tracks, err := c.Recommendations(ctx); err == nil && len(tracks) > 0 {
			return tracks[:min(limit, len(tracks))], nil
		}
	}
	tracks, _, err := c.publicQQ.Chart(ctx, "27", limit)
	return tracks, err
}

func (c *Client) Chart(ctx context.Context, id string, limit int) ([]source.Track, string, error) {
	if c.kind == "kugou" {
		return c.publicKugou.Chart(ctx, id, limit)
	}
	if c.kind == "migu" {
		return nil, "", source.ErrUnavailable
	}
	if c.kind != "qq" {
		return nil, "", source.ErrUnavailable
	}
	return c.publicQQ.Chart(ctx, id, limit)
}

func (c *Client) Search(ctx context.Context, q string, limit int) ([]source.Track, error) {
	if c.kind == "kugou" {
		return c.publicKugou.Search(ctx, q, limit)
	}
	if c.kind == "migu" {
		return c.publicMigu.Search(ctx, q, limit)
	}
	if c.kind == "bilibili" {
		return c.biliSearch(ctx, q, limit)
	}
	return c.publicQQ.Search(ctx, q, limit)
}
func (c *Client) Stream(ctx context.Context, t source.Track) (source.Stream, error) {
	if c.kind == "kugou" {
		if c.Identity() != "" {
			if stream, e := c.kugouStream(ctx, t); e == nil {
				return stream, nil
			}
		}
		return c.publicKugou.Stream(ctx, t)
	}
	if c.kind == "migu" {
		if c.Identity() != "" {
			if stream, e := c.miguStream(ctx, t); e == nil {
				return stream, nil
			}
		}
		return c.publicMigu.Stream(ctx, t)
	}
	if c.kind == "bilibili" {
		return c.biliStream(ctx, t)
	}
	s := c.sessionCopy()
	if s.ID != "" {
		if stream, err := c.qqStream(ctx, t, s); err == nil {
			return stream, nil
		}
	}
	return c.publicQQ.Stream(ctx, t)
}
func (c *Client) qqStream(ctx context.Context, t source.Track, s Session) (source.Stream, error) {
	if !qqMID.MatchString(t.ID) {
		return source.Stream{}, source.ErrUnavailable
	}
	mediaMID := t.Extra["media_mid"]
	seconds := t.Duration
	if !qqMID.MatchString(mediaMID) || seconds == 0 {
		d, err := c.qqCall(ctx, "music.pf_song_detail_svr", "get_song_detail_yqq", map[string]any{"song_mid": t.ID, "song_type": 0}, &s)
		if err != nil {
			return source.Stream{}, err
		}
		song := object(d["track_info"])
		if text(song["mid"]) != t.ID {
			return source.Stream{}, ErrResponse
		}
		mediaMID = text(object(song["file"])["media_mid"])
		seconds = integer(song["interval"])
	}
	if !qqMID.MatchString(mediaMID) || seconds <= 0 {
		return source.Stream{}, source.ErrUnavailable
	}
	for _, quality := range []struct {
		prefix  string
		bitrate int
	}{{"M800", 320000}, {"M500", 128000}} {
		d, err := c.qqCall(ctx, "vkey.GetVkeyServer", "CgiGetVkey", map[string]any{
			"guid": "1000000000", "songmid": []string{t.ID}, "songtype": []int{0}, "uin": s.ID, "loginflag": 1, "platform": "20", "filename": []string{quality.prefix + mediaMID + ".mp3"},
		}, &s)
		if err != nil {
			continue
		}
		infos := array(d["midurlinfo"])
		if len(infos) != 1 {
			continue
		}
		info := object(infos[0])
		purl := text(info["purl"])
		if purl == "" || strings.Contains(purl, ":") || strings.HasPrefix(purl, "//") || strings.Contains(purl, "..") || text(info["songmid"]) != "" && text(info["songmid"]) != t.ID {
			continue
		}
		base := "https://ws.stream.qqmusic.qq.com/"
		for _, v := range array(d["sip"]) {
			u, err := url.Parse(text(v))
			if err == nil && u.User == nil && (u.Scheme == "https" || u.Scheme == "http") && strings.HasSuffix(u.Hostname(), ".qqmusic.qq.com") {
				u.Scheme = "https"
				base = u.String()
				break
			}
		}
		u, err := url.Parse(base)
		if err != nil {
			continue
		}
		relative, err := url.Parse(purl)
		if err != nil {
			continue
		}
		return source.Stream{URL: u.ResolveReference(relative).String(), Bitrate: quality.bitrate, Format: "mp3", Seconds: seconds}, nil
	}
	return source.Stream{}, source.ErrUnavailable
}
func qqTrack(row map[string]any) (source.Track, bool) {
	id := text(row["mid"])
	if id == "" {
		id = text(row["songmid"])
	}
	title := text(row["name"])
	if title == "" {
		title = text(row["title"])
	}
	if title == "" {
		title = text(row["songname"])
	}
	if !qqMID.MatchString(id) || title == "" {
		return source.Track{}, false
	}
	t := source.Track{Source: "qq", ID: id, Title: title, Duration: integer(row["interval"]), Artists: []string{}, Extra: map[string]string{}}
	for _, v := range array(row["singer"]) {
		if name := text(object(v)["name"]); name != "" {
			t.Artists = append(t.Artists, name)
		}
	}
	album := object(row["album"])
	t.Album = text(album["name"])
	mid := text(album["mid"])
	if mid == "" {
		mid = text(row["albummid"])
		t.Album = text(row["albumname"])
	}
	if qqMID.MatchString(mid) {
		t.Cover = "https://y.gtimg.cn/music/photo_new/T002R500x500M000" + mid + ".jpg"
	}
	if media := text(object(row["file"])["media_mid"]); qqMID.MatchString(media) {
		t.Extra["media_mid"] = media
	}
	return t, len(t.Artists) > 0
}

func (c *Client) Recommendations(ctx context.Context) ([]source.Track, error) {
	if c.kind == "kugou" {
		return c.kugouRecommendations(ctx)
	}
	if c.kind == "migu" {
		return c.miguRecommendations(ctx)
	}
	if c.kind != "qq" {
		return []source.Track{}, nil
	}
	s := c.sessionCopy()
	if s.ID == "" {
		return nil, ErrNotConnected
	}
	d, err := c.qqCall(ctx, "music.radioProxy.MbTrackRadioSvr", "get_radio_track", map[string]any{"id": 99, "num": 20, "from": 0, "scene": 0, "song_ids": []int{}}, &s)
	if err != nil {
		return nil, err
	}
	if d["tracks"] == nil {
		return nil, ErrResponse
	}
	tracks := []source.Track{}
	for _, v := range array(d["tracks"]) {
		if t, ok := qqTrack(object(v)); ok {
			tracks = append(tracks, t)
		}
	}
	return tracks, nil
}

// Bilibili collection identities are BV plus an explicit, one-based part.
func biliCollectionTrack(row map[string]any) (source.Track, bool) {
	id := text(row["bvid"])
	if id == "" {
		id = text(row["bv_id"])
	}
	if !bvidPattern.MatchString(id) || integer(row["type"]) != 2 {
		return source.Track{}, false
	}
	return source.Track{Source: "bilibili", ID: id, Title: text(row["title"]), Artists: []string{}, Cover: imageURL(row["cover"]), Duration: 0, Extra: map[string]string{"inspect": "parts", "parts": strconv.Itoa(integer(row["page"]))}}, true
}
