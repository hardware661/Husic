package api

import (
	"context"
	"fmt"
	"testing"

	"github.com/hardware661/husic/internal/source"
)

type feedProvider string

func (p feedProvider) Name() string                                                { return string(p) }
func (p feedProvider) Search(context.Context, string, int) ([]source.Track, error) { return nil, nil }
func (p feedProvider) Stream(context.Context, source.Track) (source.Stream, error) {
	return source.Stream{}, source.ErrUnavailable
}
func (p feedProvider) Chart(context.Context, string, int) ([]source.Track, string, error) {
	return []source.Track{{Source: string(p), ID: "one", Title: string(p) + " song", Artists: []string{"Artist"}}}, "", nil
}
func (p feedProvider) Recommend(ctx context.Context, n int) ([]source.Track, error) {
	tracks, _, err := p.Chart(ctx, "", n)
	return tracks, err
}

func TestRecommendationsSurviveMissingNetEase(t *testing.T) {
	s := &Server{Providers: []source.Provider{feedProvider("qq"), feedProvider("kuwo"), feedProvider("kugou"), feedProvider("migu")}}
	feeds := s.recommendationFeeds(context.Background())
	if len(feeds) != 5 || feeds[0].Source != "netease" || feeds[0].Available {
		t.Fatalf("bad source order/status: %+v", feeds)
	}
	for i, f := range feeds[1:] {
		if !f.Available || f.Count != 1 {
			t.Errorf("source %d lost its independent feed: %+v", i, f)
		}
	}
	got := blendRecommendations(feeds, 20, false)
	if len(got) != 4 {
		t.Fatalf("NetEase outage emptied other recommendations: %d", len(got))
	}
}

func TestBlendingIncludesAllFiveAndMergesCopies(t *testing.T) {
	var feeds []recommendationFeed
	for _, src := range recommendationOrder {
		it := fromTrack(source.Track{Source: src, ID: "one", Title: src + " song", Artists: []string{"Artist"}})
		feeds = append(feeds, recommendationFeed{Source: src, Songs: []Item{it}})
	}
	got := blendRecommendations(feeds, 10, false)
	if len(got) != 5 || got[0].Copies[0].Source != "netease" {
		t.Fatalf("not five sources with NetEase first: %+v", got)
	}
	for i := range feeds {
		feeds[i].Songs[0].Title = "Same song"
	}
	got = blendRecommendations(feeds, 10, false)
	if len(got) != 1 || len(got[0].Copies) != 5 {
		t.Fatal(fmt.Sprintf("copies not merged: %+v", got))
	}
}
