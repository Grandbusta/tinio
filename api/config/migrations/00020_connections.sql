-- +goose Up
-- Accounts an owner connected (Google, Atlassian, ...), one per connector,
-- given to the agents they choose. access is what they allowed (the parts,
-- the files they picked); secret is the tokens and the sign-in client,
-- sealed with TINIO_KEY, never shown. state needs_you: the service signed
-- Tinio out, and the owner has to sign in again.
CREATE TABLE connections (
    id         TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    connector  TEXT NOT NULL,
    label      TEXT NOT NULL DEFAULT '',
    access     JSONB NOT NULL,
    secret     BYTEA NOT NULL,
    state      TEXT NOT NULL DEFAULT 'ok' CHECK (state IN ('ok', 'needs_you')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (account_id, connector)
);

CREATE TABLE connection_agents (
    connection_id TEXT NOT NULL REFERENCES connections (id) ON DELETE CASCADE,
    agent_id      TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    PRIMARY KEY (connection_id, agent_id)
);
CREATE INDEX connection_agents_agent ON connection_agents (agent_id);

-- A sign-in under way, by its state: kept until the service sends the
-- owner back, for 15 minutes at most. secret holds its PKCE verifier and
-- client, sealed.
CREATE TABLE connect_starts (
    state      TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    connector  TEXT NOT NULL,
    agent_id   TEXT NOT NULL DEFAULT '',
    access     JSONB NOT NULL,
    secret     BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE connect_starts;
DROP TABLE connection_agents;
DROP TABLE connections;
