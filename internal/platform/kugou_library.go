package platform

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"music/internal/source"
)

type kugouList struct {
	Collection
	Type int
}

func kugouRows(d map[string]any, keys ...string) ([]any, bool) {
	for _, name := range keys {
		if value, exists := d[name]; exists {
			rows, ok := value.([]any)
			return rows, ok
		}
	}
	return nil, false
}

func (c *Client) kugouLists(ctx context.Context, s Session) ([]kugouList, error) {
	if s.ID == "" {
		return nil, ErrNotConnected
	}
	if err := kugouValidate(s); err != nil {
		return nil, err
	}
	out := []kugouList{}
	seen := map[string]bool{}
	for page := 1; page <= 20; page++ {
		d, err := c.kugouCall(ctx, "gateway.kugou.com", "/v7/get_all_list", "cloudlist.service.kugou.com", url.Values{"plat": {"1"}}, map[string]any{"userid": s.ID, "token": s.Cookies["token"], "total_ver": 979, "type": 2, "page": page, "pagesize": 100}, s.Cookies, false)
		if err != nil {
			return nil, err
		}
		rows, ok := kugouRows(d, "info", "list")
		if !ok {
			return nil, ErrResponse
		}
		for _, value := range rows {
			row := object(value)
			id := text(row["listid"])
			name := first(row, "name", "listname", "list_name")
			if !numericID.MatchString(id) || name == "" || seen[id] {
				continue
			}
			seen[id] = true
			typ := integer(row["type"])
			if typ < 0 || typ > 2 {
				continue
			}
			count := integer(row["count"])
			if row["count"] == nil {
				count = integer(row["song_count"])
			}
			out = append(out, kugouList{Collection: Collection{ID: id, Name: name, Count: max(count, 0), Cover: kugouImage(first(row, "pic", "cover", "image", "list_pic"))}, Type: typ})
		}
		if len(rows) < 100 || d["total"] != nil && page*100 >= integer(d["total"]) {
			return out, nil
		}
	}
	return nil, ErrResponse
}

func (c *Client) kugouCollections(ctx context.Context) ([]Collection, error) {
	lists, err := c.kugouLists(ctx, c.sessionCopy())
	if err != nil {
		return nil, err
	}
	out := make([]Collection, 0, len(lists))
	for _, list := range lists {
		out = append(out, list.Collection)
	}
	return out, nil
}

func (c *Client) kugouCollection(ctx context.Context, id string, page int) (CollectionPage, error) {
	result := CollectionPage{Tracks: []source.Track{}}
	if !numericID.MatchString(id) || page < 1 || page > 100 {
		return result, ErrResponse
	}
	s := c.sessionCopy()
	lists, err := c.kugouLists(ctx, s)
	if err != nil {
		return result, err
	}
	var chosen *kugouList
	for i := range lists {
		if lists[i].ID == id {
			chosen = &lists[i]
			break
		}
	}
	if chosen == nil {
		return result, source.ErrUnavailable
	}
	d, err := c.kugouCall(ctx, "gateway.kugou.com", "/v4/get_list_all_file_v3", "cloudlist.service.kugou.com", nil, map[string]any{"listid": json.Number(id), "userid": s.ID, "type": chosen.Type, "page": page, "pagesize": 100, "area_code": 1, "allplatform": 1, "show_cover": 1, "token": s.Cookies["token"]}, s.Cookies, false)
	if err != nil {
		return result, err
	}
	rows, ok := kugouRows(d, "info", "list", "files")
	if !ok {
		return result, ErrResponse
	}
	result.Collection = chosen.Collection
	result.Total = chosen.Count
	for _, name := range []string{"count", "total", "total_count"} {
		if d[name] != nil {
			result.Total = integer(d[name])
			break
		}
	}
	if result.Total < 0 || result.Total > 10000 || len(rows) == 0 && (page-1)*100 < result.Total {
		return result, ErrResponse
	}
	result.Collection.Count = result.Total
	for _, value := range rows {
		if track, ok := kugouTrack(object(value)); ok {
			result.Tracks = append(result.Tracks, track)
		}
	}
	if page*100 < result.Total {
		result.Next = page + 1
	}
	return result, nil
}

func kugouImage(value string) string {
	return imageURL(strings.ReplaceAll(value, "{size}", "400"))
}

func kugouTrack(row map[string]any) (source.Track, bool) {
	hash := first(row, "hash", "filehash", "FileHash")
	if !kugouHash.MatchString(hash) {
		return source.Track{}, false
	}
	title := first(row, "songname", "song_name", "SongName", "audio_name")
	singer := first(row, "singername", "singer_name", "SingerName", "author_name")
	if filename := first(row, "filename", "fileName", "FileName"); filename != "" {
		if prefix, name, ok := strings.Cut(filename, " - "); ok {
			if singer == "" {
				singer = prefix
			}
			if title == "" {
				title = name
			}
		} else if title == "" {
			title = filename
		}
	}
	if title == "" {
		return source.Track{}, false
	}
	track := source.Track{Source: "kugou", ID: strings.ToUpper(hash), Title: title, Artists: []string{}, Album: first(row, "album_name", "AlbumName"), Cover: kugouImage(first(row, "album_sizable_cover", "sizable_cover", "cover", "pic")), Duration: integer(row["duration"]), Extra: map[string]string{}}
	for _, value := range strings.FieldsFunc(singer, func(r rune) bool { return r == '、' || r == '/' || r == '&' || r == '，' }) {
		if value = strings.TrimSpace(value); value != "" {
			track.Artists = append(track.Artists, value)
		}
	}
	if len(track.Artists) == 0 {
		for _, value := range array(row["singerinfo"]) {
			if name := first(object(value), "name", "singername"); name != "" {
				track.Artists = append(track.Artists, name)
			}
		}
	}
	if track.Duration == 0 {
		track.Duration = integer(row["timelen"]) / 1000
	}
	if track.Duration > 10000 {
		track.Duration /= 1000
	}
	for _, key := range []string{"album_id", "album_audio_id"} {
		if id := text(row[key]); numericID.MatchString(id) {
			track.Extra[key] = id
		}
	}
	return track, len(track.Artists) > 0
}

func (c *Client) kugouRecommendations(ctx context.Context) ([]source.Track, error) {
	s := c.sessionCopy()
	if s.ID == "" {
		return nil, ErrNotConnected
	}
	d, err := c.kugouCall(ctx, "gateway.kugou.com", "/everyday_song_recommend", "everydayrec.service.kugou.com", nil, map[string]any{"platform": "android", "userid": s.ID}, s.Cookies, false)
	if err != nil {
		return nil, err
	}
	rows, ok := kugouRows(d, "song_list", "songs", "info", "list")
	if !ok {
		return nil, ErrResponse
	}
	tracks := []source.Track{}
	for _, value := range rows {
		if track, ok := kugouTrack(object(value)); ok {
			tracks = append(tracks, track)
		}
	}
	return tracks, nil
}

func (c *Client) kugouStream(ctx context.Context, track source.Track) (source.Stream, error) {
	if !kugouHash.MatchString(track.ID) {
		return source.Stream{}, source.ErrUnavailable
	}
	s := c.sessionCopy()
	if s.ID == "" {
		return source.Stream{}, ErrNotConnected
	}
	if err := kugouValidate(s); err != nil {
		return source.Stream{}, err
	}
	if s.Cookies["dfid"] == "" || s.Cookies["dfid"] == "-" {
		dfid, err := c.kugouRegisterDevice(ctx, s)
		if err != nil {
			return source.Stream{}, err
		}
		s.Cookies["dfid"] = dfid
		c.mu.Lock()
		if c.session.ID == s.ID && c.session.Cookies["token"] == s.Cookies["token"] {
			c.session.Cookies["dfid"] = dfid
		}
		c.mu.Unlock()
	}
	hash := strings.ToLower(track.ID)
	for _, quality := range []string{"320", "128"} {
		q := url.Values{"album_id": {"0"}, "album_audio_id": {"0"}, "area_code": {"1"}, "hash": {hash}, "ssa_flag": {"is_fromtrack"}, "version": {"11430"}, "page_id": {"151369488"}, "quality": {quality}, "behavior": {"play"}, "pid": {"2"}, "cmd": {"26"}, "pidversion": {"3001"}, "IsFreePart": {"0"}, "ppage_id": {"463467626,350369493,788954147"}, "cdnBackup": {"1"}, "module": {""}, "clientver": {"11430"}, "key": {kugouMD5(hash + "57ae12eb6890223e355ccfcb74edf70d" + kugouAppID + s.Cookies["KUGOU_API_MID"] + s.ID)}}
		for _, name := range []string{"album_id", "album_audio_id"} {
			if id := track.Extra[name]; numericID.MatchString(id) {
				q.Set(name, id)
			}
		}
		raw, err := c.kugouRequest(ctx, "gateway.kugou.com", "/v5/url", "trackercdn.kugou.com", q, nil, s.Cookies, false)
		if err != nil {
			continue
		}
		d, err := decode(raw)
		if err != nil || integer(d["status"]) != 1 || integer(d["error_code"]) != 0 {
			continue
		}
		// URL v5 returns a flat body (unlike the account endpoints). Never
		// treat a free preview or encrypted-audio response as a full track.
		if integer(d["is_free_part"]) != 0 || integer(d["free_part"]) != 0 || text(d["encrypt_key"]) != "" {
			continue
		}
		link := text(d["url"])
		if links := array(d["url"]); len(links) > 0 {
			link = text(links[0])
		}
		u, err := url.Parse(link)
		if err != nil || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || (u.Port() != "" && u.Port() != "443" && u.Port() != "80") || !strings.HasSuffix(strings.ToLower(u.Hostname()), ".kugou.com") {
			continue
		}
		u.Scheme = "https"
		if u.Port() == "80" {
			u.Host = u.Hostname()
		}
		seconds := integer(d["timeLength"])
		if seconds == 0 {
			seconds = integer(d["timelength"])
		}
		if seconds > 10000 {
			seconds /= 1000
		}
		if seconds <= 0 || track.Duration > 0 && seconds < track.Duration-5 {
			continue
		}
		bitrate := integer(d["bitRate"])
		if bitrate == 0 {
			bitrate, _ = strconv.Atoi(quality)
			bitrate *= 1000
		} else if bitrate < 1000 {
			bitrate *= 1000
		}
		format := strings.ToLower(first(d, "extName", "format"))
		if format == "" {
			format = "mp3"
		}
		if format != "mp3" {
			continue
		}
		return source.Stream{URL: u.String(), Bitrate: bitrate, Format: format, Seconds: seconds}, nil
	}
	return source.Stream{}, source.ErrUnavailable
}
