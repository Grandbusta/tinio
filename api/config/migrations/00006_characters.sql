-- +goose Up
-- The characters to raise, each edit a new version. Tinio's own (account_id
-- NULL) are the ready ones, loaded from characters/ when the API starts; one
-- someone builds is theirs, and private. Raising one copies its files into the
-- agent's own (agent_versions), so a later version never changes an agent.
CREATE TABLE characters (
    key        TEXT NOT NULL,
    version    INT NOT NULL,
    account_id TEXT REFERENCES accounts (id) ON DELETE CASCADE,
    visibility TEXT NOT NULL CHECK (visibility IN ('ready', 'private', 'retired')),
    template   TEXT NOT NULL,
    character  TEXT NOT NULL,
    judgment   TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (key, version),
    -- Only someone's own is private, and theirs are only ever private.
    CHECK ((account_id IS NOT NULL) = (visibility = 'private'))
);
CREATE INDEX characters_account ON characters (account_id, key) WHERE account_id IS NOT NULL;

-- Which version of it an agent was raised from; 0 for none.
ALTER TABLE agents ADD COLUMN character_version INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE agents DROP COLUMN character_version;
DROP TABLE characters;
