package platform

import (
	"context"
	"net/url"
	"strconv"

	"github.com/hardware661/husic/internal/source"
)

func (c *Client) Collections(ctx context.Context) ([]Collection, error) {
	s := c.sessionCopy()
	if s.ID == "" {
		return nil, ErrNotConnected
	}
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

func (c *Client) Collection(ctx context.Context, id string, page int) (CollectionPage, error) {
	s := c.sessionCopy()
	if s.ID == "" {
		return CollectionPage{}, ErrNotConnected
	}
	if !numericID.MatchString(id) || page < 1 || page > 100 {
		return CollectionPage{}, ErrResponse
	}
	result := CollectionPage{Tracks: []source.Track{}}
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
