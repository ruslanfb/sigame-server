-- +goose Up
CREATE TABLE packs (
  id             TEXT PRIMARY KEY,                 -- UUIDv7
  version        INTEGER NOT NULL DEFAULT 1,       -- optimistic-concurrency version
  name           TEXT NOT NULL,
  language       TEXT NOT NULL DEFAULT '',
  difficulty     INTEGER NOT NULL DEFAULT 5,
  authors        TEXT NOT NULL DEFAULT '',         -- joined by \n for LIKE search
  tags           TEXT NOT NULL DEFAULT '',         -- joined by \n
  round_count    INTEGER NOT NULL DEFAULT 0,
  theme_count    INTEGER NOT NULL DEFAULT 0,
  question_count INTEGER NOT NULL DEFAULT 0,
  has_media      INTEGER NOT NULL DEFAULT 0,
  logo_media_id  TEXT NOT NULL DEFAULT '',
  doc            TEXT NOT NULL,                    -- full pack JSON
  created_at     INTEGER NOT NULL,                 -- Unix ms UTC
  updated_at     INTEGER NOT NULL                  -- Unix ms UTC
);
CREATE INDEX packs_name ON packs(name);
CREATE INDEX packs_updated ON packs(updated_at DESC);

CREATE TABLE media (
  id            TEXT PRIMARY KEY,                  -- sha256 hex of the content
  kind          TEXT NOT NULL,                     -- image|audio|video|html
  mime          TEXT NOT NULL,
  ext           TEXT NOT NULL,
  size          INTEGER NOT NULL,
  duration_ms   INTEGER NOT NULL DEFAULT 0,
  width         INTEGER NOT NULL DEFAULT 0,
  height        INTEGER NOT NULL DEFAULT 0,
  original_name TEXT NOT NULL DEFAULT '',
  ref_count     INTEGER NOT NULL DEFAULT 0,        -- number of packs referencing the object
  created_at    INTEGER NOT NULL                   -- Unix ms UTC
);

CREATE TABLE pack_media (
  pack_id  TEXT NOT NULL REFERENCES packs(id) ON DELETE CASCADE,
  media_id TEXT NOT NULL REFERENCES media(id),
  PRIMARY KEY (pack_id, media_id)
);
CREATE INDEX pack_media_media ON pack_media(media_id);

CREATE TABLE rooms (
  id         TEXT PRIMARY KEY,                     -- UUIDv7
  code       TEXT NOT NULL UNIQUE,                 -- 5-char join code
  pack_id    TEXT NOT NULL,
  name       TEXT NOT NULL DEFAULT '',
  settings   TEXT NOT NULL,                        -- json
  status     TEXT NOT NULL,
  created_at INTEGER NOT NULL,                     -- Unix ms UTC
  updated_at INTEGER NOT NULL                      -- Unix ms UTC
);

CREATE TABLE game_results (
  id          TEXT PRIMARY KEY,                    -- UUIDv7
  room_id     TEXT NOT NULL,
  pack_id     TEXT NOT NULL,
  finished_at INTEGER NOT NULL,                    -- Unix ms UTC
  doc         TEXT NOT NULL                        -- json
);

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS game_results;
DROP TABLE IF EXISTS rooms;
DROP INDEX IF EXISTS pack_media_media;
DROP TABLE IF EXISTS pack_media;
DROP TABLE IF EXISTS media;
DROP INDEX IF EXISTS packs_updated;
DROP INDEX IF EXISTS packs_name;
DROP TABLE IF EXISTS packs;
