-- +goose Up
-- Who has an account: the email we write to, and when they accepted the
-- Terms and said they were 18 or older (both when the account was made).
CREATE TABLE accounts (
    id                TEXT PRIMARY KEY,
    email             TEXT NOT NULL UNIQUE,   -- lower case
    name              TEXT NOT NULL DEFAULT '',
    terms_accepted_at TIMESTAMPTZ NOT NULL,
    adult_at          TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL
);

-- Each way to sign in to an account, a row each: email (subject is the
-- address) or google (subject is Google's stable id).
CREATE TABLE identities (
    id           TEXT PRIMARY KEY,
    account_id   TEXT NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    provider     TEXT NOT NULL,
    subject      TEXT NOT NULL,
    email        TEXT NOT NULL DEFAULT '',   -- what the provider said, for google
    created_at   TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ NOT NULL,
    UNIQUE (provider, subject)
);
CREATE INDEX identities_account ON identities (account_id);

-- One-time codes, by type (sign_in now; others later). A code is stored only
-- as its hash; one is live per type and target, and a new one replaces it.
CREATE TABLE otps (
    id         TEXT PRIMARY KEY,
    type       TEXT NOT NULL,
    target     TEXT NOT NULL,                -- where it went: an email address
    account_id TEXT REFERENCES accounts (id) ON DELETE CASCADE,  -- null when there may be no account yet
    code_hash  BYTEA NOT NULL,
    attempts   INT NOT NULL DEFAULT 0,       -- wrong tries
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX otps_live ON otps (type, target) WHERE used_at IS NULL;

-- Signed-in browsers. The token is stored only as its SHA-256 hash.
CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,
    account_id   TEXT NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    token_hash   BYTEA NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX sessions_account ON sessions (account_id);

-- +goose Down
DROP TABLE sessions;
DROP TABLE otps;
DROP TABLE identities;
DROP TABLE accounts;
