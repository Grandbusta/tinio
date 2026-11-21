-- +goose Up
-- Skills published for anyone's agent to learn. A skill has versions; each is
-- reviewed by an admin before it's listed (status review, then listed,
-- changes or rejected; a listed one is retired when a newer is listed), and
-- is private until then. details holds what a SKILL.md doesn't: the questions
-- it asks owners, what they make, the tools it needs, its suggested
-- permissions, starter principles, first task and tests. checks are the
-- automatic checks' findings. Skills are free for now. These are the
-- platform's, read across agents, so they have no row-level security.
CREATE TABLE market_skills (
    id         TEXT PRIMARY KEY,
    author_id  TEXT NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    handle     TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE market_skill_versions (
    id           TEXT PRIMARY KEY,
    skill_id     TEXT NOT NULL REFERENCES market_skills (id) ON DELETE CASCADE,
    version      INT NOT NULL,
    title        TEXT NOT NULL,
    category     TEXT NOT NULL,
    summary      TEXT NOT NULL,
    description  TEXT NOT NULL,
    details      JSONB NOT NULL,
    checks       JSONB NOT NULL,
    status       TEXT NOT NULL CHECK (status IN ('review', 'changes', 'listed', 'rejected', 'retired')),
    note         TEXT NOT NULL DEFAULT '',
    submitted_at TIMESTAMPTZ NOT NULL,
    reviewed_at  TIMESTAMPTZ,
    reviewed_by  TEXT NOT NULL DEFAULT '',
    UNIQUE (skill_id, version)
);
CREATE INDEX market_skill_versions_status ON market_skill_versions (status, submitted_at);

CREATE TABLE market_skill_files (
    version_id TEXT NOT NULL REFERENCES market_skill_versions (id) ON DELETE CASCADE,
    path       TEXT NOT NULL,
    content    BYTEA NOT NULL,
    PRIMARY KEY (version_id, path)
);

-- Who taught which skill to which agent, for "agents taught".
CREATE TABLE market_teaches (
    skill_id TEXT NOT NULL REFERENCES market_skills (id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    version  INT NOT NULL,
    at       TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (skill_id, agent_id)
);

-- Where an agent's know-how came from: '' imported by their owner, or
-- market:<skill id>@<version>.
ALTER TABLE agent_skills ADD COLUMN source TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE agent_skills DROP COLUMN source;
DROP TABLE market_teaches;
DROP TABLE market_skill_files;
DROP TABLE market_skill_versions;
DROP TABLE market_skills;
