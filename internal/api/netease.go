package api

import (
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/hardware661/husic/internal/netease"
	"github.com/hardware661/husic/internal/source"
	"github.com/hardware661/husic/internal/store"
)

var errNoAccount = errors.New("no account for this platform")

// card is a remote playlist the app can open (recommendations, charts).
type card struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Cover  string `json:"cover"`
	Count  int    `json:"count"`
	Note   string `json:"note,omitempty"`
}

func neteaseCards(list []netease.PlaylistInfo) []card {
	cards := make([]card, 0, len(list))
	for _, p := range list {
		cards = append(cards, card{Source: "netease", ID: strconv.FormatInt(p.ID, 10), Name: p.Name, Cover: p.CoverURL(), Count: p.TrackCount, Note: p.Copywriter})
	}
	return cards
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := map[string]any{"daily": []Item{}, "picks": []card{}, "recent": []Item{}, "playlists": []any{}, "fm": []Item{}, "liked": []Item{}}
	if recent, times, err := s.Store.RecentPlays(ctx, 24); err == nil {
		items := fromSongs(recent)
		for i := range items {
			if at, ok := times[items[i].SongID]; ok {
				items[i].PlayedAt = &at
			}
		}
		out["recent"] = items
	}
	if likes, err := s.Store.Likes(ctx); err == nil {
		out["liked"] = homeLikes(likes, beijingDay())
	}
	if mine, err := s.Store.Playlists(ctx); err == nil {
		out["playlists"] = mine
	}
	if s.NetEase != nil {
		if daily, err := s.dailyItems(ctx); err == nil {
			items := slices.Clone(daily)
			s.annotate(ctx, items)
			out["daily"] = items
		} else {
			s.Log.Warn("daily songs", "err", err)
		}
		if picks, err := s.recommendedCards(ctx); err == nil {
			out["picks"] = picks
		}
		out["fm"] = s.fm(ctx, 3)
	}
	feeds := s.recommendationFeeds(ctx)
	recommended := blendRecommendations(feeds, 30, false)
	items := make([]Item, len(recommended))
	for i := range recommended {
		items[i] = recommended[i].Item
	}
	s.annotate(ctx, items)
	for i := range recommended {
		recommended[i].Item = items[i]
	}
	out["recommendations"] = recommended
	out["feeds"] = feeds
	writeJSON(w, http.StatusOK, out)
}

func homeLikes(likes []store.Song, day string) []Item {
	likes = slices.Clone(likes)
	// Rank by day and song identity: navigation, refresh and server restarts
	// must not redraw the quilt. Membership changes still take effect at once.
	score := func(song store.Song) string {
		sum := sha256.Sum256([]byte(day + ":" + strconv.FormatInt(song.ID, 10)))
		return string(sum[:])
	}
	slices.SortFunc(likes, func(a, b store.Song) int { return cmp.Compare(score(a), score(b)) })
	return fromSongs(likes[:min(len(likes), 8)])
}

// neteasePlaylist opens a NetEase playlist without importing it.
func (s *Server) neteasePlaylist(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if s.NetEase == nil || id == 0 {
		fail(w, http.StatusNotFound, "no NetEase account")
		return
	}
	value, err := s.cached(r.Context(), "netease-playlist:"+r.PathValue("id"), 10*time.Minute, func(ctx context.Context) (any, error) {
		info, ids, err := s.NetEase.PlaylistDetail(ctx, id)
		if err != nil {
			return nil, err
		}
		items, err := s.neteaseItems(ctx, ids[:min(len(ids), 500)])
		return map[string]any{"playlist": neteaseCards([]netease.PlaylistInfo{info})[0], "songs": items}, err
	})
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	out := value.(map[string]any)
	items := slices.Clone(out["songs"].([]Item))
	s.annotate(r.Context(), items)
	writeJSON(w, http.StatusOK, map[string]any{"playlist": out["playlist"], "songs": items})
}

// neteaseItems looks songs up in batches.
func (s *Server) neteaseItems(ctx context.Context, ids []int64) ([]Item, error) {
	items := make([]Item, 0, len(ids))
	for batch := range slices.Chunk(ids, 400) {
		songs, privileges, err := s.NetEase.SongDetails(ctx, batch)
		if err != nil {
			return nil, err
		}
		// Songs NetEase removed (privilege st < 0) stay: other platforms may have them.
		_ = privileges
		for _, song := range songs {
			items = append(items, fromTrack(source.FromNetEase(song)))
		}
	}
	return items, nil
}

// syncNetEase imports the account's liked songs and its own playlists.
func (s *Server) syncNetEase(w http.ResponseWriter, r *http.Request) {
	if s.NetEase == nil || !s.NetEase.HasSession() {
		fail(w, http.StatusNotFound, "no NetEase account")
		return
	}
	ctx := r.Context()
	likedIDs, err := s.NetEase.LikedIDs(ctx, s.NetEase.UserID())
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	liked, err := s.neteaseItems(ctx, likedIDs)
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	// Oldest first, so the newest NetEase like ends up newest here too.
	for i := len(liked) - 1; i >= 0; i-- {
		id, err := s.save(ctx, liked[i])
		if err == nil {
			err = s.Store.Like(ctx, id)
		}
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	lists, err := s.NetEase.UserPlaylists(ctx, s.NetEase.UserID())
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	imported := 0
	for _, p := range lists {
		if p.SpecialType == 5 || p.Creator.UserID != s.NetEase.UserID() || p.Subscribed {
			continue
		}
		info, ids, err := s.NetEase.PlaylistDetail(ctx, p.ID)
		if err != nil {
			fail(w, http.StatusBadGateway, err.Error())
			return
		}
		items, err := s.neteaseItems(ctx, ids)
		if err != nil {
			fail(w, http.StatusBadGateway, err.Error())
			return
		}
		origin := "netease:" + strconv.FormatInt(p.ID, 10)
		playlistID, err := s.Store.PlaylistByOrigin(ctx, origin)
		if err != nil {
			playlistID, err = s.Store.CreatePlaylist(ctx, p.Name, info.Copywriter, info.CoverURL(), origin)
		} else {
			err = s.Store.UpdatePlaylist(ctx, playlistID, p.Name, info.Copywriter, info.CoverURL())
		}
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		songIDs := make([]int64, 0, len(items))
		for _, it := range items {
			id, err := s.save(ctx, it)
			if err != nil {
				fail(w, http.StatusInternalServerError, err.Error())
				return
			}
			songIDs = append(songIDs, id)
		}
		if err := s.Store.ReplacePlaylist(ctx, playlistID, songIDs); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		imported++
	}
	writeJSON(w, http.StatusOK, map[string]int{"liked": len(liked), "playlists": imported})
}
