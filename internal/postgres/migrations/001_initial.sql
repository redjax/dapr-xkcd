CREATE TABLE comics (
    id                   INTEGER PRIMARY KEY CHECK (id > 0),
    title                TEXT NOT NULL,
    safe_title           TEXT NOT NULL,
    alt                  TEXT NOT NULL,
    published_date       DATE NOT NULL,
    upstream_image_url   TEXT NOT NULL,
    metadata             JSONB NOT NULL,
    metadata_fetched_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE comic_images (
    comic_id       INTEGER PRIMARY KEY REFERENCES comics(id) ON DELETE CASCADE,
    content_type   TEXT NOT NULL,
    byte_size      BIGINT NOT NULL CHECK (byte_size > 0),
    sha256         TEXT NOT NULL,
    data           BYTEA NOT NULL,
    downloaded_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (byte_size = octet_length(data))
);

CREATE TABLE comic_fetches (
    comic_id        INTEGER PRIMARY KEY CHECK (comic_id > 0),
    status          TEXT NOT NULL CHECK (
        status IN ('pending', 'complete', 'unavailable', 'retry_pending')
    ),
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMPTZ,
    last_error      TEXT
);

CREATE TABLE sync_state (
    name             TEXT PRIMARY KEY,
    latest_seen_id   INTEGER NOT NULL DEFAULT 0,
    latest_saved_id  INTEGER NOT NULL DEFAULT 0,
    backfill_next_id INTEGER NOT NULL DEFAULT 1,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (latest_seen_id >= 0),
    CHECK (latest_saved_id >= 0),
    CHECK (backfill_next_id > 0)
);

INSERT INTO sync_state (name) VALUES ('xkcd');

CREATE INDEX comics_published_date_idx ON comics (published_date, id);
CREATE INDEX comic_fetches_status_idx ON comic_fetches (status, comic_id);