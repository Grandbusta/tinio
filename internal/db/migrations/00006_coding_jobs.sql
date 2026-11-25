-- +goose Up
-- Coding jobs: an agent's job for Claude Code in one of their folders that's a git
-- repository, on a copy of it (a worktree, on its own branch) unless the owner said
-- otherwise. card is the chat row that shows it, updated as it goes; session is the
-- CLI's, to carry it on.
CREATE TABLE coding_jobs (
    id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    folder_id TEXT NOT NULL,
    topic_id TEXT DEFAULT '' NOT NULL,
    title TEXT NOT NULL,
    brief TEXT NOT NULL,
    cli TEXT NOT NULL,
    model TEXT NOT NULL,
    repo TEXT NOT NULL,
    dir TEXT DEFAULT '' NOT NULL,
    branch TEXT DEFAULT '' NOT NULL,
    base TEXT DEFAULT '' NOT NULL,
    session TEXT DEFAULT '' NOT NULL,
    status TEXT NOT NULL,
    card TEXT DEFAULT '' NOT NULL,
    cost_usd REAL DEFAULT 0 NOT NULL,
    turns INTEGER DEFAULT 0 NOT NULL,
    started_at TEXT NOT NULL,
    ended_at TEXT DEFAULT '' NOT NULL,
    CHECK (status IN ('working', 'waiting', 'ready', 'stopped', 'failed', 'cancelled')),
    PRIMARY KEY (id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE INDEX coding_jobs_agent ON coding_jobs (agent_id, started_at);

-- The chat gains a job's card ('job', updated as it goes), its questions to the owner
-- ('job_ask'), and the owner's replies to a job ('job_reply'), which go to the job and
-- not to the agent. SQLite can't change a CHECK, so the table is made again.
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
    CHECK (kind IN ('message', 'ask', 'choice', 'file', 'approve', 'pick', 'hello', 'react', 'job', 'job_ask', 'job_reply')),
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
    CHECK (kind IN ('message', 'ask', 'choice', 'file', 'approve', 'pick', 'hello', 'react')),
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
INSERT INTO chat_messages_old (agent_id, id, author, text, at, kind, payload, settled_at, topic_id, reply_to)
    SELECT agent_id, id, author, text, at, kind, payload, settled_at, topic_id, reply_to FROM chat_messages
    WHERE kind NOT IN ('job', 'job_ask', 'job_reply');
DROP TABLE chat_messages;
ALTER TABLE chat_messages_old RENAME TO chat_messages;
CREATE INDEX chat_messages_topic ON chat_messages (agent_id, topic_id, id) WHERE (topic_id IS NOT NULL);
DROP TABLE coding_jobs;
