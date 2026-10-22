package api

import (
	"context"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"music/internal/netease"
	"music/internal/source"
)

// pick is a recommended song with the reason it was picked.
type pick struct {
	Item
	Reason string `json:"reason"`
}

func (s *Server) provider(name string) source.Provider {
	for _, p := range s.Providers {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

func (s *Server) charts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"charts": source.Charts})
}

func (s *Server) chart(w http.ResponseWriter, r *http.Request) {
	src, id := r.PathValue("source"), r.PathValue("id")
	i := slices.IndexFunc(source.Charts, func(c source.Chart) bool { return c.Source == src && c.ID == id })
	if i < 0 {
		fail(w, http.StatusNotFound, "no such chart")
		return
	}
	value, err := s.chartData(r.Context(), src, id)
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	out := value.(map[string]any)
	items := slices.Clone(out["songs"].([]Item))
	s.annotate(r.Context(), items)
	writeJSON(w, http.StatusOK, map[string]any{"chart": source.Charts[i], "updated": out["updated"], "songs": items})
}

// chartData loads one ranking. Charts change daily at most, so answers live
// for hours and the warm-up keeps every chart ready.
func (s *Server) chartData(ctx context.Context, src, id string) (any, error) {
	return s.cached(ctx, "chart:"+src+":"+id, 3*time.Hour, func(ctx context.Context) (any, error) {
		if src == "netease" {
			if s.NetEase == nil {
				return nil, errNoAccount
			}
			n, _ := strconv.ParseInt(id, 10, 64)
			_, ids, err := s.NetEase.PlaylistDetail(ctx, n)
			if err != nil {
				return nil, err
			}
			items, err := s.neteaseItems(ctx, ids[:min(len(ids), 100)])
			return map[string]any{"songs": items, "updated": ""}, err
		}
		cs, ok := s.provider(src).(source.ChartSource)
		if !ok {
			return nil, errNoAccount
		}
		tracks, updated, err := cs.Chart(ctx, id, 100)
		items := make([]Item, 0, len(tracks))
		for _, t := range tracks {
			items = append(items, fromTrack(t))
		}
		return map[string]any{"songs": items, "updated": updated}, err
	})
}

// songPool is a NetEase song list Discover draws from, refreshed in the background.
type songPool struct {
	key  string
	load func(ctx context.Context) ([]netease.Song, error)
}

// beijingDay names the NetEase day, which rolls over at 06:00 Beijing time.
func beijingDay() string {
	return time.Now().Add(2 * time.Hour).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
}

func (s *Server) pools() map[string]songPool {
	day := beijingDay()
	area := func(name string, id int) songPool {
		return songPool{name + ":" + day, func(ctx context.Context) ([]netease.Song, error) { return s.NetEase.NewReleases(ctx, id) }}
	}
	return map[string]songPool{
		"radar": {"radar:" + day, func(ctx context.Context) ([]netease.Song, error) { return s.NetEase.NewSongsForYou(ctx, 30) }},
		"fresh": area("fresh", 7), "all": area("all", 0), "west": area("west", 96), "japan": area("japan", 8), "korea": area("korea", 16),
	}
}

func (s *Server) poolSongs(ctx context.Context, p songPool) ([]netease.Song, error) {
	v, err := s.cached(ctx, p.key, 30*time.Minute, func(ctx context.Context) (any, error) { return p.load(ctx) })
	if err != nil {
		return nil, err
	}
	return v.([]netease.Song), nil
}

func (s *Server) dailyItems(ctx context.Context) ([]Item, error) {
	v, err := s.cached(ctx, "daily:"+beijingDay(), 30*time.Minute, func(ctx context.Context) (any, error) {
		songs, err := s.NetEase.DailySongs(ctx)
		items := make([]Item, 0, len(songs))
		for _, song := range songs {
			items = append(items, fromTrack(source.FromNetEase(song)))
		}
		return items, err
	})
	if err != nil {
		return nil, err
	}
	return v.([]Item), nil
}

func (s *Server) recommendedCards(ctx context.Context) ([]card, error) {
	v, err := s.cached(ctx, "picks:"+beijingDay(), 30*time.Minute, func(ctx context.Context) (any, error) {
		list, err := s.NetEase.RecommendedPlaylists(ctx)
		return neteaseCards(list), err
	})
	if err != nil {
		return nil, err
	}
	return v.([]card), nil
}

// Warm keeps every slow NetEase pool filled, so pages answer from memory.
func (s *Server) Warm(ctx context.Context) {
	for {
		s.warmOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Minute):
		}
	}
}

func (s *Server) warmOnce(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	jobs := []func(){
		func() { _ = s.recommendationFeeds(ctx) },
	}
	if s.NetEase != nil {
		jobs = append(jobs,
			func() { _, _ = s.dailyItems(ctx) },
			func() { _, _ = s.recommendedCards(ctx) },
			func() { s.fillRadio(ctx, radioTarget) },
			func() {
				if likes, err := s.Store.Likes(ctx); err == nil {
					for _, i := range rand.Perm(len(likes))[:min(len(likes), 10)] {
						if c, ok := fromSong(likes[i]).copy("netease"); ok {
							s.similarSongs(ctx, c.ID)
						}
					}
				}
			},
		)
	}
	for _, p := range s.pools() {
		if s.NetEase != nil {
			jobs = append(jobs, func() { _, _ = s.poolSongs(ctx, p) })
		}
	}
	jobs = append(jobs, func() {
		for _, c := range source.Charts {
			_, _ = s.chartData(ctx, c.Source, c.ID)
		}
	})
	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job()
		}()
	}
	wg.Wait()
}

// discover gathers several kinds of songs the owner has not met yet. Everything
// comes from warm pools; every request reshuffles them, so "换一批" works.
func (s *Server) discover(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := map[string]any{"picks": []card{}, "radar": []pick{}, "fresh": []pick{}, "more": []pick{}, "world": []pick{}, "random": []pick{}, "similar": nil, "hero": nil}
	pools := s.pools()
	var feeds []recommendationFeed
	var mu sync.Mutex
	var wg sync.WaitGroup
	set := func(key string, value any) {
		mu.Lock()
		out[key] = value
		mu.Unlock()
	}
	run := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}
	draw := func(name string, n int, reason string) []pick {
		if s.NetEase == nil {
			return []pick{}
		}
		songs, err := s.poolSongs(ctx, pools[name])
		if err != nil {
			s.Log.Warn("discover", "pool", name, "err", err)
			return []pick{}
		}
		return s.unknown(ctx, songs, n, reason)
	}
	run(func() {
		if s.NetEase == nil {
			return
		}
		if cards, err := s.recommendedCards(ctx); err == nil {
			cards = slices.Clone(cards)
			rand.Shuffle(len(cards), func(i, j int) { cards[i], cards[j] = cards[j], cards[i] })
			set("picks", cards[:min(4, len(cards))])
		}
	})
	run(func() { feeds = s.recommendationFeeds(ctx) })
	run(func() {
		world := draw("west", 6, "欧美新歌")
		world = append(world, draw("japan", 3, "日本新歌")...)
		world = append(world, draw("korea", 3, "韩国新歌")...)
		set("world", world)
	})
	run(func() {
		if s.NetEase == nil {
			return
		}
		if seed, songs := s.similarToALike(ctx); len(songs) > 0 {
			set("similar", map[string]any{"seed": seed, "songs": songs})
		}
	})
	wg.Wait()
	out["feeds"] = feeds
	for i := range feeds {
		s.annotate(ctx, feeds[i].Songs)
		feeds[i].Songs = slices.DeleteFunc(feeds[i].Songs, func(it Item) bool { return it.SongID != 0 })
	}
	all := blendRecommendations(feeds, 100, true)
	take := func(n int) []pick {
		k := min(n, len(all))
		batch := all[:k]
		all = all[k:]
		return batch
	}
	out["radar"], out["fresh"], out["more"], out["random"] = take(17), take(15), take(24), take(12)
	for _, key := range []string{"radar", "fresh", "random"} {
		if list := out[key].([]pick); len(list) > 0 && list[0].Cover != "" {
			out["hero"] = list[0]
			out[key] = list[1:]
			break
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// unknown keeps songs that are not in the library or history, shuffled.
func (s *Server) unknown(ctx context.Context, songs []netease.Song, n int, reason string) []pick {
	items := make([]Item, 0, len(songs))
	for _, song := range songs {
		items = append(items, fromTrack(source.FromNetEase(song)))
	}
	s.annotate(ctx, items)
	picks := make([]pick, 0, n)
	seen := map[string]bool{}
	for _, i := range rand.Perm(len(items)) {
		it := items[i]
		if it.SongID != 0 || seen[it.Copies[0].ID] {
			continue
		}
		seen[it.Copies[0].ID] = true
		picks = append(picks, pick{Item: it, Reason: reason})
		if len(picks) == n {
			break
		}
	}
	return picks
}

func (s *Server) similarSongs(ctx context.Context, id string) ([]netease.Song, error) {
	n, _ := strconv.ParseInt(id, 10, 64)
	v, err := s.cached(ctx, "similar:"+id, 6*time.Hour, func(ctx context.Context) (any, error) { return s.NetEase.SimilarSongs(ctx, n, 30) })
	if err != nil {
		return nil, err
	}
	return v.([]netease.Song), nil
}

// similarToALike looks at liked NetEase songs whose neighbours are already
// cached and keeps the seed with the most unheard ones; it never waits on the
// network, but warms two more seeds for a later visit. Fewer than three is no block.
func (s *Server) similarToALike(ctx context.Context) (*Item, []pick) {
	likes, err := s.Store.Likes(ctx)
	if err != nil {
		return nil, nil
	}
	var seed Item
	var best []pick
	cold := 0
	for _, i := range rand.Perm(len(likes)) {
		candidate := fromSong(likes[i])
		c, ok := candidate.copy("netease")
		if !ok {
			continue
		}
		v, ok := s.peek("similar:" + c.ID)
		if !ok {
			if cold < 2 {
				cold++
				go func() {
					bg, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					_, _ = s.similarSongs(bg, c.ID)
				}()
			}
			continue
		}
		if picks := s.unknown(ctx, v.([]netease.Song), 7, "相似"); len(picks) > len(best) {
			seed, best = candidate, picks
			if len(best) == 7 {
				break
			}
		}
	}
	if len(best) < 3 { // the mixtape's track list reads fine from three songs
		return nil, nil
	}
	return &seed, best
}

// The private radio hands out three songs a call; a small buffer keeps some
// ready so no page waits on it.
const radioTarget = 24

type fmBuffer struct {
	mu      sync.Mutex
	songs   []netease.Song
	filling bool
}

// takeRadio hands out up to n buffered radio songs and tops the buffer up
// in the background. Only a cold buffer makes the caller wait.
func (s *Server) takeRadio(ctx context.Context, n int) []netease.Song {
	b := &s.radio
	b.mu.Lock()
	empty := len(b.songs) == 0
	b.mu.Unlock()
	if empty {
		s.fillRadio(ctx, n)
	}
	b.mu.Lock()
	k := min(n, len(b.songs))
	out := slices.Clone(b.songs[:k])
	b.songs = b.songs[k:]
	low := len(b.songs) < radioTarget-6
	b.mu.Unlock()
	if low {
		go s.fillRadio(context.Background(), radioTarget)
	}
	return out
}

func (s *Server) fillRadio(ctx context.Context, want int) {
	if s.NetEase == nil || !s.NetEase.HasSession() {
		return
	}
	b := &s.radio
	b.mu.Lock()
	if b.filling {
		b.mu.Unlock()
		return
	}
	b.filling = true
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.filling = false
		b.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	for range want/3 + 2 {
		b.mu.Lock()
		have := len(b.songs)
		b.mu.Unlock()
		if have >= want {
			return
		}
		batch, err := s.NetEase.PersonalFM(ctx)
		if err != nil || len(batch) == 0 {
			return
		}
		b.mu.Lock()
		b.songs = append(b.songs, batch...)
		b.mu.Unlock()
	}
}

// fmMore hands the player the next few radio songs when its queue runs low.
func (s *Server) fmMore(w http.ResponseWriter, r *http.Request) {
	if s.NetEase == nil {
		writeJSON(w, http.StatusOK, map[string]any{"songs": []Item{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"songs": s.fm(r.Context(), 3)})
}

// fm returns a few songs from the account's private radio.
func (s *Server) fm(ctx context.Context, n int) []Item {
	songs := s.takeRadio(ctx, n)
	items := make([]Item, 0, len(songs))
	for _, song := range songs {
		items = append(items, fromTrack(source.FromNetEase(song)))
	}
	s.annotate(ctx, items)
	return items
}
