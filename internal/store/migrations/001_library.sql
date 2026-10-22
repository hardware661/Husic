-- One row per recording as Music knows it; platform copies hang off it.
CREATE TABLE songs (
    id         bigserial PRIMARY KEY,
    title      text        NOT NULL,
    artists    text[]      NOT NULL DEFAULT '{}',
    album      text        NOT NULL DEFAULT '',
    duration   integer     NOT NULL DEFAULT 0, -- seconds
    cover      text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- The same song on each platform. A platform track belongs to one song.
CREATE TABLE song_sources (
    source      text        NOT NULL,
    source_id   text        NOT NULL,
    song_id     bigint      NOT NULL REFERENCES songs (id) ON DELETE CASCADE,
    title       text        NOT NULL,
    artists     text[]      NOT NULL DEFAULT '{}',
    album       text        NOT NULL DEFAULT '',
    duration    integer     NOT NULL DEFAULT 0,
    cover       text        NOT NULL DEFAULT '',
    extra       jsonb       NOT NULL DEFAULT '{}',
    pinned      boolean     NOT NULL DEFAULT false, -- the owner picked this copy
    last_ok     timestamptz,
    last_fail   timestamptz,
    fail_reason text        NOT NULL DEFAULT '',
    PRIMARY KEY (source, source_id)
);
CREATE INDEX song_sources_song ON song_sources (song_id);

CREATE TABLE likes (
    song_id    bigint PRIMARY KEY REFERENCES songs (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE playlists (
    id          bigserial PRIMARY KEY,
    name        text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    cover       text        NOT NULL DEFAULT '',
    origin      text        NOT NULL DEFAULT '', -- e.g. "netease:123" when imported
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX playlists_origin ON playlists (origin) WHERE origin <> '';

CREATE TABLE playlist_items (
    playlist_id bigint           NOT NULL REFERENCES playlists (id) ON DELETE CASCADE,
    song_id     bigint           NOT NULL REFERENCES songs (id) ON DELETE CASCADE,
    position    double precision NOT NULL,
    added_at    timestamptz      NOT NULL DEFAULT now(),
    PRIMARY KEY (playlist_id, song_id)
);
CREATE INDEX playlist_items_order ON playlist_items (playlist_id, position);

CREATE TABLE plays (
    id        bigserial PRIMARY KEY,
    song_id   bigint      NOT NULL REFERENCES songs (id) ON DELETE CASCADE,
    source    text        NOT NULL,
    seconds   integer     NOT NULL DEFAULT 0,
    played_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX plays_recent ON plays (played_at DESC);

-- Small settings and sync bookmarks.
CREATE TABLE kv (
    key        text PRIMARY KEY,
    value      jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
