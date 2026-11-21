-- +goose Up
-- Each agent's quiet hours, HH:MM in their time zone ('' and '': none), when
-- they don't message their owner first; and when their owner paused them (null:
-- not paused). A paused agent isn't claimed by any worker, so they neither work
-- nor hear anything until they're resumed.
ALTER TABLE agents
    ADD COLUMN quiet_from  TEXT NOT NULL DEFAULT '',
    ADD COLUMN quiet_until TEXT NOT NULL DEFAULT '',
    ADD COLUMN paused_at   TIMESTAMPTZ;

-- +goose Down
ALTER TABLE agents DROP COLUMN quiet_from, DROP COLUMN quiet_until, DROP COLUMN paused_at;
