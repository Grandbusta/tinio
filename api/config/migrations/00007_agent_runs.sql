-- +goose Up
-- Who runs each agent. A worker holds an agent while lease_until is ahead,
-- renewing it as it runs them; one whose lease lapsed (or never had one) is
-- free for any worker to claim. born_at is when their first start finished
-- seeding them, so later starts don't seed again. This is the platform's, not
-- the agent's memory, and workers read it across agents, so it has no
-- row-level security (like agents).
CREATE TABLE agent_runs (
    agent_id    TEXT PRIMARY KEY REFERENCES agents (id) ON DELETE CASCADE,
    worker      TEXT,
    lease_until TIMESTAMPTZ,
    born_at     TIMESTAMPTZ,
    CHECK ((worker IS NULL) = (lease_until IS NULL))
);
CREATE INDEX agent_runs_lease ON agent_runs (lease_until NULLS FIRST);
INSERT INTO agent_runs (agent_id) SELECT id FROM agents;

-- +goose Down
DROP TABLE agent_runs;
