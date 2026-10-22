package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"music/internal/catalog"
	"music/internal/lyrics"
	"music/internal/resolve"
	"music/internal/source"
	"music/internal/store"
)

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" || len(query) > 200 {
		fail(w, http.StatusBadRequest, "empty query")
		return
	}
	results := catalog.Search(r.Context(), s.Providers, query, 3*time.Second)
	items := make([]Item, 0, min(len(results), 50))
	for _, res := range results[:min(len(results), 50)] {
		items = append(items, fromResult(res))
	}
	s.annotate(r.Context(), items)
	writeJSON(w, http.StatusOK, map[string]any{"songs": items})
}

// stream picks the platform copy that plays right now.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Item     Item     `json:"item"`
		Retry    string   `json:"retry"`    // "source:id" that just failed in the player
		Rejected []string `json:"rejected"` // all copies rejected during this listen
	}
	if err := readBody(r, &req); err != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	if len(req.Rejected) > 32 {
		fail(w, http.StatusBadRequest, "too many rejected copies")
		return
	}
	if req.Retry != "" {
		req.Rejected = append(req.Rejected, req.Retry)
	}
	item := req.Item
	if item.SongID == 0 {
		items := []Item{item}
		s.annotate(r.Context(), items)
		item = items[0]
	}
	if item.SongID != 0 {
		if song, err := s.Store.Song(r.Context(), item.SongID); err == nil {
			item.Copies = append(song.Sources, item.Copies...)
		}
	}
	t, stream, err := s.Resolver.Resolve(r.Context(), item.SongID, item.want(), item.Copies, req.Rejected...)
	if err != nil {
		if resolve.IsUnavailable(err) {
			fail(w, http.StatusNotFound, "unavailable")
			return
		}
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"url": stream.URL, "source": t.Source, "id": t.ID, "bitrate": stream.Bitrate, "format": stream.Format,
	})
}

func (s *Server) lyrics(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Item Item `json:"item"`
	}
	if err := readBody(r, &req); err != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	item := req.Item
	key := "lyrics:" + item.Title + "|" + strings.Join(item.Artists, "/") + "|" + strconv.Itoa(item.Duration)
	value, err := s.cached(r.Context(), key, time.Hour, func(ctx context.Context) (any, error) { return s.findLyrics(ctx, item), nil })
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, value)
}

// findLyrics prefers NetEase (word timings and translations), then any
// platform copy that has lyrics, then the open LRCLIB database.
func (s *Server) findLyrics(ctx context.Context, item Item) lyrics.Lyrics {
	if s.NetEase != nil {
		t, ok := item.copy("netease")
		if !ok {
			tracks, err := (source.NetEase{Client: s.NetEase}).Search(ctx, item.Title+" "+strings.Join(item.Artists, " "), 8)
			if err == nil {
				for _, candidate := range tracks {
					if source.SameSong(item.want(), candidate) {
						t, ok = candidate, true
						break
					}
				}
			}
		}
		if ok {
			id, _ := strconv.ParseInt(t.ID, 10, 64)
			if raw, err := s.NetEase.Lyrics(ctx, id); err == nil {
				if parsed := lyrics.Parse(raw.LRC, raw.Translation, raw.Words); len(parsed.Lines) > 0 {
					return parsed
				}
			}
		}
	}
	for _, c := range item.Copies {
		for _, p := range s.Providers {
			if l, ok := p.(source.LyricsSource); ok && p.Name() == c.Source {
				if text, err := l.Lyrics(ctx, c); err == nil {
					if parsed := lyrics.Parse(text, "", ""); len(parsed.Lines) > 0 {
						return parsed
					}
				}
			}
		}
	}
	if len(item.Artists) > 0 {
		client := &http.Client{Timeout: 5 * time.Second}
		if text, err := lyrics.LRCLib(ctx, client, item.Title, item.Artists[0], item.Duration); err == nil && text != "" {
			return lyrics.Parse(text, "", "")
		}
	}
	return lyrics.Lyrics{Lines: []lyrics.Line{}}
}

func (s *Server) like(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Item  Item `json:"item"`
		Liked bool `json:"liked"`
	}
	if err := readBody(r, &req); err != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	id, err := s.save(r.Context(), req.Item)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Liked {
		err = s.Store.Like(r.Context(), id)
	} else {
		err = s.Store.Unlike(r.Context(), id)
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.pushLike(r, id, req.Liked)
	writeJSON(w, http.StatusOK, map[string]any{"songId": id, "liked": req.Liked})
}

// pushLike mirrors a heart onto NetEase when the song has a NetEase copy.
func (s *Server) pushLike(r *http.Request, songID int64, liked bool) {
	if s.NetEase == nil || !s.NetEase.HasSession() {
		return
	}
	song, err := s.Store.Song(r.Context(), songID)
	if err != nil {
		return
	}
	for _, c := range song.Sources {
		if c.Source == "netease" {
			id, _ := strconv.ParseInt(c.ID, 10, 64)
			s.background("netease like", func(ctx context.Context) error { return s.NetEase.SetLiked(ctx, id, liked) })
			return
		}
	}
}

func (s *Server) play(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Item    Item   `json:"item"`
		Source  string `json:"source"`
		ID      string `json:"id"`
		Seconds int    `json:"seconds"`
	}
	if err := readBody(r, &req); err != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	id, err := s.save(r.Context(), req.Item)
	if err == nil {
		err = s.Store.RecordPlay(r.Context(), id, req.Source, req.Seconds)
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Source == "netease" && s.NetEase != nil && s.NetEase.HasSession() && req.Seconds >= 30 {
		trackID, _ := strconv.ParseInt(req.ID, 10, 64)
		s.background("netease scrobble", func(ctx context.Context) error { return s.NetEase.Scrobble(ctx, trackID, req.Seconds) })
	}
	writeJSON(w, http.StatusOK, map[string]any{"songId": id})
}

func (s *Server) library(w http.ResponseWriter, r *http.Request) {
	likes, err := s.Store.Likes(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	playlists, err := s.Store.Playlists(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"likes": fromSongs(likes), "playlists": playlists})
}

func (s *Server) pin(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct{ Source, ID string }
	if err := readBody(r, &req); err != nil || id == 0 {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := s.Store.Pin(r.Context(), id, req.Source, req.ID); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) createPlaylist(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name, Description string }
	if err := readBody(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		fail(w, http.StatusBadRequest, "name required")
		return
	}
	id, err := s.Store.CreatePlaylist(r.Context(), strings.TrimSpace(req.Name), req.Description, "", "")
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

func (s *Server) playlist(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	p, songs, err := s.Store.Playlist(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "no such playlist")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"playlist": p, "songs": fromSongs(songs)})
}

func (s *Server) updatePlaylist(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct{ Name, Description, Cover string }
	if err := readBody(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		fail(w, http.StatusBadRequest, "name required")
		return
	}
	if err := s.Store.UpdatePlaylist(r.Context(), id, strings.TrimSpace(req.Name), req.Description, req.Cover); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) deletePlaylist(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.Store.DeletePlaylist(r.Context(), id); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) addToPlaylist(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Items []Item `json:"items"`
	}
	if err := readBody(r, &req); err != nil || len(req.Items) == 0 {
		fail(w, http.StatusBadRequest, "items required")
		return
	}
	var songIDs []int64
	for _, it := range req.Items {
		songID, err := s.save(r.Context(), it)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		songIDs = append(songIDs, songID)
	}
	if err := s.Store.AddToPlaylist(r.Context(), id, songIDs); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.pushPlaylist(r, id, songIDs, true)
	writeJSON(w, http.StatusOK, map[string]any{"songIds": songIDs})
}

func (s *Server) removeFromPlaylist(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	songID, _ := strconv.ParseInt(r.PathValue("song"), 10, 64)
	if err := s.Store.RemoveFromPlaylist(r.Context(), id, songID); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.pushPlaylist(r, id, []int64{songID}, false)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// pushPlaylist mirrors edits of an imported NetEase playlist back to it.
func (s *Server) pushPlaylist(r *http.Request, playlistID int64, songIDs []int64, add bool) {
	if s.NetEase == nil || !s.NetEase.HasSession() {
		return
	}
	p, _, err := s.Store.Playlist(r.Context(), playlistID)
	remote, ok := strings.CutPrefix(p.Origin, "netease:")
	if err != nil || !ok {
		return
	}
	pid, _ := strconv.ParseInt(remote, 10, 64)
	songs, err := s.Store.Songs(r.Context(), songIDs)
	if err != nil {
		return
	}
	var ids []int64
	for _, song := range songs {
		for _, c := range song.Sources {
			if c.Source == "netease" {
				n, _ := strconv.ParseInt(c.ID, 10, 64)
				ids = append(ids, n)
				break
			}
		}
	}
	if len(ids) > 0 {
		s.background("netease playlist", func(ctx context.Context) error { return s.NetEase.EditPlaylist(ctx, pid, add, ids) })
	}
}
