package platform

import (
	"context"
	"strconv"

	"github.com/hardware661/husic/internal/source"
)

func (c *Client) Search(ctx context.Context, q string, limit int) ([]source.Track, error) {
	return c.biliSearch(ctx, q, limit)
}

func (c *Client) Stream(ctx context.Context, t source.Track) (source.Stream, error) {
	return c.biliStream(ctx, t)
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
