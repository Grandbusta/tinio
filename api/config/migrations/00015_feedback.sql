-- +goose Up
-- What people tell us from the app, and the errors their browsers met: read
-- with SELECT * FROM feedback ORDER BY created_at DESC.
CREATE TABLE feedback (
    id         TEXT PRIMARY KEY,
    account_id TEXT REFERENCES accounts (id) ON DELETE SET NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('feedback', 'error')),
    page       TEXT NOT NULL DEFAULT '',
    text       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX feedback_newest ON feedback (created_at DESC);

-- +goose Down
DROP TABLE feedback;
