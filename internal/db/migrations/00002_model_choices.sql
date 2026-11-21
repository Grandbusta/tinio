-- +goose Up
-- The models the owner chose for each of the agents' jobs (replies, decisions, their
-- own day, light mode, works), by job; a job with no row uses Tinio's default.
CREATE TABLE model_choices (
    job TEXT NOT NULL PRIMARY KEY,
    model TEXT NOT NULL,
    chosen_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE model_choices;
