-- +goose Up
-- The sections are named for what people see: their character and how they
-- work.
UPDATE self_versions SET section = 'character' WHERE section = 'life';
UPDATE self_versions SET section = 'work' WHERE section = 'living';

-- +goose Down
UPDATE self_versions SET section = 'life' WHERE section = 'character';
UPDATE self_versions SET section = 'living' WHERE section = 'work';
