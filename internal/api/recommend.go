package api

import (
	"context"
	"math/rand/v2"
	"net/http"
	"slices"
	"sync"
	"time"

	"music/internal/source"
)

var recommendationOrder = []string{"netease", "qq", "kuwo", "kugou", "migu"}

func (s *Server) radioMore(w http.ResponseWriter, r *http.Request) {
	picks := blendRecommendations(s.recommendationFeeds(r.Context()), 24, true)
	items := make([]Item, len(picks))
	for i, p := range picks {
		items[i] = p.Item
	}
	s.annotate(r.Context(), items)
	writeJSON(w, http.StatusOK, map[string]any{"songs": items})
}

type recommendationFeed struct {
	Source    string `json:"source"`
	Kind      string `json:"kind"`
	Songs     []Item `json:"-"`
	Count     int    `json:"count"`
	Available bool   `json:"available"`
}

// Each source has its own cache and failure boundary. NetEase is first, not
// required: the remaining four still work when its account or API is down.
func (s *Server) recommendationFeeds(ctx context.Context) []recommendationFeed {
	feeds := make([]recommendationFeed, len(recommendationOrder))
	var wg sync.WaitGroup
	for i, src := range recommendationOrder {
		wg.Add(1)
		go func() {
			defer wg.Done()
			kind := "榜单精选"
			if src == "netease" {
				kind = "每日推荐"
			}
			if src == "migu" {
				kind = "平台推荐"
			}
			feed := recommendationFeed{Source: src, Kind: kind, Songs: []Item{}}
			v, err := s.cached(ctx, "recommendation:"+src+":"+beijingDay(), 30*time.Minute, func(ctx context.Context) (any, error) {
				ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
				defer cancel()
				if src == "netease" {
					if s.NetEase == nil {
						return nil, errNoAccount
					}
					if daily, ok := s.peek("daily:" + beijingDay()); ok {
						return slices.Clone(daily.([]Item)), nil
					}
					songs, err := s.NetEase.DailySongs(ctx)
					items := make([]Item, 0, len(songs))
					for _, song := range songs {
						items = append(items, fromTrack(source.FromNetEase(song)))
					}
					return items, err
				}
				p := s.provider(src)
				var tracks []source.Track
				var err error
				if recommender, ok := p.(source.Recommender); ok {
					tracks, err = recommender.Recommend(ctx, 40)
				} else if charts, ok := p.(source.ChartSource); ok {
					id := map[string]string{"qq": "27", "kuwo": "17", "kugou": "31308"}[src]
					tracks, _, err = charts.Chart(ctx, id, 60)
				} else {
					return nil, errNoAccount
				}
				items := make([]Item, 0, len(tracks))
				for _, track := range tracks {
					items = append(items, fromTrack(track))
				}
				return items, err
			})
			if err == nil {
				feed.Songs = slices.Clone(v.([]Item))
				feed.Count = len(feed.Songs)
				feed.Available = feed.Count > 0
			}
			feeds[i] = feed
		}()
	}
	wg.Wait()
	return feeds
}

// Round-robin gives every available platform a place, with NetEase first.
// Merge actual copies of the same recording instead of repeating five rows.
func blendRecommendations(feeds []recommendationFeed, n int, shuffle bool) []pick {
	buckets := make([][]Item, len(feeds))
	for i, f := range feeds {
		buckets[i] = slices.Clone(f.Songs)
		if shuffle {
			rand.Shuffle(len(buckets[i]), func(a, b int) { buckets[i][a], buckets[i][b] = buckets[i][b], buckets[i][a] })
		}
	}
	out := make([]pick, 0, n)
	for round := 0; len(out) < n; round++ {
		any := false
		for _, b := range buckets {
			if round >= len(b) {
				continue
			}
			any = true
			item := b[round]
			at := slices.IndexFunc(out, func(p pick) bool { return source.SameSong(p.want(), item.want()) })
			if at >= 0 {
				for _, c := range item.Copies {
					if !slices.ContainsFunc(out[at].Copies, func(t source.Track) bool { return t.Ref() == c.Ref() }) {
						out[at].Copies = append(slices.Clone(out[at].Copies), c)
					}
				}
				continue
			}
			out = append(out, pick{Item: item, Reason: "为你发现的好歌"})
			if len(out) == n {
				break
			}
		}
		if !any {
			break
		}
	}
	return out
}
