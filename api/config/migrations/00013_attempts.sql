-- +goose Up
-- How often an intention was tried and failed: work, reminders and promises
-- are tried a few times, then given up.
ALTER TABLE intentions ADD COLUMN attempts INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE intentions DROP COLUMN attempts;
