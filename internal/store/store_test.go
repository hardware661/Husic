package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hardware661/husic/internal/source"
)

// Every test runs on SQLite, and also on PostgreSQL when HUSIC_TEST_POSTGRES
// names an empty database that the test may fill and drop tables in.
func engines(t *testing.T) map[string]*Store {
	t.Helper()
	ctx := context.Background()
	out := map[string]*Store{}
	lite, err := Open(ctx, "sqlite:"+filepath.Join(t.TempDir(), "husic.db"))
	if err != nil {
		t.Fatal(err)
	}
	out["sqlite"] = lite
	if dsn := os.Getenv("HUSIC_TEST_POSTGRES"); dsn != "" {
		pg, err := Open(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		reset(t, pg.db)
		out["postgres"] = pg
	}
	for name, s := range out {
		if err := s.Migrate(ctx); err != nil {
			t.Fatalf("%s migrate: %v", name, err)
		}
		t.Cleanup(s.Close)
	}
	return out
}

func reset(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`DROP TABLE IF EXISTS kv, plays, playlist_items, playlists, likes, song_sources, songs, schema_migrations CASCADE`)
	if err != nil {
		t.Fatal(err)
	}
}

func track(src, id, title string) source.Track {
	return source.Track{Source: src, ID: id, Title: title, Artists: []string{"Artist", "Guest"}, Album: "Album", Duration: 200, Cover: "https://img.example/" + id, Extra: map[string]string{"k": "v"}}
}

func TestLibraryRoundTrip(t *testing.T) {
	for name, s := range engines(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if err := s.Migrate(ctx); err != nil {
				t.Fatal("second migrate must be a no-op:", err)
			}
			a, err := s.SaveTrack(ctx, track("kuwo", "1", "First"))
			if err != nil {
				t.Fatal(err)
			}
			again, err := s.SaveTrack(ctx, track("kuwo", "1", "First (renamed)"))
			if err != nil || again != a {
				t.Fatalf("same track became song %d (%v)", again, err)
			}
			if err := s.LinkTrack(ctx, a, track("qq", "x", "First")); err != nil {
				t.Fatal(err)
			}
			b, _ := s.SaveTrack(ctx, track("kugou", "2", "Second"))
			if err := s.Pin(ctx, a, "qq", "x"); err != nil {
				t.Fatal(err)
			}
			if err := s.MarkSource(ctx, "kuwo", "1", false, "gone"); err != nil {
				t.Fatal(err)
			}
			if err := s.Like(ctx, a); err != nil {
				t.Fatal(err)
			}
			if err := s.Like(ctx, a); err != nil {
				t.Fatal("liking twice must be fine:", err)
			}
			songs, err := s.Songs(ctx, []int64{b, a})
			if err != nil || len(songs) != 2 || songs[0].ID != b || songs[1].ID != a {
				t.Fatalf("order: %+v %v", songs, err)
			}
			first := songs[1]
			if !first.Liked || len(first.Sources) != 2 || first.Sources[0].Source != "qq" || !first.Sources[0].Pinned {
				t.Fatalf("song: %+v", first)
			}
			if strings.Join(first.Sources[1].Artists, "/") != "Artist/Guest" || first.Sources[1].Extra["k"] != "v" || first.Sources[1].Title != "First (renamed)" {
				t.Fatalf("copy: %+v", first.Sources[1])
			}
			ids, liked, err := s.Lookup(ctx, []source.Track{track("qq", "x", ""), track("kugou", "2", ""), track("migu", "none", "")})
			if err != nil || ids["qq:x"] != a || ids["kugou:2"] != b || len(ids) != 2 || !liked[a] || liked[b] {
				t.Fatalf("lookup: %v %v %v", ids, liked, err)
			}
			if err := s.RecordPlay(ctx, a, "qq", 30); err != nil {
				t.Fatal(err)
			}
			time.Sleep(5 * time.Millisecond)
			_ = s.RecordPlay(ctx, b, "kugou", 30)
			recent, times, err := s.RecentPlays(ctx, 10)
			if err != nil || len(recent) != 2 || recent[0].ID != b || times[a].IsZero() || time.Since(times[a]) > time.Minute {
				t.Fatalf("recent: %+v %v %v", recent, times, err)
			}
			likes, err := s.Likes(ctx)
			if err != nil || len(likes) != 1 || likes[0].ID != a {
				t.Fatalf("likes: %+v %v", likes, err)
			}
			if err := s.Unlike(ctx, a); err != nil {
				t.Fatal(err)
			}

			p, err := s.CreatePlaylist(ctx, "Mix", "", "", "netease:9")
			if err != nil {
				t.Fatal(err)
			}
			if found, err := s.PlaylistByOrigin(ctx, "netease:9"); err != nil || found != p {
				t.Fatalf("origin: %d %v", found, err)
			}
			if err := s.AddToPlaylist(ctx, p, []int64{b, a, b}); err != nil {
				t.Fatal(err)
			}
			list, err := s.Playlists(ctx)
			if err != nil || len(list) != 1 || list[0].Count != 2 || len(list[0].Covers) != 2 || list[0].UpdatedAt.IsZero() {
				t.Fatalf("playlists: %+v %v", list, err)
			}
			if err := s.ReplacePlaylist(ctx, p, []int64{a, b, a}); err != nil {
				t.Fatal(err)
			}
			info, items, err := s.Playlist(ctx, p)
			if err != nil || info.Name != "Mix" || len(items) != 2 || items[0].ID != a {
				t.Fatalf("playlist: %+v %+v %v", info, items, err)
			}
			if err := s.UpdatePlaylist(ctx, p, "Renamed", "d", ""); err != nil {
				t.Fatal(err)
			}
			if err := s.UpdatePlaylist(ctx, p+100, "x", "", ""); err != ErrNotFound {
				t.Fatalf("missing playlist: %v", err)
			}
			if err := s.RemoveFromPlaylist(ctx, p, a); err != nil {
				t.Fatal(err)
			}
			if err := s.DeletePlaylist(ctx, p); err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.Playlist(ctx, p); err != ErrNotFound {
				t.Fatalf("deleted playlist: %v", err)
			}

			var got map[string]int
			if err := s.GetKV(ctx, "sync", &got); err != ErrNotFound {
				t.Fatalf("empty kv: %v", err)
			}
			_ = s.SetKV(ctx, "sync", map[string]int{"n": 1})
			_ = s.SetKV(ctx, "sync", map[string]int{"n": 2})
			if err := s.GetKV(ctx, "sync", &got); err != nil || got["n"] != 2 {
				t.Fatalf("kv: %v %v", got, err)
			}
		})
	}
}

func TestSavingTheSameNewTrackAtOnceGivesOneSong(t *testing.T) {
	for name, s := range engines(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			const n = 8
			ids := make([]int64, n)
			errs := make([]error, n)
			var wg sync.WaitGroup
			start := make(chan struct{})
			for i := range n {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					ids[i], errs[i] = s.SaveTrack(ctx, track("kuwo", "same", "Song"))
				}()
			}
			close(start)
			wg.Wait()
			for i := range n {
				if errs[i] != nil || ids[i] != ids[0] {
					t.Fatalf("saves: %v %v", ids, errs)
				}
			}
			song, err := s.Song(ctx, ids[0])
			if err != nil || len(song.Sources) != 1 {
				t.Fatalf("song without its copy: %+v %v", song, err)
			}
			var orphans int
			_ = s.db.QueryRow(`SELECT count(*) FROM songs WHERE id NOT IN (SELECT song_id FROM song_sources)`).Scan(&orphans)
			if orphans != 0 {
				t.Fatalf("%d songs were left without any copy", orphans)
			}
		})
	}
}
