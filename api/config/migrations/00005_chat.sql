-- +goose Up
-- An agent's web chat, both sides. The owner's messages are what their agent
-- hears (as percepts from source 'web', external_id the message's id); the
-- agent's are what it said.
CREATE TABLE chat_messages (
    agent_id TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id       TEXT NOT NULL,
    author   TEXT NOT NULL CHECK (author IN ('owner', 'agent')),
    text     TEXT NOT NULL,
    at       TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (agent_id, id)
);
ALTER TABLE chat_messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_messages FORCE ROW LEVEL SECURITY;
CREATE POLICY agent_only ON chat_messages USING (agent_id = current_setting('app.agent_id', true))
    WITH CHECK (agent_id = current_setting('app.agent_id', true));

-- +goose Down
DROP TABLE chat_messages;
