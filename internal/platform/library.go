package platform

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"music/internal/source"
)

func first(row map[string]any, keys ...string) string {
	for _, k := range keys {
		if value := text(row[k]); value != "" {
			return value
		}
	}
	return ""
}
func collectionID(row map[string]any) string { return first(row, "tid", "dissid", "disstid", "id") }
func (c *Client) Collections(ctx context.Context) ([]Collection, error) {
	if c.kind == "kugou" {
		return c.kugouCollections(ctx)
	}
	if c.kind == "migu" {
		return c.miguCollections(ctx)
	}
	s := c.sessionCopy()
	if s.ID == "" {
		return nil, ErrNotConnected
	}
	if c.kind == "bilibili" {
		d, _, err := c.biliCall(ctx, "api.bilibili.com", "/x/v3/fav/folder/created/list-all", url.Values{"up_mid": {s.ID}, "jsonp": {"jsonp"}}, s.Cookies)
		if err != nil {
			return nil, err
		}
		if d["list"] == nil && integer(d["count"]) != 0 {
			return nil, ErrResponse
		}
		out := []Collection{}
		for _, v := range array(d["list"]) {
			row := object(v)
			id := text(row["id"])
			if numericID.MatchString(id) {
				out = append(out, Collection{ID: id, Name: text(row["title"]), Cover: imageURL(row["cover"]), Count: integer(row["media_count"])})
			}
		}
		return out, nil
	}
	d, err := c.qqCall(ctx, "music.musicasset.PlaylistBaseRead", "GetPlaylistByUin", map[string]any{"uin": s.ID}, &s)
	if err != nil {
		return nil, err
	}
	if d["v_playlist"] == nil {
		return nil, ErrResponse
	}
	out := []Collection{}
	for _, v := range array(d["v_playlist"]) {
		row := object(v)
		id := collectionID(row)
		if !numericID.MatchString(id) {
			continue
		}
		out = append(out, Collection{ID: id, Name: first(row, "diss_name", "dissname", "title", "name"), Cover: imageURL(first(row, "picurl", "cover", "logo", "picUrl")), Count: integer(row["songnum"])})
	}
	return out, nil
}

func (c *Client) Collection(ctx context.Context, id string, page int) (CollectionPage, error) {
	if c.kind == "kugou" {
		return c.kugouCollection(ctx, id, page)
	}
	if c.kind == "migu" {
		return c.miguCollection(ctx, id, page)
	}
	s := c.sessionCopy()
	if s.ID == "" {
		return CollectionPage{}, ErrNotConnected
	}
	if !numericID.MatchString(id) || page < 1 || page > 100 {
		return CollectionPage{}, ErrResponse
	}
	result := CollectionPage{Tracks: []source.Track{}}
	if c.kind == "bilibili" {
		d, _, err := c.biliCall(ctx, "api.bilibili.com", "/x/v3/fav/resource/list", url.Values{"media_id": {id}, "pn": {strconv.Itoa(page)}, "ps": {"20"}, "keyword": {""}, "order": {"mtime"}, "type": {"0"}, "tid": {"0"}, "platform": {"web"}}, s.Cookies)
		if err != nil {
			return result, err
		}
		info := object(d["info"])
		if text(info["id"]) != id || d["medias"] == nil && integer(info["media_count"]) != 0 {
			return result, ErrResponse
		}
		result.Collection = Collection{ID: id, Name: text(info["title"]), Cover: imageURL(info["cover"]), Count: integer(info["media_count"])}
		result.Total = result.Collection.Count
		for _, v := range array(d["medias"]) {
			if t, ok := biliCollectionTrack(object(v)); ok {
				result.Tracks = append(result.Tracks, t)
			}
		}
		if d["has_more"] == true {
			result.Next = page + 1
		}
		return result, nil
	}
	d, err := c.qqCall(ctx, "music.srfDissInfo.DissInfo", "CgiGetDiss", map[string]any{"disstid": json.Number(id), "dirid": 0, "tag": true, "song_begin": (page - 1) * 100, "song_num": 100, "userinfo": true, "orderlist": true, "onlysonglist": false}, &s)
	if err != nil {
		return result, err
	}
	info := object(d["dirinfo"])
	if collectionID(info) != id || d["songlist"] == nil || d["total_song_num"] == nil {
		return result, ErrResponse
	}
	result.Total = integer(d["total_song_num"])
	if result.Total < 0 || result.Total > 10000 {
		return result, ErrResponse
	}
	result.Collection = Collection{ID: id, Name: first(info, "title", "dissname", "diss_name"), Cover: imageURL(first(info, "picurl", "cover", "logo")), Count: result.Total}
	for _, v := range array(d["songlist"]) {
		if t, ok := qqTrack(object(v)); ok {
			result.Tracks = append(result.Tracks, t)
		}
	}
	if page*100 < result.Total {
		if len(array(d["songlist"])) == 0 {
			return result, ErrResponse
		}
		result.Next = page + 1
	}
	return result, nil
}
