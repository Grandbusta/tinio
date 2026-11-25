-- +goose Up
-- What each coding job did, for its page: each step told by who did it (the agent,
-- the CLI, the owner), in order. And the commit each job started from, its rounds (a snapshot of its copy after
-- each, under refs/tinio/jobs/<job>/<round>), the commit made on the owner's yes, and
-- the pull request opened on another.
CREATE TABLE coding_steps (
    job_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    at TEXT NOT NULL,
    who TEXT NOT NULL,
    what TEXT NOT NULL,
    detail TEXT DEFAULT '' NOT NULL,
    CHECK (who IN ('agent', 'cli', 'owner', 'tinio')),
    PRIMARY KEY (job_id, seq),
    FOREIGN KEY (job_id) REFERENCES coding_jobs(id) ON DELETE CASCADE
);
-- What an agent keeps about each of their folders from the coding jobs done in it: a
-- few short notes (how its tests run, rules the owner gave, what was sent back), for
-- their next brief, the CLI and their checks.
CREATE TABLE coding_notes (
    agent_id TEXT NOT NULL,
    folder_id TEXT NOT NULL,
    notes TEXT DEFAULT '[]' NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (agent_id, folder_id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
ALTER TABLE coding_jobs ADD COLUMN base_sha TEXT DEFAULT '' NOT NULL;
ALTER TABLE coding_jobs ADD COLUMN rounds INTEGER DEFAULT 0 NOT NULL;
ALTER TABLE coding_jobs ADD COLUMN commit_sha TEXT DEFAULT '' NOT NULL;
ALTER TABLE coding_jobs ADD COLUMN pr_url TEXT DEFAULT '' NOT NULL;

-- +goose Down
ALTER TABLE coding_jobs DROP COLUMN pr_url;
ALTER TABLE coding_jobs DROP COLUMN commit_sha;
ALTER TABLE coding_jobs DROP COLUMN rounds;
ALTER TABLE coding_jobs DROP COLUMN base_sha;
DROP TABLE coding_notes;
DROP TABLE coding_steps;
