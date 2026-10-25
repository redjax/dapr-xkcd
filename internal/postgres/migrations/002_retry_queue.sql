ALTER TABLE comic_fetches
    DROP CONSTRAINT comic_fetches_status_check;

ALTER TABLE comic_fetches
    ADD CONSTRAINT comic_fetches_status_check
    CHECK (
        status IN (
            'pending',
            'complete',
            'unavailable',
            'retry_pending',
            'failed'
        )
    );

ALTER TABLE comic_fetches
    ADD COLUMN retry_attempts INTEGER NOT NULL DEFAULT 0
        CHECK (retry_attempts >= 0),
    ADD COLUMN next_retry_at TIMESTAMPTZ;

UPDATE comic_fetches
SET next_retry_at = now() + interval '5 minutes'
WHERE status = 'retry_pending';

CREATE INDEX comic_fetches_retry_due_idx
    ON comic_fetches (next_retry_at, comic_id)
    WHERE status = 'retry_pending';