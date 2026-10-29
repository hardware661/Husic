// Package api is Husic's HTTP surface: JSON under /api, the web app elsewhere.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hardware661/husic/internal/accounts"
	"github.com/hardware661/husic/internal/catalog"
	"github.com/hardware661/husic/internal/netease"
	"github.com/hardware661/husic/internal/resolve"
	"github.com/hardware661/husic/internal/source"
	"github.com/hardware661/husic/internal/store"
)

type Server struct {
	Accounts  *accounts.Manager
	Store     *store.Store
	Resolver  *resolve.Resolver
	Providers []source.Provider // searched together, best first
	Platforms map[string]PlatformLibrary
	NetEase   *netease.Client
	// GatewayHeader/GatewayKey: the edge proxy proves the request passed the
	// shared login. Without a key only loopback clients are served, unless
	// AllowRemote says the network in front is already private (a container
	// port published on localhost, a home LAN, a VPN).
	GatewayHeader string
	GatewayKey    string
	AllowRemote   bool
	WebDir        string
	YouTube       interface {
		http.Handler
		Status() (bool, bool)
	}
	Log *slog.Logger

	cacheMu sync.Mutex
	cache   map[string]cacheEntry
	radio   fmBuffer
}

type cacheEntry struct {
	value      any
	until      time.Time
	refreshing bool
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.accountRoutes(mux)
	s.platformRoutes(mux)
	mux.HandleFunc("GET /api/search", s.search)
	mux.HandleFunc("GET /api/suggestions", s.suggestions)
	mux.HandleFunc("POST /api/stream", s.stream)
	mux.HandleFunc("POST /api/download", s.download)
	mux.HandleFunc("POST /api/lyrics", s.lyrics)
	mux.HandleFunc("POST /api/likes", s.like)
	mux.HandleFunc("POST /api/plays", s.play)
	mux.HandleFunc("GET /api/library", s.library)
	mux.HandleFunc("POST /api/songs/{id}/pin", s.pin)
	mux.HandleFunc("POST /api/playlists", s.createPlaylist)
	mux.HandleFunc("GET /api/playlists/{id}", s.playlist)
	mux.HandleFunc("PATCH /api/playlists/{id}", s.updatePlaylist)
	mux.HandleFunc("DELETE /api/playlists/{id}", s.deletePlaylist)
	mux.HandleFunc("POST /api/playlists/{id}/songs", s.addToPlaylist)
	mux.HandleFunc("DELETE /api/playlists/{id}/songs/{song}", s.removeFromPlaylist)
	mux.HandleFunc("GET /api/home", s.home)
	mux.HandleFunc("GET /api/discover", s.discover)
	mux.HandleFunc("GET /api/fm", s.fmMore)
	mux.HandleFunc("GET /api/radio", s.radioMore)
	mux.HandleFunc("GET /api/charts", s.charts)
	mux.HandleFunc("GET /api/charts/{source}/{id}", s.chart)
	mux.HandleFunc("GET /api/netease/playlists/{id}", s.neteasePlaylist)
	mux.HandleFunc("POST /api/sync/netease", s.syncNetEase)
	if s.YouTube != nil {
		mux.Handle("GET /api/media/{token}", s.YouTube)
	}
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		status := map[string]any{"ok": true}
		if s.YouTube != nil {
			ready, expired := s.YouTube.Status()
			status["youtube"] = map[string]bool{"configured": ready, "sessionExpired": expired}
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, http.StatusNotFound, "not found") })
	mux.Handle("/", s.static())
	return s.gate(mux)
}

func (s *Server) gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.GatewayKey != "" {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get(s.GatewayHeader)), []byte(s.GatewayKey)) != 1 {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		} else if host, _, err := net.SplitHostPort(r.RemoteAddr); !s.AllowRemote && (err != nil || !net.ParseIP(host).IsLoopback()) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// static serves the built web app; unknown paths get index.html so the
// app's own routes survive a reload.
func (s *Server) static() http.Handler {
	files := http.FileServer(http.Dir(s.WebDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(s.WebDir, filepath.Clean("/"+r.URL.Path))
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, filepath.Join(s.WebDir, "index.html"))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

// Item is a song as the app shows it: library songs carry SongID; search
// and recommendation results only carry their platform copies.
type Item struct {
	SongID   int64          `json:"songId,omitempty"`
	Title    string         `json:"title"`
	Artists  []string       `json:"artists"`
	Album    string         `json:"album"`
	Duration int            `json:"duration"`
	Cover    string         `json:"cover"`
	Copies   []source.Track `json:"copies"`
	Liked    bool           `json:"liked"`
	PlayedAt *time.Time     `json:"playedAt,omitempty"`
}

func fromSong(song store.Song) Item {
	return Item{SongID: song.ID, Title: song.Title, Artists: song.Artists, Album: song.Album, Duration: song.Duration,
		Cover: song.Cover, Copies: song.Sources, Liked: song.Liked}
}

func fromTrack(t source.Track) Item {
	return Item{Title: t.Title, Artists: nonNil(t.Artists), Album: t.Album, Duration: t.Duration, Cover: t.Cover, Copies: []source.Track{t}}
}

func fromResult(r catalog.Result) Item {
	item := fromTrack(r.Track)
	item.Copies = r.Copies
	return item
}

func fromSongs(songs []store.Song) []Item {
	items := make([]Item, 0, len(songs))
	for _, song := range songs {
		items = append(items, fromSong(song))
	}
	return items
}

// want is the recording an item stands for, used to match other platforms.
func (it Item) want() source.Track {
	return source.Track{Title: it.Title, Artists: it.Artists, Album: it.Album, Duration: it.Duration}
}

func (it Item) copy(src string) (source.Track, bool) {
	for _, c := range it.Copies {
		if c.Source == src {
			return c, true
		}
	}
	return source.Track{}, false
}

// annotate fills in library song IDs and hearts for transient items.
func (s *Server) annotate(ctx context.Context, items []Item) {
	var tracks []source.Track
	for _, it := range items {
		tracks = append(tracks, it.Copies...)
	}
	songs, liked, err := s.Store.Lookup(ctx, tracks)
	if err != nil {
		s.Log.Warn("lookup", "err", err)
		return
	}
	for i := range items {
		for _, c := range items[i].Copies {
			if id, ok := songs[c.Ref()]; ok {
				items[i].SongID, items[i].Liked = id, liked[id]
				break
			}
		}
	}
}

// save puts an item into the library (or finds it there) with all its copies.
func (s *Server) save(ctx context.Context, it Item) (int64, error) {
	if it.SongID != 0 {
		return it.SongID, nil
	}
	if len(it.Copies) == 0 {
		return 0, errors.New("item has no platform copy")
	}
	first := it.Copies[0]
	if first.Cover == "" {
		first.Cover = it.Cover
	}
	id, err := s.Store.SaveTrack(ctx, first)
	if err != nil {
		return 0, err
	}
	for _, c := range it.Copies[1:] {
		if err := s.Store.LinkTrack(ctx, id, c); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// cached memoises slow upstream answers. Past its time an answer is still
// served at once while a fresh one loads in the background; only the very
// first load makes a caller wait, and it finishes even if that caller leaves.
func (s *Server) cached(ctx context.Context, key string, ttl time.Duration, load func(context.Context) (any, error)) (any, error) {
	s.cacheMu.Lock()
	if s.cache == nil {
		s.cache = map[string]cacheEntry{}
	}
	hit, ok := s.cache[key]
	stale := ok && time.Now().After(hit.until) && !hit.refreshing
	if stale {
		hit.refreshing = true
		s.cache[key] = hit
	}
	s.cacheMu.Unlock()
	if stale {
		go func() {
			bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			value, err := load(bg)
			s.cacheMu.Lock()
			defer s.cacheMu.Unlock()
			if err != nil {
				hit.refreshing = false // a later request tries again
				s.cache[key] = hit
				return
			}
			s.cache[key] = cacheEntry{value: value, until: time.Now().Add(ttl)}
		}()
	}
	if ok {
		return hit.value, nil
	}
	lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	value, err := load(lctx)
	if err != nil {
		return nil, err
	}
	s.cacheMu.Lock()
	s.cache[key] = cacheEntry{value: value, until: time.Now().Add(ttl)}
	if len(s.cache) > cacheSweepSize {
		s.sweepLocked()
	}
	s.cacheMu.Unlock()
	return value, nil
}

// Keys include per-song and per-day parts, so the map would only grow.
// Answers a day past their time are no longer useful even as stale ones.
const cacheSweepSize = 2048

func (s *Server) sweepLocked() {
	cutoff := time.Now().Add(-24 * time.Hour)
	for key, entry := range s.cache {
		if entry.until.Before(cutoff) && !entry.refreshing {
			delete(s.cache, key)
		}
	}
}

// peek returns a cached answer, fresh or stale, without loading anything.
func (s *Server) peek(key string) (any, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	hit, ok := s.cache[key]
	return hit.value, ok
}

// background runs account write-backs without making the listener wait.
func (s *Server) background(what string, fn func(ctx context.Context) error) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			s.Log.Warn(what, "err", err)
		}
	}()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func readBody(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v)
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
