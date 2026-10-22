package source

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Recommender lists a platform's own public recommendations, not search results.
type Recommender interface {
	Recommend(context.Context, int) ([]Track, error)
}

func (p Migu) Recommend(ctx context.Context, limit int) ([]Track, error) {
	h := p.RecommendHTTP
	if h == nil {
		h = p.HTTP
	}
	doc, err := h.getJSON(ctx, "https://app.c.nf.migu.cn/pc/resource-dataloader/recommend-song/v1.0", url.Values{
		"scene": {"TODAY_RECOMMEND"}, "action": {"1"}, "size": {strconv.Itoa(limit)},
	}, map[string]string{"Referer": "https://music.migu.cn/", "User-Agent": "Mozilla/5.0", "deviceId": "music-personal-web"})
	if err != nil {
		return nil, err
	}
	if str(doc["code"]) != "000000" {
		return nil, fmt.Errorf("migu recommendations unavailable")
	}
	var out []Track
	for _, row := range arr(dig(doc, "data", "songItemList")) {
		raw := obj(row)
		id := str(raw["contentId"])
		if id == "" || str(raw["songName"]) == "" {
			continue
		}
		cover := str(raw["img3"])
		if cover == "" {
			cover = str(raw["img1"])
		}
		if strings.HasPrefix(cover, "/") {
			cover = "https://d.musicapp.migu.cn" + cover
		}
		out = append(out, Track{Source: "migu", ID: id, Title: clean(str(raw["songName"])), Artists: names(arr(raw["singerList"]), "name"),
			Album: clean(str(raw["album"])), Duration: num(raw["duration"]), Cover: secure(cover),
			Extra: map[string]string{"copyrightId": str(raw["copyrightId"]), "songId": str(raw["songId"]), "albumId": str(raw["albumId"])}})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}
