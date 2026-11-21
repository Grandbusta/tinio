-- +goose Up
-- Each account's plan (internal/usage.Plans), set by hand until billing is
-- built, and what its agents' models cost this week and this hour, raised in
-- the statement that saves each decision. A week counts from when the account
-- started; a counter from an earlier week or hour is read as nothing.
ALTER TABLE accounts ADD COLUMN plan TEXT NOT NULL DEFAULT 'free'
    CHECK (plan IN ('free', 'personal', 'pro', 'team'));

CREATE TABLE account_usage (
    account_id TEXT PRIMARY KEY REFERENCES accounts (id) ON DELETE CASCADE,
    week_start TIMESTAMPTZ NOT NULL,
    week_usd   NUMERIC(14, 8) NOT NULL,
    hour_start TIMESTAMPTZ NOT NULL,
    hour_usd   NUMERIC(14, 8) NOT NULL
);

-- +goose Down
DROP TABLE account_usage;
ALTER TABLE accounts DROP COLUMN plan;
