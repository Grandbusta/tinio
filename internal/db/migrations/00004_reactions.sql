-- +goose Up
-- The owner's thumb on something their agent made, in the chat: a row the
-- agent hears, like a tap on a request, whose payload says which work and how
-- it landed (perception.Reaction). Never shown. SQLite can't change a CHECK,
-- so the table is made again with 'react' allowed; nothing refers to it.
CREATE TABLE chat_messages_new (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    author TEXT NOT NULL,
    text TEXT NOT NULL,
    at TEXT NOT NULL,
    kind TEXT DEFAULT 'message' NOT NULL,
    payload TEXT,
    settled_at TEXT,
    topic_id TEXT,
    reply_to TEXT,
    CHECK (author IN ('owner', 'agent')),
    CHECK (kind IN ('message', 'ask', 'choice', 'file', 'approve', 'pick', 'hello', 'react')),
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
INSERT INTO chat_messages_new (agent_id, id, author, text, at, kind, payload, settled_at, topic_id, reply_to)
    SELECT agent_id, id, author, text, at, kind, payload, settled_at, topic_id, reply_to FROM chat_messages;
DROP TABLE chat_messages;
ALTER TABLE chat_messages_new RENAME TO chat_messages;
CREATE INDEX chat_messages_topic ON chat_messages (agent_id, topic_id, id) WHERE (topic_id IS NOT NULL);

-- +goose Down
CREATE TABLE chat_messages_old (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    author TEXT NOT NULL,
    text TEXT NOT NULL,
    at TEXT NOT NULL,
    kind TEXT DEFAULT 'message' NOT NULL,
    payload TEXT,
    settled_at TEXT,
    topic_id TEXT,
    reply_to TEXT,
    CHECK (author IN ('owner', 'agent')),
    CHECK (kind IN ('message', 'ask', 'choice', 'file', 'approve', 'pick', 'hello')),
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
INSERT INTO chat_messages_old (agent_id, id, author, text, at, kind, payload, settled_at, topic_id, reply_to)
    SELECT agent_id, id, author, text, at, kind, payload, settled_at, topic_id, reply_to FROM chat_messages WHERE kind <> 'react';
DROP TABLE chat_messages;
ALTER TABLE chat_messages_old RENAME TO chat_messages;
CREATE INDEX chat_messages_topic ON chat_messages (agent_id, topic_id, id) WHERE (topic_id IS NOT NULL);
