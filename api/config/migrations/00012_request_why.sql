-- +goose Up
-- Why an agent asks its owner, shown with the request.
ALTER TABLE approvals ADD COLUMN why TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE approvals DROP COLUMN why;
