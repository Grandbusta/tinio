-- +goose Up
-- When someone last wrote, or the agent last spoke first to them, by kind:
-- asked on every tick while an agent is awake.
CREATE INDEX percepts_by_speaker ON percepts (agent_id, speaker, kind, at DESC);

-- +goose Down
DROP INDEX percepts_by_speaker;
