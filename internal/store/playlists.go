package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Playlists lists every playlist with its size and a few covers for a mosaic.
func (s *Store) Playlists(ctx context.Context) ([]Playlist, error) {
	rows, err := s.db.Query(ctx, `
		SELECT p.id, p.name, p.description, p.cover, p.origin, p.updated_at,
		       (SELECT count(*) FROM playlist_items i WHERE i.playlist_id = p.id),
		       ARRAY(SELECT s.cover FROM playlist_items i JOIN songs s ON s.id = i.song_id
		             WHERE i.playlist_id = p.id AND s.cover <> '' ORDER BY i.position LIMIT 4)
		FROM playlists p ORDER BY p.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Playlist, error) {
		var p Playlist
		err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Cover, &p.Origin, &p.UpdatedAt, &p.Count, &p.Covers)
		p.Covers = placeholderCovers(p.Covers)
		return p, err
	})
}

func (s *Store) Playlist(ctx context.Context, id int64) (Playlist, []Song, error) {
	var p Playlist
	err := s.db.QueryRow(ctx, `SELECT id, name, description, cover, origin, updated_at FROM playlists WHERE id = $1`, id).
		Scan(&p.ID, &p.Name, &p.Description, &p.Cover, &p.Origin, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, nil, ErrNotFound
	}
	if err != nil {
		return p, nil, err
	}
	ids, err := s.ids(ctx, `SELECT song_id FROM playlist_items WHERE playlist_id = $1 ORDER BY position`, id)
	if err != nil {
		return p, nil, err
	}
	songs, err := s.Songs(ctx, ids)
	p.Count = len(songs)
	return p, songs, err
}

// PlaylistByOrigin finds an imported playlist, e.g. origin "netease:123".
func (s *Store) PlaylistByOrigin(ctx context.Context, origin string) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `SELECT id FROM playlists WHERE origin = $1`, origin).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

func (s *Store) CreatePlaylist(ctx context.Context, name, description, cover, origin string) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `INSERT INTO playlists (name, description, cover, origin) VALUES ($1, $2, $3, $4) RETURNING id`,
		name, description, cover, origin).Scan(&id)
	return id, err
}

func (s *Store) UpdatePlaylist(ctx context.Context, id int64, name, description, cover string) error {
	tag, err := s.db.Exec(ctx, `UPDATE playlists SET name = $2, description = $3, cover = $4, updated_at = now() WHERE id = $1`,
		id, name, description, cover)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) DeletePlaylist(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `DELETE FROM playlists WHERE id = $1`, id)
	return err
}

// AddToPlaylist appends songs that are not in the playlist yet.
func (s *Store) AddToPlaylist(ctx context.Context, id int64, songIDs []int64) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var last float64
		if err := tx.QueryRow(ctx, `SELECT coalesce(max(position), 0) FROM playlist_items WHERE playlist_id = $1`, id).Scan(&last); err != nil {
			return err
		}
		for _, songID := range songIDs {
			last++
			if _, err := tx.Exec(ctx, `INSERT INTO playlist_items (playlist_id, song_id, position) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
				id, songID, last); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE playlists SET updated_at = now() WHERE id = $1`, id)
		return err
	})
}

func (s *Store) RemoveFromPlaylist(ctx context.Context, id, songID int64) error {
	_, err := s.db.Exec(ctx, `DELETE FROM playlist_items WHERE playlist_id = $1 AND song_id = $2`, id, songID)
	if err == nil {
		_, err = s.db.Exec(ctx, `UPDATE playlists SET updated_at = now() WHERE id = $1`, id)
	}
	return err
}

// ReplacePlaylist sets the exact contents and order, as an import sync does.
func (s *Store) ReplacePlaylist(ctx context.Context, id int64, songIDs []int64) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM playlist_items WHERE playlist_id = $1`, id); err != nil {
			return err
		}
		seen := map[int64]bool{}
		for i, songID := range songIDs {
			if seen[songID] {
				continue
			}
			seen[songID] = true
			if _, err := tx.Exec(ctx, `INSERT INTO playlist_items (playlist_id, song_id, position) VALUES ($1, $2, $3)`,
				id, songID, float64(i+1)); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE playlists SET updated_at = now() WHERE id = $1`, id)
		return err
	})
}
