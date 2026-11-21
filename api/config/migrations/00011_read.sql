-- +goose Up
-- The last row of their agent's chat the owner has seen: what the agent says
-- after it is unread.
ALTER TABLE agents ADD COLUMN owner_read_until TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE agents DROP COLUMN owner_read_until;
