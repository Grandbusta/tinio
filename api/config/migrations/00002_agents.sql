-- +goose Up
-- Each agent, owned by an account: which ready character they started from (or
-- '' for one built by hand), what they call their owner, who their owner is
-- to them, and their pace (rounds of work a day: 1 like a person, 3, 10).
CREATE TABLE agents (
    id            TEXT PRIMARY KEY,
    account_id    TEXT NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    character_key TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL,
    owner_name    TEXT NOT NULL,
    relation      TEXT NOT NULL,
    pace          INT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL
);
CREATE INDEX agents_account ON agents (account_id, created_at);

-- +goose Down
DROP TABLE agents;
