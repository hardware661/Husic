package source

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Chart is a ranked list a platform publishes.
type Chart struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Short  string `json:"short"`
}

// Charts lists the rankings Husic shows, grouped by platform.
var Charts = []Chart{
	{"netease", "3778678", "网易云热歌榜", "热歌"}, {"netease", "3779629", "网易云新歌榜", "新歌"},
	{"netease", "19723756", "网易云飙升榜", "飙升"}, {"netease", "2884035", "网易云原创榜", "原创"},
	{"qq", "26", "QQ 音乐热歌榜", "热歌"}, {"qq", "27", "QQ 音乐新歌榜", "新歌"},
	{"qq", "62", "QQ 音乐飙升榜", "飙升"}, {"qq", "4", "QQ 音乐流行指数", "流行指数"},
	{"kuwo", "16", "酷我热歌榜", "热歌"}, {"kuwo", "17", "酷我新歌榜", "新歌"}, {"kuwo", "93", "酷我飙升榜", "飙升"},
	{"kugou", "8888", "酷狗 TOP500", "TOP500"}, {"kugou", "6666", "酷狗飙升榜", "飙升"}, {"kugou", "31308", "酷狗新歌榜", "新歌"},
}

// ChartSource is a provider that can list one of its charts.
type ChartSource interface {
	Chart(ctx context.Context, id string, limit int) (tracks []Track, updated string, err error)
}

func (p QQ) Chart(ctx context.Context, id string, limit int) ([]Track, string, error) {
	doc, err := p.HTTP.getJSON(ctx, "https://c.y.qq.com/v8/fcg-bin/fcg_v8_toplist_cp.fcg", url.Values{
		"topid": {id}, "format": {"json"}, "inCharset": {"utf8"}, "outCharset": {"utf-8"}, "platform": {"yqq.json"},
		"needNewCode": {"0"}, "type": {"top"}, "tpl": {"3"}, "page": {"detail"}, "song_begin": {"0"}, "song_num": {strconv.Itoa(limit)},
	}, qqHeaders)
	if err != nil {
		return nil, "", err
	}
	var out []Track
	for _, row := range arr(doc["songlist"]) {
		raw := obj(obj(row)["data"])
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
	return out, str(doc["update_time"]), nil
}

func (p Kuwo) Chart(ctx context.Context, id string, limit int) ([]Track, string, error) {
	doc, err := p.HTTP.getJSON(ctx, "https://kbangserver.kuwo.cn/ksong.s", url.Values{
		"from": {"pc"}, "fmt": {"json"}, "pn": {"0"}, "rn": {strconv.Itoa(limit)}, "type": {"bang"}, "data": {"content"},
		"id": {id}, "show_copyright_off": {"0"}, "pcmp4": {"1"}, "isbang": {"1"},
	}, kuwoHeaders)
	if err != nil {
		return nil, "", err
	}
	var out []Track
	var albums []string
	for _, row := range arr(doc["musiclist"]) {
		raw := obj(row)
		rid := str(raw["id"])
		if rid == "" {
			continue
		}
		seconds := num(raw["song_duration"])
		if seconds == 0 {
			seconds = num(raw["duration"])
		}
		out = append(out, Track{Source: "kuwo", ID: rid, Title: clean(str(raw["name"])), Artists: splitArtists(str(raw["artist"])),
			Album: clean(str(raw["album"])), Duration: seconds, Cover: kuwoCover(str(raw["pic"]))})
		albums = append(albums, str(raw["albumid"]))
	}
	covers := p.albumCovers(ctx, albums)
	for i := range out {
		if out[i].Cover == "" {
			out[i].Cover = covers[albums[i]]
		}
	}
	return out, str(doc["pub"]), nil
}

// albumCovers looks artwork up by album, since Kuwo's chart rows carry none.
// It gives up after a few seconds; songs it missed simply have no cover.
func (p Kuwo) albumCovers(ctx context.Context, albums []string) map[string]string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		covers = map[string]string{}
		slots  = make(chan struct{}, 12)
	)
	for _, id := range albums {
		if id == "" || id == "0" {
			continue
		}
		if _, seen := covers[id]; seen {
			continue
		}
		covers[id] = ""
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			doc, err := p.HTTP.getJSON(ctx, "https://search.kuwo.cn/r.s", url.Values{
				"stype": {"albuminfo"}, "albumid": {id}, "show_copyright_off": {"1"}, "pn": {"0"}, "rn": {"1"}, "mobi": {"1"}, // mobi=1 gets real JSON
			}, kuwoHeaders)
			if err != nil {
				return
			}
			pic := str(doc["hts_img"])
			if pic == "" {
				pic = str(doc["img"])
			}
			mu.Lock()
			covers[id] = kuwoCover(pic)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return covers
}

// kuwoCover asks Kuwo's image host for a larger cut of an album picture.
func kuwoCover(pic string) string {
	if pic == "" {
		return ""
	}
	if !strings.HasPrefix(pic, "http") {
		pic = "https://img4.kuwo.cn/star/albumcover/500/" + strings.TrimPrefix(pic, "/")
	}
	for _, size := range []string{"/120/", "/240/"} {
		pic = strings.Replace(pic, size, "/500/", 1)
	}
	return secure(pic)
}

func (p Kugou) Chart(ctx context.Context, id string, limit int) ([]Track, string, error) {
	// Kugou only serves this list over plain HTTP; it carries no credentials.
	doc, err := p.HTTP.getJSON(ctx, "http://mobilecdnbj.kugou.com/api/v3/rank/song", url.Values{
		"rankid": {id}, "ranktype": {"2"}, "page": {"1"}, "pagesize": {strconv.Itoa(limit)}, "volid": {""}, "plat": {"2"}, "version": {"8955"},
	}, kugouHeaders)
	if err != nil {
		return nil, "", err
	}
	var out []Track
	for _, row := range arr(dig(doc, "data", "info")) {
		raw := obj(row)
		hash := str(raw["hash"])
		singer, title, ok := strings.Cut(str(raw["filename"]), " - ")
		if hash == "" || !ok {
			continue
		}
		out = append(out, Track{Source: "kugou", ID: hash, Title: clean(title), Artists: splitArtists(singer), Duration: num(raw["duration"]),
			Cover: strings.Replace(str(dig(raw, "trans_param", "union_cover")), "{size}", "400", 1)})
	}
	return out, "", nil
}
