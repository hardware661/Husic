package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
	"music/internal/source"
)

var bvidPattern = regexp.MustCompile(`^BV[0-9A-Za-z]{10}$`)
var htmlTag = regexp.MustCompile(`<[^>]*>`)

type Video struct {
	ID    string         `json:"id"`
	Title string         `json:"title"`
	Cover string         `json:"cover"`
	Parts []source.Track `json:"parts"`
}

func (c *Client) biliCall(ctx context.Context, host, path string, q url.Values, cookies map[string]string) (map[string]any, *http.Response, error) {
	if host == "api.bilibili.com" {
		var err error
		cookies, err = c.biliCookies(ctx, cookies)
		if err != nil {
			return nil, nil, err
		}
	}
	r, raw, err := c.request(ctx, host, path, q, nil, nil, cookies)
	if err != nil {
		return nil, nil, err
	}
	d, err := decode(raw)
	if err != nil || d["code"] == nil {
		return nil, nil, ErrResponse
	}
	if code := integer(d["code"]); code != 0 {
		if code == -101 {
			return nil, nil, ErrExpired
		}
		return nil, nil, errors.New("B 站暂未允许访问，请稍后重试")
	}
	data := object(d["data"])
	if data == nil {
		return nil, nil, ErrResponse
	}
	return data, r, nil
}
func (c *Client) biliStart(ctx context.Context) (Login, error) {
	d, _, err := c.biliCall(ctx, "passport.bilibili.com", "/x/passport-login/web/qrcode/generate", nil, nil)
	if err != nil {
		return Login{}, err
	}
	key, link := text(d["qrcode_key"]), text(d["url"])
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.Host != "passport.bilibili.com" && u.Host != "account.bilibili.com" || u.User != nil || key == "" || len(key) > 512 {
		return Login{}, ErrResponse
	}
	image, err := qrcode.Encode(link, qrcode.Medium, 320)
	return Login{Key: key, Image: image}, err
}
func (c *Client) biliPoll(ctx context.Context, key string) (LoginResult, error) {
	if len(key) > 512 || !validCookie(key) {
		return LoginResult{}, ErrResponse
	}
	d, r, err := c.biliCall(ctx, "passport.bilibili.com", "/x/passport-login/web/qrcode/poll", url.Values{"qrcode_key": {key}}, nil)
	if err != nil {
		return LoginResult{}, err
	}
	if d["code"] == nil {
		return LoginResult{}, ErrResponse
	}
	switch integer(d["code"]) {
	case 86101:
		return LoginResult{State: "waiting"}, nil
	case 86090:
		return LoginResult{State: "scanned"}, nil
	case 86038:
		return LoginResult{State: "expired"}, nil
	case 0:
		s := Session{Cookies: map[string]string{}}
		for _, cookie := range r.Cookies() {
			domain := strings.TrimPrefix(strings.ToLower(cookie.Domain), ".")
			if domain != "bilibili.com" || cookie.Path != "/" && cookie.Path != "" || cookie.MaxAge < 0 || !cookie.Expires.IsZero() && time.Now().After(cookie.Expires) {
				continue
			}
			switch cookie.Name {
			case "SESSDATA", "bili_jct", "DedeUserID", "DedeUserID__ckMd5", "buvid3", "buvid4":
				if !validCookie(cookie.Value) {
					return LoginResult{}, ErrResponse
				}
				s.Cookies[cookie.Name] = cookie.Value
			}
		}
		s.ID = s.Cookies["DedeUserID"]
		if err = c.validate(s); err != nil {
			return LoginResult{}, err
		}
		return LoginResult{State: "confirmed", Session: &s}, nil
	default:
		return LoginResult{}, errors.New("B 站未接受这次登录，请重新扫码")
	}
}
func (c *Client) biliVerify(ctx context.Context, s Session) (Profile, error) {
	d, _, err := c.biliCall(ctx, "api.bilibili.com", "/x/web-interface/nav", nil, s.Cookies)
	if err != nil {
		return Profile{}, err
	}
	if d["isLogin"] != true || text(d["mid"]) != s.ID {
		return Profile{}, ErrExpired
	}
	return Profile{ID: s.ID, Name: text(d["uname"])}, nil
}

func VideoID(input string) (string, bool) {
	if bvidPattern.MatchString(input) {
		return input, true
	}
	u, err := url.Parse(input)
	if err != nil || u.Scheme != "https" && u.Scheme != "http" || u.User != nil || u.Host != "www.bilibili.com" && u.Host != "m.bilibili.com" && u.Host != "bilibili.com" {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "video" || !bvidPattern.MatchString(parts[1]) {
		return "", false
	}
	return parts[1], true
}

func (c *Client) Video(ctx context.Context, id string) (Video, error) {
	if !bvidPattern.MatchString(id) {
		return Video{}, errors.New("请填写 B 站视频链接或 BV 号")
	}
	session := c.sessionCopy()
	d, _, err := c.biliCall(ctx, "api.bilibili.com", "/x/web-interface/view", url.Values{"bvid": {id}}, session.Cookies)
	if err != nil {
		// One bounded same-BV public-page fallback, carried over from the old
		// player. Parse the JSON value only; never evaluate page JavaScript.
		_, raw, pageErr := c.request(ctx, "www.bilibili.com", "/video/"+id+"/", nil, nil, nil, session.Cookies)
		if pageErr != nil {
			return Video{}, err
		}
		marker := []byte("window.__INITIAL_STATE__=")
		start := bytes.Index(raw, marker)
		if start < 0 {
			marker = []byte("window.__INITIAL_STATE__ = ")
			start = bytes.Index(raw, marker)
		}
		if start < 0 {
			return Video{}, err
		}
		var state map[string]any
		decoder := json.NewDecoder(bytes.NewReader(raw[start+len(marker):]))
		decoder.UseNumber()
		if decoder.Decode(&state) != nil {
			return Video{}, err
		}
		d = object(state["videoData"])
	}
	if text(d["bvid"]) != id || integer(d["state"]) < 0 || d["is_upower_exclusive"] == true {
		return Video{}, source.ErrUnavailable
	}
	pages := array(d["pages"])
	count := integer(d["videos"])
	if len(pages) == 0 || len(pages) > 999 || count != len(pages) {
		return Video{}, ErrResponse
	}
	video := Video{ID: id, Title: html.UnescapeString(text(d["title"])), Cover: imageURL(d["pic"]), Parts: make([]source.Track, 0, len(pages))}
	for i, v := range pages {
		page := object(v)
		if integer(page["page"]) != i+1 || integer(page["duration"]) <= 0 || !numericID.MatchString(text(page["cid"])) {
			return Video{}, ErrResponse
		}
		title := html.UnescapeString(text(page["part"]))
		if len(pages) == 1 || title == "" {
			title = video.Title
		}
		video.Parts = append(video.Parts, source.Track{Source: "bilibili", ID: id + ":" + strconv.Itoa(i+1), Title: title, Duration: integer(page["duration"]), Cover: video.Cover, Artists: []string{}, Pinned: true,
			Extra: map[string]string{"bvid": id, "part": strconv.Itoa(i + 1), "cid": text(page["cid"]), "video_title": video.Title}})
	}
	return video, nil
}

func (c *Client) biliSearch(ctx context.Context, query string, limit int) ([]source.Track, error) {
	if id, ok := VideoID(strings.TrimSpace(query)); ok {
		v, err := c.Video(ctx, id)
		if err != nil {
			return nil, err
		}
		return v.Parts, nil
	}
	s := c.sessionCopy()
	d, err := c.biliWBI(ctx, "/x/web-interface/wbi/search/type", url.Values{"search_type": {"video"}, "keyword": {query}, "page": {"1"}, "page_size": {strconv.Itoa(min(max(limit, 1), 20))}}, s.Cookies)
	if err != nil {
		return nil, err
	}
	tracks := make([]source.Track, 0, limit)
	for _, v := range array(d["result"]) {
		row := object(v)
		id := text(row["bvid"])
		if !bvidPattern.MatchString(id) {
			continue
		}
		tracks = append(tracks, source.Track{Source: "bilibili", ID: id, Title: html.UnescapeString(htmlTag.ReplaceAllString(text(row["title"]), "")), Cover: imageURL(row["pic"]), Artists: []string{}, Extra: map[string]string{"inspect": "parts"}})
		if len(tracks) >= limit {
			break
		}
	}
	return tracks, nil
}

func (c *Client) biliStream(ctx context.Context, t source.Track) (source.Stream, error) {
	parts := strings.Split(t.ID, ":")
	id := parts[0]
	video, err := c.Video(ctx, id)
	if err != nil {
		return source.Stream{}, err
	}
	part := 1
	if len(parts) == 2 {
		part, _ = strconv.Atoi(parts[1])
	} else if len(parts) != 1 || len(video.Parts) != 1 {
		return source.Stream{}, errors.New("请打开视频，选择要听的分P")
	}
	if part < 1 || part > len(video.Parts) {
		return source.Stream{}, source.ErrUnavailable
	}
	selected := video.Parts[part-1]
	s := c.sessionCopy()
	d, _, err := c.biliCall(ctx, "api.bilibili.com", "/x/player/playurl", url.Values{"bvid": {id}, "cid": {selected.Extra["cid"]}, "qn": {"64"}, "fnval": {"16"}, "fnver": {"0"}, "fourk": {"0"}}, s.Cookies)
	if err != nil {
		return source.Stream{}, err
	}
	seconds := integer(d["timelength"]) / 1000
	if seconds < selected.Duration-6 || seconds > selected.Duration+6 || d["is_preview"] == true || d["is_preview"] == json.Number("1") {
		return source.Stream{}, source.ErrUnavailable
	}
	var chosen map[string]any
	for _, v := range array(object(d["dash"])["audio"]) {
		audio := object(v)
		if !strings.HasPrefix(text(audio["codecs"]), "mp4a") || text(audio["mimeType"]) != "audio/mp4" && text(audio["mime_type"]) != "audio/mp4" {
			continue
		}
		if chosen == nil || integer(audio["bandwidth"]) > integer(chosen["bandwidth"]) {
			chosen = audio
		}
	}
	if chosen == nil {
		return source.Stream{}, source.ErrUnavailable
	}
	link := text(chosen["baseUrl"])
	if link == "" {
		link = text(chosen["base_url"])
	}
	proxy, err := c.registerMedia(link)
	if err != nil {
		return source.Stream{}, err
	}
	return source.Stream{URL: proxy, Bitrate: integer(chosen["bandwidth"]), Format: "m4a", Seconds: seconds}, nil
}
