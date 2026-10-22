// Package ytmusic searches YouTube Music the way its web client does.
// Playback addresses are resolved elsewhere; this only finds candidates.
package ytmusic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Track is one song row of a search result.
type Track struct {
	VideoID  string
	Title    string
	Subtitle string // "Artist • Album • 3:45" as YouTube Music shows it
}

type Client struct {
	http *http.Client
}

func New(timeout time.Duration) *Client {
	return &Client{http: &http.Client{Timeout: timeout}}
}

// songsFilter limits results to the "Songs" shelf (official audio tracks).
const songsFilter = "EgWKAQIIAWoMEA4QChADEAQQCRAF"

// SearchSongs looks only at official song tracks.
func (c *Client) SearchSongs(ctx context.Context, query string) ([]Track, error) {
	return c.search(ctx, query, songsFilter)
}

// SearchAll also returns uploaded videos (covers, fan uploads, live clips).
func (c *Client) SearchAll(ctx context.Context, query string) ([]Track, error) {
	return c.search(ctx, query, "")
}

func (c *Client) search(ctx context.Context, query, params string) ([]Track, error) {
	request := map[string]any{
		"context": map[string]any{"client": map[string]any{
			"clientName": "WEB_REMIX", "clientVersion": "1.20241023.01.00", "hl": "zh-CN", "gl": "US",
		}},
		"query": query,
	}
	if params != "" {
		request["params"] = params
	}
	body, _ := json.Marshal(request)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://music.youtube.com/youtubei/v1/search?prettyPrint=false", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://music.youtube.com")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ytmusic search: HTTP %d", resp.StatusCode)
	}
	var tree any
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, err
	}
	var tracks []Track
	walk(tree, func(node map[string]any) {
		if row, ok := node["musicResponsiveListItemRenderer"].(map[string]any); ok {
			if t, ok := parseRow(row); ok {
				tracks = append(tracks, t)
			}
		}
	})
	return tracks, nil
}

// The renderer tree changes often; walking it for rows is sturdier than
// following one fixed path.
func walk(node any, visit func(map[string]any)) {
	switch v := node.(type) {
	case map[string]any:
		visit(v)
		for _, child := range v {
			walk(child, visit)
		}
	case []any:
		for _, child := range v {
			walk(child, visit)
		}
	}
}

func parseRow(row map[string]any) (Track, bool) {
	columns, _ := row["flexColumns"].([]any)
	if len(columns) < 2 {
		return Track{}, false
	}
	t := Track{Title: columnText(columns[0]), Subtitle: columnText(columns[1])}
	if item, ok := row["playlistItemData"].(map[string]any); ok {
		t.VideoID, _ = item["videoId"].(string)
	}
	return t, t.VideoID != "" && t.Title != ""
}

func columnText(column any) string {
	col, _ := column.(map[string]any)
	renderer, _ := col["musicResponsiveListItemFlexColumnRenderer"].(map[string]any)
	text, _ := renderer["text"].(map[string]any)
	runs, _ := text["runs"].([]any)
	var b strings.Builder
	for _, r := range runs {
		if run, ok := r.(map[string]any); ok {
			s, _ := run["text"].(string)
			b.WriteString(s)
		}
	}
	return b.String()
}
