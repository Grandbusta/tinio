-- +goose Up
-- An owner opening their agent's chat for the first time, once (memory.HelloID):
-- the agent introduces themselves.
ALTER TABLE chat_messages DROP CONSTRAINT chat_messages_kind_check,
    ADD CONSTRAINT chat_messages_kind_check
        CHECK (kind IN ('message', 'ask', 'choice', 'file', 'approve', 'pick', 'hello'));

-- +goose Down
DELETE FROM chat_messages WHERE kind = 'hello';
ALTER TABLE chat_messages DROP CONSTRAINT chat_messages_kind_check,
    ADD CONSTRAINT chat_messages_kind_check
        CHECK (kind IN ('message', 'ask', 'choice', 'file', 'approve', 'pick'));
