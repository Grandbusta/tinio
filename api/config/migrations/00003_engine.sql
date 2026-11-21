-- +goose Up
-- Everything an agent's mind keeps, in the platform's one database: the
-- engine's tables (they were one database per agent), each row tagged with
-- its agent_id. The agent leads every key, and every link between an agent's
-- rows includes it, so a row can never point into another agent's. Deleting an
-- agent deletes all of it. Only sources (public books and articles) is shared.

-- What it perceived: one row per incoming message or event.
CREATE TABLE percepts (
    agent_id    TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id          TEXT NOT NULL,
    source      TEXT NOT NULL,
    external_id TEXT NOT NULL,
    thread      TEXT NOT NULL DEFAULT '',
    speaker     TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL,
    text        TEXT NOT NULL DEFAULT '',
    payload     JSONB,
    at          TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (agent_id, id),
    UNIQUE (agent_id, source, external_id)
);
CREATE INDEX percepts_thread ON percepts (agent_id, source, thread, at DESC);

-- Every System 1 and System 2 decision, chained by parent_id.
CREATE TABLE decisions (
    agent_id   TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id         TEXT NOT NULL,
    parent_id  TEXT,
    at         TIMESTAMPTZ NOT NULL,
    percept_id TEXT,
    set_id     TEXT NOT NULL,
    tier       TEXT NOT NULL,
    provider   TEXT NOT NULL DEFAULT '',
    answers    JSONB NOT NULL DEFAULT '{}',
    action     TEXT NOT NULL,
    reply      TEXT,
    gate       TEXT NOT NULL,
    latency_ms DOUBLE PRECISION NOT NULL DEFAULT 0,
    cost_usd   NUMERIC(14, 8) NOT NULL DEFAULT 0,
    outcome    JSONB,
    affect     JSONB,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id, parent_id) REFERENCES decisions (agent_id, id),
    FOREIGN KEY (agent_id, percept_id) REFERENCES percepts (agent_id, id)
);
CREATE INDEX decisions_at ON decisions (agent_id, at DESC);
CREATE INDEX decisions_percept ON decisions (agent_id, percept_id);

CREATE TABLE labels (
    agent_id    TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    decision_id TEXT NOT NULL,
    question    TEXT NOT NULL,
    value       TEXT NOT NULL,
    source      TEXT NOT NULL,
    at          TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (agent_id, decision_id, question),
    FOREIGN KEY (agent_id, decision_id) REFERENCES decisions (agent_id, id)
);

-- The people it knows, by key: a speaker ('account:<id>', 'telegram:<id>') or
-- someone from its story.
CREATE TABLE people (
    agent_id   TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    relation   TEXT NOT NULL DEFAULT '',
    circle     TEXT NOT NULL DEFAULT '',
    source     TEXT NOT NULL,
    first_seen TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (agent_id, key)
);

-- Facts about itself ('self') or a person (their key).
CREATE TABLE facts (
    agent_id   TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id         TEXT NOT NULL,
    percept_id TEXT NOT NULL,
    subject    TEXT NOT NULL DEFAULT '',
    text       TEXT NOT NULL,
    at         TIMESTAMPTZ NOT NULL,
    areas      TEXT[] NOT NULL DEFAULT '{}',
    public     BOOLEAN,
    asked_at   TIMESTAMPTZ,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id, percept_id) REFERENCES percepts (agent_id, id)
);
CREATE INDEX facts_at ON facts (agent_id, at DESC);
CREATE INDEX facts_subject ON facts (agent_id, subject, at DESC);
CREATE INDEX facts_areas ON facts USING gin (areas);

-- Its public page, as last written: one per agent.
CREATE TABLE profile (
    agent_id   TEXT PRIMARY KEY REFERENCES agents (id) ON DELETE CASCADE,
    page       JSONB NOT NULL,
    inputs     TEXT NOT NULL,
    written_at TIMESTAMPTZ NOT NULL
);

-- Public-domain books and Wikipedia articles, fetched once for everyone. How
-- far an agent got is in its own projects (source_id, position).
CREATE TABLE sources (
    id         TEXT PRIMARY KEY,
    kind       TEXT NOT NULL,
    title      TEXT NOT NULL,
    author     TEXT NOT NULL DEFAULT '',
    url        TEXT NOT NULL UNIQUE,
    text       TEXT NOT NULL,
    chars      INT NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE dreams (
    agent_id TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id       TEXT NOT NULL,
    text     TEXT NOT NULL,
    status   TEXT NOT NULL,
    source   TEXT NOT NULL,
    since    TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    PRIMARY KEY (agent_id, id)
);

CREATE TABLE goals (
    agent_id   TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id         TEXT NOT NULL,
    dream_id   TEXT NOT NULL,
    text       TEXT NOT NULL,
    done_when  TEXT NOT NULL,
    due        TIMESTAMPTZ NOT NULL,
    status     TEXT NOT NULL,
    progress   TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    skill      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id, dream_id) REFERENCES dreams (agent_id, id)
);

CREATE TABLE projects (
    agent_id   TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id         TEXT NOT NULL,
    title      TEXT NOT NULL,
    kind       TEXT NOT NULL,
    status     TEXT NOT NULL,
    notes      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    source_id  TEXT REFERENCES sources (id),
    position   INT NOT NULL DEFAULT 0,
    goal_id    TEXT,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id, goal_id) REFERENCES goals (agent_id, id)
);

-- What it means to do: reminders, promises, its own activities.
CREATE TABLE intentions (
    agent_id   TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id         TEXT NOT NULL,
    subject    TEXT NOT NULL,
    percept_id TEXT,
    text       TEXT NOT NULL,
    due_at     TIMESTAMPTZ NOT NULL,
    status     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    kind       TEXT NOT NULL DEFAULT 'remind',
    project_id TEXT,
    result     TEXT,
    done_at    TIMESTAMPTZ,
    ability    TEXT NOT NULL DEFAULT '',
    args       JSONB NOT NULL DEFAULT '{}',
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id, percept_id) REFERENCES percepts (agent_id, id),
    FOREIGN KEY (agent_id, project_id) REFERENCES projects (agent_id, id)
);
CREATE INDEX intentions_due ON intentions (agent_id, due_at) WHERE status = 'pending';

-- What it made.
CREATE TABLE works (
    agent_id        TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id              TEXT NOT NULL,
    intention_id    TEXT,
    project_id      TEXT,
    kind            TEXT NOT NULL,
    title           TEXT NOT NULL,
    text            TEXT NOT NULL,
    public          BOOLEAN,
    created_at      TIMESTAMPTZ NOT NULL,
    public_by       TEXT,
    review_asked_at TIMESTAMPTZ,
    embedding       REAL[],
    embedded_with   TEXT NOT NULL DEFAULT '',
    data            JSONB,
    data_kind       TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id, intention_id) REFERENCES intentions (agent_id, id),
    FOREIGN KEY (agent_id, project_id) REFERENCES projects (agent_id, id)
);
CREATE INDEX works_created ON works (agent_id, created_at DESC);

-- What waits for the owner's yes.
CREATE TABLE approvals (
    agent_id   TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id         TEXT NOT NULL,
    class      TEXT NOT NULL,
    person     TEXT NOT NULL DEFAULT '',
    what       TEXT NOT NULL,
    text       TEXT NOT NULL DEFAULT '',
    kind       TEXT NOT NULL DEFAULT '',
    ref        TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL,
    asked_at   TIMESTAMPTZ NOT NULL,
    sent_at    TIMESTAMPTZ,
    decided_at TIMESTAMPTZ,
    message    JSONB,
    PRIMARY KEY (agent_id, id)
);
CREATE INDEX approvals_pending ON approvals (agent_id, asked_at) WHERE status = 'pending';

-- Permission changes the owner made in chat.
CREATE TABLE autonomy_rules (
    agent_id   TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id         TEXT NOT NULL,
    class      TEXT NOT NULL,
    verdict    TEXT NOT NULL,
    set_by     TEXT NOT NULL,
    percept_id TEXT,
    at         TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id, percept_id) REFERENCES percepts (agent_id, id)
);
CREATE INDEX autonomy_rules_class ON autonomy_rules (agent_id, class, at DESC);

CREATE TABLE skills (
    agent_id   TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    level      DOUBLE PRECISION NOT NULL,
    shown_by   TEXT[] NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (agent_id, name)
);

CREATE TABLE skill_evidence (
    agent_id TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id       TEXT NOT NULL,
    skill    TEXT NOT NULL,
    work_id  TEXT,
    score    DOUBLE PRECISION NOT NULL,
    source   TEXT NOT NULL,
    note     TEXT NOT NULL DEFAULT '',
    at       TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id, skill) REFERENCES skills (agent_id, name),
    FOREIGN KEY (agent_id, work_id) REFERENCES works (agent_id, id)
);
CREATE INDEX skill_evidence_skill ON skill_evidence (agent_id, skill, at DESC);

CREATE TABLE lessons (
    agent_id   TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id         TEXT NOT NULL,
    skill      TEXT NOT NULL,
    text       TEXT NOT NULL,
    source     TEXT NOT NULL,
    work_id    TEXT,
    at         TIMESTAMPTZ NOT NULL,
    retired_at TIMESTAMPTZ,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id, skill) REFERENCES skills (agent_id, name),
    FOREIGN KEY (agent_id, work_id) REFERENCES works (agent_id, id)
);
CREATE INDEX lessons_active ON lessons (agent_id, skill, at DESC) WHERE retired_at IS NULL;

CREATE TABLE learning_plans (
    agent_id   TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id         TEXT NOT NULL,
    goal_id    TEXT NOT NULL,
    skill      TEXT NOT NULL,
    stage      INT NOT NULL DEFAULT 0,
    status     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (agent_id, id),
    UNIQUE (agent_id, goal_id),
    FOREIGN KEY (agent_id, goal_id) REFERENCES goals (agent_id, id)
);

CREATE TABLE plan_stages (
    agent_id  TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    plan_id   TEXT NOT NULL,
    n         INT NOT NULL,
    learn     TEXT NOT NULL,
    practice  TEXT NOT NULL,
    done_when TEXT NOT NULL,
    done_at   TIMESTAMPTZ,
    PRIMARY KEY (agent_id, plan_id, n),
    FOREIGN KEY (agent_id, plan_id) REFERENCES learning_plans (agent_id, id)
);

CREATE TABLE resources (
    agent_id TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id       TEXT NOT NULL,
    plan_id  TEXT NOT NULL,
    stage    INT NOT NULL,
    ability  TEXT NOT NULL,
    query    TEXT NOT NULL,
    why      TEXT NOT NULL DEFAULT '',
    status   TEXT NOT NULL,
    title    TEXT NOT NULL DEFAULT '',
    work_id  TEXT,
    added_at TIMESTAMPTZ NOT NULL,
    done_at  TIMESTAMPTZ,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id, plan_id) REFERENCES learning_plans (agent_id, id),
    FOREIGN KEY (agent_id, work_id) REFERENCES works (agent_id, id)
);
CREATE INDEX resources_plan ON resources (agent_id, plan_id, stage);

-- Pages its own searches found: it may read only these.
CREATE TABLE web_found (
    agent_id TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    url      TEXT NOT NULL,
    title    TEXT NOT NULL,
    snippet  TEXT NOT NULL DEFAULT '',
    query    TEXT NOT NULL,
    found_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (agent_id, url)
);

-- Every search and page read, for the daily caps and what it cost.
CREATE TABLE web_log (
    agent_id TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id       BIGSERIAL,
    kind     TEXT NOT NULL,
    what     TEXT NOT NULL,
    cost_usd NUMERIC(14, 8) NOT NULL DEFAULT 0,
    at       TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (agent_id, id)
);
CREATE INDEX web_log_kind_at ON web_log (agent_id, kind, at DESC);

-- How its owner decides: past decisions as cases, and principles.
CREATE TABLE judgment_cases (
    agent_id      TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id            TEXT NOT NULL,
    key           TEXT NOT NULL,
    type          TEXT NOT NULL,
    situation     TEXT NOT NULL,
    options       TEXT[] NOT NULL DEFAULT '{}',
    choice        TEXT NOT NULL,
    reason        TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL,
    source        TEXT NOT NULL,
    at            TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL,
    outcome       TEXT NOT NULL DEFAULT '',
    embedding     REAL[],
    embedded_with TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (agent_id, id),
    UNIQUE (agent_id, key)
);

CREATE TABLE principles (
    agent_id   TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id         TEXT NOT NULL,
    text       TEXT NOT NULL,
    types      TEXT[] NOT NULL DEFAULT '{}',
    status     TEXT NOT NULL,
    source     TEXT NOT NULL,
    seed       TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    kind       TEXT NOT NULL DEFAULT 'principle',
    rank       INT NOT NULL DEFAULT 0,
    PRIMARY KEY (agent_id, id),
    UNIQUE (agent_id, seed)
);

CREATE TABLE principle_cases (
    agent_id     TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    principle_id TEXT NOT NULL,
    case_id      TEXT NOT NULL,
    PRIMARY KEY (agent_id, principle_id, case_id),
    FOREIGN KEY (agent_id, principle_id) REFERENCES principles (agent_id, id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id, case_id) REFERENCES judgment_cases (agent_id, id) ON DELETE CASCADE
);

CREATE TABLE judgment_calls (
    agent_id      TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id            TEXT NOT NULL,
    type          TEXT NOT NULL,
    mode          TEXT NOT NULL,
    situation     TEXT NOT NULL,
    options       TEXT[] NOT NULL,
    pick          INT NOT NULL,
    why           TEXT NOT NULL DEFAULT '',
    confidence    DOUBLE PRECISION NOT NULL DEFAULT 0,
    ref           TEXT,
    status        TEXT NOT NULL,
    waiting       TEXT NOT NULL DEFAULT '',
    waiting_since TIMESTAMPTZ,
    owner_pick    INT,
    owner_choice  TEXT NOT NULL DEFAULT '',
    owner_reason  TEXT NOT NULL DEFAULT '',
    agreed        BOOLEAN,
    answered_by   TEXT NOT NULL DEFAULT '',
    message       JSONB,
    case_id       TEXT,
    created_at    TIMESTAMPTZ NOT NULL,
    asked_at      TIMESTAMPTZ,
    answered_at   TIMESTAMPTZ,
    outcome       TEXT NOT NULL DEFAULT '',
    verdict       TEXT NOT NULL DEFAULT '',
    outcome_note  TEXT NOT NULL DEFAULT '',
    outcome_by    TEXT NOT NULL DEFAULT '',
    outcome_at    TIMESTAMPTZ,
    stakes        TEXT NOT NULL DEFAULT '',
    reversible    BOOLEAN NOT NULL DEFAULT true,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id, case_id) REFERENCES judgment_cases (agent_id, id)
);
CREATE INDEX judgment_calls_status ON judgment_calls (agent_id, status, created_at DESC);
CREATE INDEX judgment_calls_answered ON judgment_calls (agent_id, type, answered_at DESC) WHERE agreed IS NOT NULL;

CREATE TABLE tastes (
    agent_id   TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id         TEXT NOT NULL,
    kind       TEXT NOT NULL DEFAULT '',
    likes      BOOLEAN NOT NULL,
    text       TEXT NOT NULL,
    work_id    TEXT,
    source     TEXT NOT NULL,
    at         TIMESTAMPTZ NOT NULL,
    retired_at TIMESTAMPTZ,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id, work_id) REFERENCES works (agent_id, id)
);
CREATE INDEX tastes_active ON tastes (agent_id, at DESC) WHERE retired_at IS NULL;

-- Tool actions it proposed, and how they went.
CREATE TABLE actions (
    agent_id    TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id          TEXT NOT NULL,
    tool        TEXT NOT NULL,
    class       TEXT NOT NULL,
    args        TEXT NOT NULL,
    why         TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL,
    result      TEXT NOT NULL DEFAULT '',
    percept_id  TEXT,
    proposed_at TIMESTAMPTZ NOT NULL,
    done_at     TIMESTAMPTZ,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id, percept_id) REFERENCES percepts (agent_id, id)
);
CREATE INDEX actions_status ON actions (agent_id, status, proposed_at);

-- Its files, every version kept: character, work and judgment.
CREATE TABLE agent_versions (
    agent_id TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    id       TEXT NOT NULL,
    section  TEXT NOT NULL,
    doc      TEXT NOT NULL,
    source   TEXT NOT NULL,
    at       TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (agent_id, id)
);
CREATE INDEX agent_versions_section ON agent_versions (agent_id, section, at DESC, id DESC);

-- Its secrets, sealed.
CREATE TABLE secrets (
    agent_id TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    name     TEXT NOT NULL,
    sealed   BYTEA NOT NULL,
    set_at   TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (agent_id, name)
);

-- Agent Skills its owner installed, with their files.
CREATE TABLE agent_skills (
    agent_id    TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL,
    added_at    TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (agent_id, name)
);

CREATE TABLE agent_skill_files (
    agent_id TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    skill    TEXT NOT NULL,
    path     TEXT NOT NULL,
    content  BYTEA NOT NULL,
    PRIMARY KEY (agent_id, skill, path),
    FOREIGN KEY (agent_id, skill) REFERENCES agent_skills (agent_id, name) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE agent_skill_files, agent_skills, secrets, agent_versions, actions, tastes, judgment_calls,
    principle_cases, principles, judgment_cases, web_log, web_found, resources, plan_stages,
    learning_plans, lessons, skill_evidence, skills, autonomy_rules, approvals, works, intentions,
    projects, goals, dreams, sources, profile, facts, people, labels, decisions, percepts;
