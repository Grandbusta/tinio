-- +goose Up
-- What an agent and the people they talk with have going, as the agent keeps
-- it: one chat, sorted into topics as it goes, each with one person (their
-- key, like a percept's speaker), so one person's never reaches another's
-- reply. title is '' until the agent names it. summary is the turns at or
-- before folded_at, put in a few lines, and status where it stands now; both
-- are as of summed_at, the newest turn they were written from. settled is what was decided in it, kept apart so
-- folding can't lose it. A topic is quiet once finished or silent a while,
-- and open again when it's brought up. embedding is of its title and
-- summary, by embedded_with's model; NULL until it's needed, and again each
-- time it's summed up, so an old topic is found by what it was about.
CREATE TABLE topics (
    agent_id   TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id         TEXT NOT NULL,
    person     TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    summary    TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT '',
    settled    TEXT[] NOT NULL DEFAULT '{}',
    state      TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'quiet')),
    started_at TIMESTAMPTZ NOT NULL,
    last_at    TIMESTAMPTZ NOT NULL,
    folded_at  TIMESTAMPTZ,
    summed_at  TIMESTAMPTZ,
    embedding     REAL[],
    embedded_with TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (agent_id, id)
);
CREATE INDEX topics_recent ON topics (agent_id, person, last_at DESC);
ALTER TABLE topics ENABLE ROW LEVEL SECURITY;
ALTER TABLE topics FORCE ROW LEVEL SECURITY;
CREATE POLICY agent_only ON topics USING (agent_id = current_setting('app.agent_id', true))
    WITH CHECK (agent_id = current_setting('app.agent_id', true));

-- The topic a message is in, and the agent's own percepts (a delivery) in the
-- topic of what they answer; NULL: none, like small talk.
ALTER TABLE percepts ADD COLUMN topic_id TEXT;
CREATE INDEX percepts_topic ON percepts (agent_id, topic_id, at DESC) WHERE topic_id IS NOT NULL;

-- A chat row's topic, for the app, and the row it replies to, when the owner
-- chose one.
ALTER TABLE chat_messages ADD COLUMN topic_id TEXT, ADD COLUMN reply_to TEXT;
CREATE INDEX chat_messages_topic ON chat_messages (agent_id, topic_id, id) WHERE topic_id IS NOT NULL;

-- The topic an ask came up in, so its card shows there.
ALTER TABLE approvals ADD COLUMN topic_id TEXT;

-- +goose Down
ALTER TABLE approvals DROP COLUMN topic_id;
DROP INDEX chat_messages_topic;
ALTER TABLE chat_messages DROP COLUMN topic_id, DROP COLUMN reply_to;
DROP INDEX percepts_topic;
ALTER TABLE percepts DROP COLUMN topic_id;
DROP TABLE topics;
