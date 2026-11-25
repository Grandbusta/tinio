-- +goose Up
-- The owner's settings for this computer that aren't any one agent's, each kept
-- as JSON by name: "coding", how coding jobs run.
CREATE TABLE app_settings (
    name TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL,
    set_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE app_settings;
