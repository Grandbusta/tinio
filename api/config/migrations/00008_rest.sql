-- +goose Up
-- Agents rest when idle and are woken when they need to be: a free agent is
-- claimed once wake_at has passed. kept is the little their life loop keeps in
-- memory, kept while they rest.
ALTER TABLE agent_runs
    ADD COLUMN wake_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN kept    JSONB NOT NULL DEFAULT '{}';
CREATE INDEX agent_runs_wake ON agent_runs (wake_at) WHERE worker IS NULL;

-- +goose Down
DROP INDEX agent_runs_wake;
ALTER TABLE agent_runs DROP COLUMN wake_at, DROP COLUMN kept;
