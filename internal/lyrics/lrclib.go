package lyrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// LRCLib looks a song up on lrclib.net, an open lyrics database, and returns
// synced LRC text when one of its entries has about the same length.
func LRCLib(ctx context.Context, client *http.Client, title, artist string, seconds int) (string, error) {
	q := url.Values{"track_name": {title}, "artist_name": {artist}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://lrclib.net/api/search?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Music (personal player; hdwde.com)")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var found []struct {
		Duration float64 `json:"duration"`
		Synced   string  `json:"syncedLyrics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&found); err != nil {
		return "", err
	}
	for _, f := range found {
		if f.Synced != "" && (seconds == 0 || abs(int(f.Duration)-seconds) <= 3) {
			return f.Synced, nil
		}
	}
	return "", nil
}
