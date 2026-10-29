-- SQLite form of ../001_library.sql. Lists (artists) and extra fields are
-- stored as JSON text; times are UTC 'YYYY-MM-DD HH:MM:SS.SSS' text.
CREATE TABLE songs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT    NOT NULL,
    artists    TEXT    NOT NULL DEFAULT '[]',
    album      TEXT    NOT NULL DEFAULT '',
    duration   INTEGER NOT NULL DEFAULT 0, -- seconds
    cover      TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now'))
);

CREATE TABLE song_sources (
    source      TEXT    NOT NULL,
    source_id   TEXT    NOT NULL,
    song_id     INTEGER NOT NULL REFERENCES songs (id) ON DELETE CASCADE,
    title       TEXT    NOT NULL,
    artists     TEXT    NOT NULL DEFAULT '[]',
    album       TEXT    NOT NULL DEFAULT '',
    duration    INTEGER NOT NULL DEFAULT 0,
    cover       TEXT    NOT NULL DEFAULT '',
    extra       TEXT    NOT NULL DEFAULT '{}',
    pinned      INTEGER NOT NULL DEFAULT 0,
    last_ok     TEXT,
    last_fail   TEXT,
    fail_reason TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (source, source_id)
);
CREATE INDEX song_sources_song ON song_sources (song_id);

CREATE TABLE likes (
    song_id    INTEGER PRIMARY KEY REFERENCES songs (id) ON DELETE CASCADE,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now'))
);

CREATE TABLE playlists (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    cover       TEXT NOT NULL DEFAULT '',
    origin      TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now'))
);
CREATE UNIQUE INDEX playlists_origin ON playlists (origin) WHERE origin <> '';

CREATE TABLE playlist_items (
    playlist_id INTEGER NOT NULL REFERENCES playlists (id) ON DELETE CASCADE,
    song_id     INTEGER NOT NULL REFERENCES songs (id) ON DELETE CASCADE,
    position    REAL    NOT NULL,
    added_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
    PRIMARY KEY (playlist_id, song_id)
);
CREATE INDEX playlist_items_order ON playlist_items (playlist_id, position);

CREATE TABLE plays (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    song_id   INTEGER NOT NULL REFERENCES songs (id) ON DELETE CASCADE,
    source    TEXT    NOT NULL,
    seconds   INTEGER NOT NULL DEFAULT 0,
    played_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now'))
);
CREATE INDEX plays_recent ON plays (played_at DESC);

CREATE TABLE kv (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now'))
);
