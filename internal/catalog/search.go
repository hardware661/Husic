// Package catalog answers "what music is there": one search across every
// platform, with copies of the same recording merged into one result.
package catalog

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hardware661/husic/internal/source"
)

// Result is one recording and every platform copy the search turned up.
type Result struct {
	source.Track
	Copies []source.Track `json:"copies"`
	score  float64
}

// Words that mark derivative cuts; they sink unless the query asks for them.
var derivative = []string{"伴奏", "ktv", "dj", "片段", "铃声", "remix", "翻自", "cover", "live", "现场", "加速", "降调", "0.8x", "1.2x"}

// Search asks every provider at once (each gets at most budget), merges the
// copies and ranks by how well and how widely each recording matched.
func Search(ctx context.Context, providers []source.Provider, query string, budget time.Duration) []Result {
	type answer struct {
		rank   int
		tracks []source.Track
	}
	answers := make([]answer, len(providers))
	var wg sync.WaitGroup
	for i, p := range providers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(ctx, budget)
			defer cancel()
			tracks, err := p.Search(ctx, query, 20)
			if err == nil {
				answers[i] = answer{rank: i, tracks: tracks}
			}
		}()
	}
	wg.Wait()

	var results []*Result
	folded := fold(query)
	for _, a := range answers {
		weight := 1.0 / float64(1+a.rank) // earlier providers break ties
		for pos, t := range a.tracks {
			if t.Title == "" {
				continue
			}
			var group *Result
			for _, r := range results {
				if source.SameSong(r.Track, t) {
					group = r
					break
				}
			}
			if group == nil {
				group = &Result{Track: t}
				results = append(results, group)
			}
			if !slices.ContainsFunc(group.Copies, func(c source.Track) bool { return c.Source == t.Source }) {
				group.Copies = append(group.Copies, t)
				group.score += 1/float64(1+pos) + 0.2*weight
			}
			if group.Cover == "" && t.Cover != "" {
				group.Cover = t.Cover
			}
			if group.Album == "" {
				group.Album = t.Album
			}
		}
	}
	for _, r := range results {
		title := fold(r.Title)
		switch {
		case title == folded:
			r.score += 2
		case strings.Contains(folded, title) || strings.Contains(title, folded):
			r.score += 1
		}
		for _, artist := range r.Artists {
			if a := fold(artist); a != "" && strings.Contains(folded, a) {
				r.score += 1.5
				break
			}
		}
		lower := strings.ToLower(r.Title + " " + r.Album)
		for _, w := range derivative {
			if strings.Contains(lower, w) && !strings.Contains(strings.ToLower(query), w) {
				r.score -= 1.5
				break
			}
		}
	}
	slices.SortStableFunc(results, func(a, b *Result) int { return cmp.Compare(b.score, a.score) })
	out := make([]Result, 0, len(results))
	for _, r := range results {
		out = append(out, *r)
	}
	return out
}

func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r > ' ' && !strings.ContainsRune(",.!?;:'\"-_()（）[]【】《》、，。！？；：·/&", r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
