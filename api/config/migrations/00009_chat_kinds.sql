-- +goose Up
-- Web chat carries what Telegram's buttons and documents carry: a request to
-- the owner (ask) and a call on their judgment (choice), their taps on them
-- (approve, pick), and files. payload says which request, call or file;
-- settled_at is when a request or call ended, and its text says how.
ALTER TABLE chat_messages
    ADD COLUMN kind       TEXT NOT NULL DEFAULT 'message'
        CHECK (kind IN ('message', 'ask', 'choice', 'file', 'approve', 'pick')),
    ADD COLUMN payload    JSONB,
    ADD COLUMN settled_at TIMESTAMPTZ;

-- A file an agent sent in their chat.
CREATE TABLE chat_files (
    agent_id     TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id           TEXT NOT NULL,
    name         TEXT NOT NULL,
    content_type TEXT NOT NULL,
    data         BYTEA NOT NULL CHECK (length(data) <= 10485760),
    at           TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (agent_id, id)
);
ALTER TABLE chat_files ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_files FORCE ROW LEVEL SECURITY;
CREATE POLICY agent_only ON chat_files USING (agent_id = current_setting('app.agent_id', true))
    WITH CHECK (agent_id = current_setting('app.agent_id', true));

-- +goose Down
DROP TABLE chat_files;
ALTER TABLE chat_messages DROP COLUMN kind, DROP COLUMN payload, DROP COLUMN settled_at;
