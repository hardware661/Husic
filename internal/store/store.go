// Package store keeps the library: songs and their platform copies, likes,
// playlists and listening history. It runs on PostgreSQL or on a single
// SQLite file; both hold the same tables, see migrations/.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx"
	_ "modernc.org/sqlite"             // registers "sqlite"

	"github.com/hardware661/husic/internal/source"
)

//go:embed migrations/*.sql migrations/sqlite/*.sql
var migrations embed.FS

var ErrNotFound = errors.New("not found")

type Store struct {
	db     *sql.DB
	sqlite bool
}

// Song is a recording with every platform copy Husic knows about.
type Song struct {
	ID       int64          `json:"id"`
	Title    string         `json:"title"`
	Artists  []string       `json:"artists"`
	Album    string         `json:"album"`
	Duration int            `json:"duration"`
	Cover    string         `json:"cover"`
	Sources  []source.Track `json:"sources"`
	Liked    bool           `json:"liked"`
}

type Playlist struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Cover       string    `json:"cover"`
	Origin      string    `json:"origin"`
	Count       int       `json:"count"`
	Covers      []string  `json:"covers"` // first few song covers, for a mosaic
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Open connects to the library. A postgres:// or postgresql:// URL (or a
// key=value connection string) uses PostgreSQL; sqlite:PATH uses one file.
func Open(ctx context.Context, dsn string) (*Store, error) {
	s := &Store{}
	var err error
	if path, ok := strings.CutPrefix(dsn, "sqlite:"); ok {
		if path == "" {
			return nil, errors.New("sqlite: needs a file path, e.g. sqlite:/data/husic.db")
		}
		s.sqlite = true
		// One writer at a time; foreign keys and a busy timeout per connection.
		s.db, err = sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate")
		if err == nil {
			s.db.SetMaxOpenConns(1)
		}
	} else {
		s.db, err = sql.Open("pgx", dsn)
	}
	if err != nil {
		return nil, err
	}
	if err := s.db.PingContext(ctx); err != nil {
		s.db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() { s.db.Close() }

// Migrate applies the embedded migrations that have not run yet, in order.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY, applied_at `+s.timeType()+` NOT NULL DEFAULT (`+s.now()+`))`); err != nil {
		return err
	}
	dir := "migrations"
	if s.sqlite {
		dir = "migrations/sqlite"
	}
	entries, err := fs.ReadDir(migrations, dir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var done bool
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		text, err := migrations.ReadFile(dir + "/" + name)
		if err != nil {
			return err
		}
		err = s.tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(text)); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name)
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// SaveTrack returns the song a platform track belongs to, creating the song
// on first sight. Metadata of an existing copy is refreshed. Two requests
// saving the same new track at once end up with the same song.
func (s *Store) SaveTrack(ctx context.Context, t source.Track) (int64, error) {
	for attempt := 0; ; attempt++ {
		var id int64
		err := s.tx(ctx, func(tx *sql.Tx) error {
			err := tx.QueryRowContext(ctx, `SELECT song_id FROM song_sources WHERE source = $1 AND source_id = $2`, t.Source, t.ID).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				err = tx.QueryRowContext(ctx, `INSERT INTO songs (title, artists, album, duration, cover) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
					t.Title, s.list(t.Artists), t.Album, t.Duration, t.Cover).Scan(&id)
				if err != nil {
					return err
				}
				// Another request may have claimed the track meanwhile: then
				// this transaction rolls back and the next attempt finds it.
				res, err := tx.ExecContext(ctx, `INSERT INTO song_sources (source, source_id, song_id, title) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
					t.Source, t.ID, id, t.Title)
				if err != nil {
					return err
				}
				if n, _ := res.RowsAffected(); n == 0 {
					return errClaimed
				}
			} else if err != nil {
				return err
			}
			return s.upsertSource(ctx, tx, id, t)
		})
		if errors.Is(err, errClaimed) && attempt < 3 {
			continue
		}
		return id, err
	}
}

var errClaimed = errors.New("track was saved concurrently")

// LinkTrack records another platform copy of an existing song. A track that
// already belongs to a different song is left where it is.
func (s *Store) LinkTrack(ctx context.Context, songID int64, t source.Track) error {
	var current int64
	err := s.db.QueryRowContext(ctx, `SELECT song_id FROM song_sources WHERE source = $1 AND source_id = $2`, t.Source, t.ID).Scan(&current)
	if err == nil && current != songID {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return s.tx(ctx, func(tx *sql.Tx) error { return s.upsertSource(ctx, tx, songID, t) })
}

func (s *Store) upsertSource(ctx context.Context, tx *sql.Tx, songID int64, t source.Track) error {
	extra, _ := json.Marshal(t.Extra)
	if t.Extra == nil {
		extra = []byte("{}")
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO song_sources (source, source_id, song_id, title, artists, album, duration, cover, extra)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (source, source_id) DO UPDATE SET
			title = EXCLUDED.title, artists = EXCLUDED.artists, album = EXCLUDED.album,
			duration = EXCLUDED.duration, cover = CASE WHEN EXCLUDED.cover <> '' THEN EXCLUDED.cover ELSE song_sources.cover END,
			extra = EXCLUDED.extra`,
		t.Source, t.ID, songID, t.Title, s.list(t.Artists), t.Album, t.Duration, t.Cover, s.object(extra))
	return err
}

// Pin makes one platform copy the one to try first for its song.
func (s *Store) Pin(ctx context.Context, songID int64, src, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE song_sources SET pinned = (source = $2 AND source_id = $3) WHERE song_id = $1`, songID, src, id)
	return err
}

// MarkSource remembers whether a copy played, so broken copies sink.
func (s *Store) MarkSource(ctx context.Context, src, id string, ok bool, reason string) error {
	if ok {
		_, err := s.db.ExecContext(ctx, `UPDATE song_sources SET last_ok = `+s.now()+`, fail_reason = '' WHERE source = $1 AND source_id = $2`, src, id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE song_sources SET last_fail = `+s.now()+`, fail_reason = $3 WHERE source = $1 AND source_id = $2`, src, id, reason)
	return err
}

// Songs loads songs with their copies, keeping the order of ids.
func (s *Store) Songs(ctx context.Context, ids []int64) ([]Song, error) {
	if len(ids) == 0 {
		return []Song{}, nil
	}
	in, args := s.in(ids)
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.title, s.artists, s.album, s.duration, s.cover, (l.song_id IS NOT NULL)
		FROM songs s LEFT JOIN likes l ON l.song_id = s.id WHERE s.id `+in, args...)
	if err != nil {
		return nil, err
	}
	byID := map[int64]*Song{}
	for rows.Next() {
		var song Song
		if err := rows.Scan(&song.ID, &song.Title, s.listInto(&song.Artists), &song.Album, &song.Duration, &song.Cover, &song.Liked); err != nil {
			rows.Close()
			return nil, err
		}
		song.Sources = []source.Track{}
		byID[song.ID] = &song
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = s.db.QueryContext(ctx, `
		SELECT song_id, source, source_id, title, artists, album, duration, cover, extra, pinned
		FROM song_sources WHERE song_id `+in+`
		ORDER BY pinned DESC, (last_fail IS NOT NULL AND (last_ok IS NULL OR last_fail > last_ok)), source`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var songID int64
		var t source.Track
		var extra []byte
		if err := rows.Scan(&songID, &t.Source, &t.ID, &t.Title, s.listInto(&t.Artists), &t.Album, &t.Duration, &t.Cover, &extra, &t.Pinned); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(extra, &t.Extra)
		if song := byID[songID]; song != nil {
			song.Sources = append(song.Sources, t)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Song, 0, len(ids))
	for _, id := range ids {
		if song := byID[id]; song != nil {
			out = append(out, *song)
		}
	}
	return out, nil
}

func (s *Store) Song(ctx context.Context, id int64) (Song, error) {
	songs, err := s.Songs(ctx, []int64{id})
	if err != nil {
		return Song{}, err
	}
	if len(songs) == 0 {
		return Song{}, ErrNotFound
	}
	return songs[0], nil
}

func (s *Store) ids(ctx context.Context, query string, args ...any) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) Like(ctx context.Context, songID int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO likes (song_id) VALUES ($1) ON CONFLICT DO NOTHING`, songID)
	return err
}

func (s *Store) Unlike(ctx context.Context, songID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM likes WHERE song_id = $1`, songID)
	return err
}

// Likes lists liked songs, newest first.
func (s *Store) Likes(ctx context.Context) ([]Song, error) {
	ids, err := s.ids(ctx, `SELECT song_id FROM likes ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	return s.Songs(ctx, ids)
}

func (s *Store) RecordPlay(ctx context.Context, songID int64, src string, seconds int) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO plays (song_id, source, seconds) VALUES ($1, $2, $3)`, songID, src, seconds)
	return err
}

// Recent lists recently played songs, each once, newest first.
func (s *Store) Recent(ctx context.Context, limit int) ([]Song, error) {
	songs, _, err := s.RecentPlays(ctx, limit)
	return songs, err
}

// RecentPlays is Recent with the time each song was last played.
func (s *Store) RecentPlays(ctx context.Context, limit int) ([]Song, map[int64]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT song_id, max(played_at) AS at FROM plays GROUP BY song_id ORDER BY at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, nil, err
	}
	var ids []int64
	times := map[int64]time.Time{}
	for rows.Next() {
		var id int64
		var at timeValue
		if err := rows.Scan(&id, &at); err != nil {
			rows.Close()
			return nil, nil, err
		}
		ids, times[id] = append(ids, id), at.Time
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	songs, err := s.Songs(ctx, ids)
	return songs, times, err
}

func (s *Store) GetKV(ctx context.Context, key string, out any) error {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = $1`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (s *Store) SetKV(ctx context.Context, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO kv (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = `+s.now(), key, s.object(raw))
	return err
}

func placeholderCovers(covers []string) []string {
	out := covers[:0]
	for _, c := range covers {
		if strings.TrimSpace(c) != "" {
			out = append(out, c)
		}
	}
	return out
}

// Lookup maps platform tracks to the library songs they belong to and tells
// which of those songs are liked.
func (s *Store) Lookup(ctx context.Context, tracks []source.Track) (map[string]int64, map[int64]bool, error) {
	songs, liked := map[string]int64{}, map[int64]bool{}
	if len(tracks) == 0 {
		return songs, liked, nil
	}
	pairs := make([]string, 0, len(tracks))
	args := make([]any, 0, 2*len(tracks))
	for _, t := range tracks {
		pairs = append(pairs, fmt.Sprintf("($%d, $%d)", len(args)+1, len(args)+2))
		args = append(args, t.Source, t.ID)
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH q (source, source_id) AS (VALUES `+strings.Join(pairs, ", ")+`)
		SELECT ss.source, ss.source_id, ss.song_id, (l.song_id IS NOT NULL)
		FROM q JOIN song_sources ss ON ss.source = q.source AND ss.source_id = q.source_id
		LEFT JOIN likes l ON l.song_id = ss.song_id`, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var src, id string
		var songID int64
		var isLiked bool
		if err := rows.Scan(&src, &id, &songID, &isLiked); err != nil {
			return nil, nil, err
		}
		songs[src+":"+id] = songID
		liked[songID] = isLiked
	}
	return songs, liked, rows.Err()
}
