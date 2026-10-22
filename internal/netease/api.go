package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type Artist struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Album struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	PicURL string `json:"picUrl"`
}

type Song struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Artists  []Artist `json:"ar"`
	Album    Album    `json:"al"`
	Duration int64    `json:"dt"` // milliseconds
	Fee      int      `json:"fee"`
}

// Privilege is what this caller (guest or account, from this network) may do with a song.
type Privilege struct {
	ID         int64  `json:"id"`
	Fee        int    `json:"fee"`   // 0 free, 1 VIP, 4 paid album, 8 free at low quality
	St         int    `json:"st"`    // < 0: unavailable (no copyright / region)
	Pl         int    `json:"pl"`    // max playable bitrate, 0 = cannot play
	MaxBr      int    `json:"maxbr"` // best bitrate that exists
	PlLevel    string `json:"plLevel"`
	MaxBrLevel string `json:"maxBrLevel"`
}

type SongURL struct {
	ID            int64           `json:"id"`
	URL           string          `json:"url"`
	Br            int             `json:"br"`
	Size          int64           `json:"size"`
	Type          string          `json:"type"`
	Level         string          `json:"level"`
	Code          int             `json:"code"`
	Fee           int             `json:"fee"`
	Time          int64           `json:"time"` // milliseconds of audio actually served
	Expi          int             `json:"expi"` // seconds the URL stays valid
	FreeTrialInfo json.RawMessage `json:"freeTrialInfo"`
}

// Trial reports whether NetEase only granted a preview clip.
func (u SongURL) Trial() bool {
	s := strings.TrimSpace(string(u.FreeTrialInfo))
	return s != "" && s != "null"
}

type Playlist struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	TrackCount  int    `json:"trackCount"`
	SpecialType int    `json:"specialType"` // 5 = the user's liked songs
	Subscribed  bool   `json:"subscribed"`
	Creator     struct {
		UserID int64 `json:"userId"`
	} `json:"creator"`
}

type Account struct {
	UserID   int64
	Nickname string
	VipType  int
}

// QR login states returned by QRCheck.
const (
	QRExpired   = 800
	QRWaiting   = 801
	QRScanned   = 802
	QRConfirmed = 803
)

func (c *Client) QRKey(ctx context.Context) (string, error) {
	var out struct {
		Unikey string `json:"unikey"`
	}
	r, err := c.loginClient().Weapi(ctx, "/login/qrcode/unikey", map[string]any{"type": 1}, &out)
	if err == nil {
		err = r.err("/login/qrcode/unikey")
	}
	return out.Unikey, err
}

// QRLoginURL is what the QR code must contain for the app to scan.
func QRLoginURL(key string) string { return "https://music.163.com/login?codekey=" + key }

// QRCheck polls a login; on QRConfirmed the cookies carry MUSIC_U and __csrf.
func (c *Client) QRCheck(ctx context.Context, key string) (int, string, []*http.Cookie, error) {
	r, err := c.loginClient().Weapi(ctx, "/login/qrcode/client/login", map[string]any{"key": key, "type": 1}, nil)
	return r.Code, r.Message, r.Cookies, err
}

func (c *Client) Account(ctx context.Context) (Account, error) {
	var out struct {
		Account *struct {
			ID      int64 `json:"id"`
			VipType int   `json:"vipType"`
		} `json:"account"`
		Profile *struct {
			UserID   int64  `json:"userId"`
			Nickname string `json:"nickname"`
			VipType  int    `json:"vipType"`
		} `json:"profile"`
	}
	r, err := c.Weapi(ctx, "/w/nuser/account/get", map[string]any{}, &out)
	if err == nil {
		err = r.err("/w/nuser/account/get")
	}
	if err != nil {
		return Account{}, err
	}
	if out.Account == nil || out.Profile == nil {
		return Account{}, &Error{Path: "/w/nuser/account/get", Code: 301, Message: "not logged in"}
	}
	return Account{UserID: out.Profile.UserID, Nickname: out.Profile.Nickname, VipType: max(out.Account.VipType, out.Profile.VipType)}, nil
}

func (c *Client) UserPlaylists(ctx context.Context, uid int64) ([]Playlist, error) {
	var out struct {
		Playlist []Playlist `json:"playlist"`
	}
	r, err := c.Weapi(ctx, "/user/playlist", map[string]any{"uid": uid, "limit": 1000, "offset": 0, "includeVideo": true}, &out)
	if err == nil {
		err = r.err("/user/playlist")
	}
	return out.Playlist, err
}

// PlaylistTrackIDs returns every track ID of a playlist (charts are playlists too).
func (c *Client) PlaylistTrackIDs(ctx context.Context, id int64) (string, []int64, error) {
	var out struct {
		Playlist struct {
			Name     string `json:"name"`
			TrackIDs []struct {
				ID int64 `json:"id"`
			} `json:"trackIds"`
		} `json:"playlist"`
	}
	r, err := c.Weapi(ctx, "/v6/playlist/detail", map[string]any{"id": id, "n": 100000, "s": 8}, &out)
	if err == nil {
		err = r.err("/v6/playlist/detail")
	}
	ids := make([]int64, 0, len(out.Playlist.TrackIDs))
	for _, t := range out.Playlist.TrackIDs {
		ids = append(ids, t.ID)
	}
	return out.Playlist.Name, ids, err
}

// SongDetails looks up at most 500 songs at once.
func (c *Client) SongDetails(ctx context.Context, ids []int64) ([]Song, []Privilege, error) {
	refs := make([]string, len(ids))
	for i, id := range ids {
		refs[i] = fmt.Sprintf(`{"id":%d}`, id)
	}
	var out struct {
		Songs      []Song      `json:"songs"`
		Privileges []Privilege `json:"privileges"`
	}
	r, err := c.Weapi(ctx, "/v3/song/detail", map[string]any{"c": "[" + strings.Join(refs, ",") + "]"}, &out)
	if err == nil {
		err = r.err("/v3/song/detail")
	}
	return out.Songs, out.Privileges, err
}

// SongURLs asks for playable addresses. level is standard, higher, exhigh,
// lossless or hires; NetEase silently caps it at what this caller may play.
func (c *Client) SongURLs(ctx context.Context, ids []int64, level string) ([]SongURL, error) {
	list := make([]string, len(ids))
	for i, id := range ids {
		list[i] = strconv.FormatInt(id, 10)
	}
	var out struct {
		Data []SongURL `json:"data"`
	}
	r, err := c.Eapi(ctx, "/song/enhance/player/url/v1", map[string]any{"ids": "[" + strings.Join(list, ",") + "]", "level": level, "encodeType": "flac"}, &out)
	if err == nil {
		err = r.err("/song/enhance/player/url/v1")
	}
	return out.Data, err
}

// DailySongs is the account's own "每日推荐".
func (c *Client) DailySongs(ctx context.Context) ([]Song, error) {
	if !c.HasSession() {
		songs, err := c.NewReleases(ctx, 7)
		return songs[:min(30, len(songs))], err
	}
	var out struct {
		Data struct {
			DailySongs []Song `json:"dailySongs"`
		} `json:"data"`
	}
	r, err := c.Weapi(ctx, "/v3/discovery/recommend/songs", map[string]any{}, &out)
	if err == nil {
		err = r.err("/v3/discovery/recommend/songs")
	}
	return out.Data.DailySongs, err
}

// Search finds songs; each carries this caller's privilege.
func (c *Client) Search(ctx context.Context, keyword string, limit, offset int) ([]Song, []Privilege, error) {
	var out struct {
		Result struct {
			Songs []struct {
				Song
				Privilege Privilege `json:"privilege"`
			} `json:"songs"`
		} `json:"result"`
	}
	r, err := c.Weapi(ctx, "/cloudsearch/get/web", map[string]any{"s": keyword, "type": 1, "limit": limit, "offset": offset, "total": true}, &out)
	if err == nil {
		err = r.err("/cloudsearch/get/web")
	}
	songs := make([]Song, 0, len(out.Result.Songs))
	privileges := make([]Privilege, 0, len(out.Result.Songs))
	for _, s := range out.Result.Songs {
		songs = append(songs, s.Song)
		privileges = append(privileges, s.Privilege)
	}
	return songs, privileges, err
}

// Lyrics holds LRC text, its translation and word-timed YRC when NetEase has it.
type Lyrics struct {
	LRC         string
	Translation string
	Words       string
}

func (c *Client) Lyrics(ctx context.Context, id int64) (Lyrics, error) {
	var out struct {
		Lrc    struct{ Lyric string } `json:"lrc"`
		Tlyric struct{ Lyric string } `json:"tlyric"`
		Yrc    struct{ Lyric string } `json:"yrc"`
	}
	r, err := c.Eapi(ctx, "/song/lyric/v1", map[string]any{"id": id, "cp": false, "tv": 0, "lv": 0, "rv": 0, "kv": 0, "yv": 0, "ytv": 0, "yrv": 0}, &out)
	if err == nil {
		err = r.err("/song/lyric/v1")
	}
	return Lyrics{LRC: out.Lrc.Lyric, Translation: out.Tlyric.Lyric, Words: out.Yrc.Lyric}, err
}

// PlaylistInfo is a playlist card: NetEase's recommendations, charts, search.
type PlaylistInfo struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Cover      string `json:"coverImgUrl"`
	Picture    string `json:"picUrl"`
	TrackCount int    `json:"trackCount"`
	Copywriter string `json:"copywriter"`
}

func (p PlaylistInfo) CoverURL() string {
	if p.Cover != "" {
		return p.Cover
	}
	return p.Picture
}

// RecommendedPlaylists is the account's daily playlist picks.
func (c *Client) RecommendedPlaylists(ctx context.Context) ([]PlaylistInfo, error) {
	if !c.HasSession() {
		var out struct {
			Result []PlaylistInfo `json:"result"`
		}
		r, err := c.Weapi(ctx, "/personalized/playlist", map[string]any{"limit": 12, "total": true}, &out)
		if err == nil {
			err = r.err("/personalized/playlist")
		}
		return out.Result, err
	}
	var out struct {
		Recommend []PlaylistInfo `json:"recommend"`
	}
	r, err := c.Weapi(ctx, "/v1/discovery/recommend/resource", map[string]any{}, &out)
	if err == nil {
		err = r.err("/v1/discovery/recommend/resource")
	}
	return out.Recommend, err
}

// PersonalFM returns the next few songs of the account's private radio.
func (c *Client) PersonalFM(ctx context.Context) ([]Song, error) {
	var out struct {
		Data []struct {
			ID       int64    `json:"id"`
			Name     string   `json:"name"`
			Artists  []Artist `json:"artists"`
			Album    Album    `json:"album"`
			Duration int64    `json:"duration"`
		} `json:"data"`
	}
	r, err := c.Weapi(ctx, "/v1/radio/get", map[string]any{}, &out)
	if err == nil {
		err = r.err("/v1/radio/get")
	}
	songs := make([]Song, 0, len(out.Data))
	for _, s := range out.Data {
		songs = append(songs, Song{ID: s.ID, Name: s.Name, Artists: s.Artists, Album: s.Album, Duration: s.Duration})
	}
	return songs, err
}

// PlaylistDetail returns a playlist's card and its first tracks with details.
func (c *Client) PlaylistDetail(ctx context.Context, id int64) (PlaylistInfo, []int64, error) {
	var out struct {
		Playlist struct {
			PlaylistInfo
			Description string `json:"description"`
			TrackIDs    []struct {
				ID int64 `json:"id"`
			} `json:"trackIds"`
		} `json:"playlist"`
	}
	r, err := c.Weapi(ctx, "/v6/playlist/detail", map[string]any{"id": id, "n": 100000, "s": 8}, &out)
	if err == nil {
		err = r.err("/v6/playlist/detail")
	}
	ids := make([]int64, 0, len(out.Playlist.TrackIDs))
	for _, t := range out.Playlist.TrackIDs {
		ids = append(ids, t.ID)
	}
	info := out.Playlist.PlaylistInfo
	if info.Copywriter == "" {
		info.Copywriter = out.Playlist.Description
	}
	return info, ids, err
}

// LikedIDs lists the account's liked song IDs.
func (c *Client) LikedIDs(ctx context.Context, uid int64) ([]int64, error) {
	var out struct {
		IDs []int64 `json:"ids"`
	}
	r, err := c.Weapi(ctx, "/song/like/get", map[string]any{"uid": uid}, &out)
	if err == nil {
		err = r.err("/song/like/get")
	}
	return out.IDs, err
}

// SetLiked likes or unlikes a song on the account (it feeds recommendations).
func (c *Client) SetLiked(ctx context.Context, id int64, liked bool) error {
	r, err := c.Weapi(ctx, "/radio/like", map[string]any{"alg": "itembased", "trackId": id, "like": liked, "time": "3"}, nil)
	if err == nil {
		err = r.err("/radio/like")
	}
	return err
}

// EditPlaylist adds or removes tracks on one of the account's playlists.
func (c *Client) EditPlaylist(ctx context.Context, playlist int64, add bool, ids []int64) error {
	op := "del"
	if add {
		op = "add"
	}
	list, _ := json.Marshal(ids)
	r, err := c.Weapi(ctx, "/playlist/manipulate/tracks", map[string]any{"op": op, "pid": playlist, "trackIds": string(list), "imme": "true"}, nil)
	if err == nil {
		err = r.err("/playlist/manipulate/tracks")
	}
	return err
}

// Scrobble reports a listen so the account's recommendations learn from it.
func (c *Client) Scrobble(ctx context.Context, id int64, seconds int) error {
	logs, _ := json.Marshal([]map[string]any{{"action": "play", "json": map[string]any{
		"download": 0, "end": "playend", "id": id, "sourceId": "", "time": seconds, "type": "song", "wifi": 0, "source": "list",
	}}})
	r, err := c.Weapi(ctx, "/feedback/weblog", map[string]any{"logs": string(logs)}, nil)
	if err == nil {
		err = r.err("/feedback/weblog")
	}
	return err
}

// legacySong is the older song shape some recommendation endpoints return.
type legacySong struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Artists  []Artist `json:"artists"`
	Album    Album    `json:"album"`
	Duration int64    `json:"duration"`
}

func (s legacySong) song() Song {
	return Song{ID: s.ID, Name: s.Name, Artists: s.Artists, Album: s.Album, Duration: s.Duration}
}

// NewSongsForYou is NetEase's personalised "推荐新音乐".
func (c *Client) NewSongsForYou(ctx context.Context, limit int) ([]Song, error) {
	var out struct {
		Result []struct {
			Song legacySong `json:"song"`
		} `json:"result"`
	}
	r, err := c.Weapi(ctx, "/personalized/newsong", map[string]any{"type": "recommend", "limit": limit, "areaId": 0}, &out)
	if err == nil {
		err = r.err("/personalized/newsong")
	}
	songs := make([]Song, 0, len(out.Result))
	for _, item := range out.Result {
		songs = append(songs, item.Song.song())
	}
	return songs, err
}

// NewReleases is "新歌速递" for a region: 0 all, 7 华语, 96 欧美, 8 日本, 16 韩国.
func (c *Client) NewReleases(ctx context.Context, area int) ([]Song, error) {
	var out struct {
		Data []legacySong `json:"data"`
	}
	r, err := c.Weapi(ctx, "/v1/discovery/new/songs", map[string]any{"areaId": area, "total": true}, &out)
	if err == nil {
		err = r.err("/v1/discovery/new/songs")
	}
	songs := make([]Song, 0, len(out.Data))
	for _, s := range out.Data {
		songs = append(songs, s.song())
	}
	return songs, err
}

// SimilarSongs lists songs NetEase considers close to one song.
func (c *Client) SimilarSongs(ctx context.Context, id int64, limit int) ([]Song, error) {
	var out struct {
		Songs []legacySong `json:"songs"`
	}
	r, err := c.Weapi(ctx, "/v1/discovery/simiSong", map[string]any{"songid": id, "limit": limit, "offset": 0}, &out)
	if err == nil {
		err = r.err("/v1/discovery/simiSong")
	}
	songs := make([]Song, 0, len(out.Songs))
	for _, s := range out.Songs {
		songs = append(songs, s.song())
	}
	return songs, err
}
