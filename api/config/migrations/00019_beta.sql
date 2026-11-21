-- +goose Up
-- The platform's own settings, one row. beta on: only invited emails can make
-- an account (accounts sign in as usual). Changed only by an admin.
CREATE TABLE platform_settings (
    id              BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    beta            BOOLEAN NOT NULL DEFAULT false,
    beta_changed_by TEXT NOT NULL DEFAULT '',
    beta_changed_at TIMESTAMPTZ
);
INSERT INTO platform_settings DEFAULT VALUES;

-- Who an admin invited to the beta, by email (lower case). Someone has joined
-- when an account has that email; that isn't kept here. withdrawn_at: the
-- invite no longer works.
CREATE TABLE beta_invites (
    email        TEXT PRIMARY KEY,
    invited_by   TEXT NOT NULL,
    invited_at   TIMESTAMPTZ NOT NULL,
    sent_at      TIMESTAMPTZ NOT NULL,
    sends        INT NOT NULL DEFAULT 1,
    withdrawn_at TIMESTAMPTZ
);

-- +goose Down
DROP TABLE beta_invites;
DROP TABLE platform_settings;
