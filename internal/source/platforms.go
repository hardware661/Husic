package source

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/url"
	"strconv"
	"strings"
)

// QQ is QQ Music without an account: free tracks only, 128k MP3.
type QQ struct{ HTTP *HTTP }

func (QQ) Name() string { return "qq" }

var qqHeaders = map[string]string{"Referer": "https://y.qq.com/"}

func (p QQ) Search(ctx context.Context, query string, limit int) ([]Track, error) {
	doc, err := p.HTTP.getJSON(ctx, "https://c.y.qq.com/soso/fcgi-bin/search_for_qq_cp",
		url.Values{"w": {query}, "format": {"json"}, "p": {"1"}, "n": {strconv.Itoa(limit)}}, qqHeaders)
	if err != nil {
		return nil, err
	}
	var out []Track
	for _, item := range arr(dig(doc, "data", "song", "list")) {
		raw := obj(item)
		mid := str(raw["songmid"])
		if mid == "" {
			continue
		}
		t := Track{Source: "qq", ID: mid, Title: clean(str(raw["songname"])), Artists: names(arr(raw["singer"]), "name"),
			Album: clean(str(raw["albumname"])), Duration: num(raw["interval"])}
		if album := str(raw["albummid"]); album != "" {
			t.Cover = "https://y.gtimg.cn/music/photo_new/T002R500x500M000" + album + ".jpg"
		}
		out = append(out, t)
	}
	return out, nil
}

func (p QQ) Stream(ctx context.Context, t Track) (Stream, error) {
	guid, _ := rand.Int(rand.Reader, big.NewInt(9_000_000_000))
	filename := "M500" + t.ID + t.ID + ".mp3"
	doc, err := p.HTTP.postJSON(ctx, "https://u.y.qq.com/cgi-bin/musicu.fcg", map[string]any{
		"comm": map[string]any{"ct": 24, "cv": 0, "uin": "0", "format": "json", "platform": "yqq"},
		"req_0": map[string]any{"module": "music.vkey.GetVkey", "method": "UrlGetVkey", "param": map[string]any{
			"guid": fmt.Sprint(guid.Int64() + 1_000_000_000), "songmid": []string{t.ID}, "songtype": []int{0},
			"uin": "0", "loginflag": 1, "platform": "20", "filename": []string{filename},
		}},
	}, qqHeaders)
	if err != nil {
		return Stream{}, err
	}
	infos := arr(dig(doc, "req_0", "data", "midurlinfo"))
	if len(infos) == 0 {
		return Stream{}, ErrUnavailable
	}
	purl := str(obj(infos[0])["purl"])
	if purl == "" || strings.Contains(purl, "://") {
		return Stream{}, ErrUnavailable
	}
	return Stream{URL: "https://ws.stream.qqmusic.qq.com/" + purl, Bitrate: 128000, Format: "mp3"}, nil
}

// Kugou is Kugou without an account.
type Kugou struct{ HTTP *HTTP }

func (Kugou) Name() string { return "kugou" }

var kugouHeaders = map[string]string{"Referer": "https://www.kugou.com/"}

func (p Kugou) Search(ctx context.Context, query string, limit int) ([]Track, error) {
	doc, err := p.HTTP.getJSON(ctx, "https://mobiles.kugou.com/api/v3/search/song", url.Values{
		"format": {"json"}, "keyword": {query}, "page": {"1"}, "pagesize": {strconv.Itoa(limit)}, "showtype": {"1"},
	}, kugouHeaders)
	if err != nil {
		return nil, err
	}
	var out []Track
	for _, item := range arr(dig(doc, "data", "info")) {
		raw := obj(item)
		hash := str(raw["hash"])
		if hash == "" {
			continue
		}
		out = append(out, Track{Source: "kugou", ID: hash, Title: clean(str(raw["songname"])), Artists: splitArtists(str(raw["singername"])),
			Album: clean(str(raw["album_name"])), Duration: num(raw["duration"])})
	}
	return out, nil
}

func (p Kugou) Stream(ctx context.Context, t Track) (Stream, error) {
	doc, err := p.HTTP.getJSON(ctx, "https://m.kugou.com/app/i/getSongInfo.php",
		url.Values{"cmd": {"playInfo"}, "hash": {t.ID}}, kugouHeaders)
	if err != nil {
		return Stream{}, err
	}
	link := str(doc["url"])
	if link == "" || num(doc["errcode"]) != 0 {
		return Stream{}, ErrUnavailable
	}
	return Stream{URL: secure(link), Bitrate: num(doc["bitRate"]) * 1000, Format: strings.ToLower(str(doc["extName"])), Seconds: num(doc["timeLength"])}, nil
}

// Kuwo is Kuwo without an account.
type Kuwo struct{ HTTP *HTTP }

func (Kuwo) Name() string { return "kuwo" }

var kuwoHeaders = map[string]string{"Referer": "https://www.kuwo.cn/"}

func (p Kuwo) Search(ctx context.Context, query string, limit int) ([]Track, error) {
	// Without the full client parameters this endpoint answers with clips
	// and karaoke cuts instead of the canonical recordings.
	doc, err := p.HTTP.getJSON(ctx, "https://search.kuwo.cn/r.s", url.Values{
		"client": {"kt"}, "all": {query}, "pn": {"0"}, "rn": {strconv.Itoa(limit)}, "uid": {"794762570"},
		"ver": {"kwplayer_ar_9.2.2.1"}, "vipver": {"1"}, "show_copyright_off": {"1"}, "newver": {"1"},
		"ft": {"music"}, "cluster": {"0"}, "strategy": {"2012"}, "encoding": {"utf8"}, "rformat": {"json"},
		"vermerge": {"1"}, "mobi": {"1"}, "issubtitle": {"1"},
	}, kuwoHeaders)
	if err != nil {
		return nil, err
	}
	var out []Track
	for _, item := range arr(doc["abslist"]) {
		raw := obj(item)
		rid := strings.TrimPrefix(str(raw["MUSICRID"]), "MUSIC_")
		if rid == "" {
			continue
		}
		title := str(raw["SONGNAME"])
		if title == "" {
			title = str(raw["NAME"])
		}
		out = append(out, Track{Source: "kuwo", ID: rid, Title: clean(title), Artists: splitArtists(str(raw["ARTIST"])),
			Album: clean(str(raw["ALBUM"])), Duration: num(raw["DURATION"])})
	}
	return out, nil
}

func (p Kuwo) Stream(ctx context.Context, t Track) (Stream, error) {
	doc, err := p.HTTP.getJSON(ctx, "https://mobi.kuwo.cn/mobi.s", url.Values{
		"f": {"web"}, "source": {"kwplayercar_ar_6.0.0.9_B_jiakong_vh.apk"}, "from": {"PC"},
		"type": {"convert_url_with_sign"}, "br": {"320kmp3"}, "rid": {t.ID}, "user": {"0"},
	}, kuwoHeaders)
	if err != nil {
		return Stream{}, err
	}
	data := obj(doc["data"])
	link := str(data["url"])
	if link == "" {
		return Stream{}, ErrUnavailable
	}
	return Stream{URL: secure(link), Bitrate: num(data["bitrate"]) * 1000, Format: str(data["format"]), Seconds: num(data["duration"])}, nil
}

// Lyrics returns Kuwo's timed lyric lines as LRC text.
func (p Kuwo) Lyrics(ctx context.Context, t Track) (string, error) {
	doc, err := p.HTTP.getJSON(ctx, "https://www.kuwo.cn/openapi/v1/www/lyric/getlyric", url.Values{"musicId": {t.ID}}, kuwoHeaders)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, item := range arr(dig(doc, "data", "lrclist")) {
		line := obj(item)
		seconds, _ := strconv.ParseFloat(str(line["time"]), 64)
		text := strings.TrimSpace(str(line["lineLyric"]))
		if text == "" || seconds == 0 && strings.Contains(text, " - ") {
			continue // the "title - artist" banner Kuwo puts first
		}
		ms := int(seconds * 1000)
		fmt.Fprintf(&b, "[%02d:%02d.%03d]%s\n", ms/60000, ms/1000%60, ms%1000, text)
	}
	return b.String(), nil
}

// Migu is Migu without an account, standard quality.
type Migu struct{ HTTP *HTTP }

func (Migu) Name() string { return "migu" }

var miguHeaders = map[string]string{
	"User-Agent": "Android_migu/7.41.13 okhttp/3.12.13", "Referer": "https://music.migu.cn/",
	"channel": "0146832", "version": "7.41.13",
}

func (p Migu) Search(ctx context.Context, query string, limit int) ([]Track, error) {
	doc, err := p.HTTP.getJSON(ctx, "https://pd.musicapp.migu.cn/MIGUM2.0/v1.0/content/search_all.do", url.Values{
		"ua": {"Android_migu"}, "version": {"5.0.1"}, "text": {query}, "pageNo": {"1"}, "pageSize": {strconv.Itoa(limit)},
		"searchSwitch": {`{"song":1,"album":0,"singer":0,"tagSong":0,"mvSong":0,"songlist":0,"bestShow":1}`},
	}, miguHeaders)
	if err != nil {
		return nil, err
	}
	var out []Track
	for _, item := range arr(dig(doc, "songResultData", "result")) {
		raw := obj(item)
		id := str(raw["contentId"])
		if id == "" {
			continue
		}
		t := Track{Source: "migu", ID: id, Title: clean(str(raw["name"])), Artists: names(arr(raw["singers"]), "name"),
			Extra: map[string]string{"copyrightId": str(raw["copyrightId"]), "songId": str(raw["songId"]), "albumId": str(raw["albumId"])}}
		if albums := arr(raw["albums"]); len(albums) > 0 {
			t.Album = clean(str(obj(albums[0])["name"]))
		}
		if images := arr(raw["imgItems"]); len(images) > 0 {
			t.Cover = str(obj(images[len(images)-1])["img"])
		}
		out = append(out, t)
	}
	return out, nil
}

func (p Migu) Stream(ctx context.Context, t Track) (Stream, error) {
	query := url.Values{"netType": {"01"}, "resourceType": {"2"}, "contentId": {t.ID},
		"copyrightId": {t.Extra["copyrightId"]}, "toneFlag": {"PQ"}}
	for _, key := range []string{"songId", "albumId"} {
		if v := t.Extra[key]; v != "" {
			query.Set(key, v)
		}
	}
	doc, err := p.HTTP.getJSON(ctx, "https://c.musicapp.migu.cn/MIGUM2.0/v2.1/content/listen-url", query, miguHeaders)
	if err != nil {
		return Stream{}, err
	}
	data := obj(doc["data"])
	link := str(data["url"])
	if link == "" || str(data["cannotCode"]) != "" && str(data["cannotCode"]) != "0" {
		return Stream{}, ErrUnavailable
	}
	if listen := str(data["freeListenType"]); listen != "" && listen != "0" {
		return Stream{}, ErrUnavailable
	}
	seconds := num(dig(data, "songItem", "length"))
	if seconds == 0 {
		seconds = num(dig(data, "songItem", "duration"))
	}
	return Stream{URL: secure(link), Bitrate: 128000, Format: "mp3", Seconds: seconds}, nil
}

// secure asks for the same CDN path over TLS: plain http audio is blocked as
// mixed content inside the HTTPS app. A CDN that cannot do TLS fails in the
// player, which then asks for the next copy.
func secure(link string) string { return strings.Replace(link, "http://", "https://", 1) }
