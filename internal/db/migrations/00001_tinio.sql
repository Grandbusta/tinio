-- +goose Up
-- Everything Tinio keeps, in one SQLite file: the owner's account, their agents, and
-- each agent's memory, every row tagged with its agent. Times are UTC text of one fixed
-- width (internal/db), so they sort and compare as text; lists are JSON arrays,
-- embeddings little-endian float32 BLOBs, booleans 0 or 1.

CREATE TABLE account_usage (
    account_id TEXT NOT NULL,
    week_start TEXT NOT NULL,
    week_usd REAL NOT NULL,
    hour_start TEXT NOT NULL,
    hour_usd REAL NOT NULL,
    PRIMARY KEY (account_id),
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
);
CREATE TABLE accounts (
    id TEXT NOT NULL,
    email TEXT NOT NULL,
    name TEXT DEFAULT '' NOT NULL,
    terms_accepted_at TEXT NOT NULL,
    adult_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    plan TEXT DEFAULT 'free' NOT NULL,
    CHECK (plan IN ('free', 'personal', 'pro', 'team')),
    UNIQUE (email),
    PRIMARY KEY (id)
);
CREATE TABLE actions (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    tool TEXT NOT NULL,
    class TEXT NOT NULL,
    args TEXT NOT NULL,
    why TEXT DEFAULT '' NOT NULL,
    status TEXT NOT NULL,
    result TEXT DEFAULT '' NOT NULL,
    percept_id TEXT,
    proposed_at TEXT NOT NULL,
    done_at TEXT,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id, percept_id) REFERENCES percepts(agent_id, id)
);
CREATE TABLE agent_runs (
    agent_id TEXT NOT NULL,
    worker TEXT,
    lease_until TEXT,
    born_at TEXT,
    wake_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000000Z', 'now')) NOT NULL,
    kept TEXT DEFAULT '{}' NOT NULL,
    CHECK ((worker IS NULL) = (lease_until IS NULL)),
    PRIMARY KEY (agent_id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE agent_skill_files (
    agent_id TEXT NOT NULL,
    skill TEXT NOT NULL,
    path TEXT NOT NULL,
    content BLOB NOT NULL,
    PRIMARY KEY (agent_id, skill, path),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id, skill) REFERENCES agent_skills(agent_id, name) ON DELETE CASCADE
);
CREATE TABLE agent_skills (
    agent_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    added_at TEXT NOT NULL,
    source TEXT DEFAULT '' NOT NULL,
    PRIMARY KEY (agent_id, name),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE agent_versions (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    section TEXT NOT NULL,
    doc TEXT NOT NULL,
    source TEXT NOT NULL,
    at TEXT NOT NULL,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE agents (
    id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    character_key TEXT DEFAULT '' NOT NULL,
    name TEXT NOT NULL,
    owner_name TEXT NOT NULL,
    relation TEXT NOT NULL,
    pace INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    character_version INTEGER DEFAULT 0 NOT NULL,
    owner_read_until TEXT DEFAULT '' NOT NULL,
    quiet_from TEXT DEFAULT '' NOT NULL,
    quiet_until TEXT DEFAULT '' NOT NULL,
    paused_at TEXT,
    PRIMARY KEY (id),
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
);
CREATE TABLE approvals (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    class TEXT NOT NULL,
    person TEXT DEFAULT '' NOT NULL,
    what TEXT NOT NULL,
    TEXT TEXT DEFAULT '' NOT NULL,
    kind TEXT DEFAULT '' NOT NULL,
    ref TEXT DEFAULT '' NOT NULL,
    status TEXT NOT NULL,
    asked_at TEXT NOT NULL,
    sent_at TEXT,
    decided_at TEXT,
    message TEXT,
    why TEXT DEFAULT '' NOT NULL,
    topic_id TEXT,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE autonomy_rules (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    class TEXT NOT NULL,
    verdict TEXT NOT NULL,
    set_by TEXT NOT NULL,
    percept_id TEXT,
    at TEXT NOT NULL,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id, percept_id) REFERENCES percepts(agent_id, id)
);
CREATE TABLE characters (
    key TEXT NOT NULL,
    version INTEGER NOT NULL,
    account_id TEXT,
    visibility TEXT NOT NULL,
    template TEXT NOT NULL,
    character TEXT NOT NULL,
    judgment TEXT NOT NULL,
    created_at TEXT NOT NULL,
    CHECK ((account_id IS NOT NULL) = (visibility = 'private')),
    CHECK (visibility IN ('ready', 'private', 'retired')),
    PRIMARY KEY (key, version),
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
);
CREATE TABLE chat_files (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    name TEXT NOT NULL,
    content_type TEXT NOT NULL,
    data BLOB NOT NULL,
    at TEXT NOT NULL,
    CHECK (length(data) <= 10485760),
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE chat_messages (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    author TEXT NOT NULL,
    TEXT TEXT NOT NULL,
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
CREATE TABLE connect_starts (
    state TEXT NOT NULL,
    account_id TEXT NOT NULL,
    connector TEXT NOT NULL,
    agent_id TEXT DEFAULT '' NOT NULL,
    access TEXT NOT NULL,
    secret BLOB NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (state),
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
);
CREATE TABLE connection_agents (
    connection_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    PRIMARY KEY (connection_id, agent_id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE
);
CREATE TABLE connections (
    id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    connector TEXT NOT NULL,
    label TEXT DEFAULT '' NOT NULL,
    access TEXT NOT NULL,
    secret BLOB NOT NULL,
    state TEXT DEFAULT 'ok' NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (state IN ('ok', 'needs_you')),
    UNIQUE (account_id, connector),
    PRIMARY KEY (id),
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
);
CREATE TABLE decisions (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    parent_id TEXT,
    at TEXT NOT NULL,
    percept_id TEXT,
    set_id TEXT NOT NULL,
    tier TEXT NOT NULL,
    provider TEXT DEFAULT '' NOT NULL,
    answers TEXT DEFAULT '{}' NOT NULL,
    action TEXT NOT NULL,
    reply TEXT,
    gate TEXT NOT NULL,
    latency_ms REAL DEFAULT 0 NOT NULL,
    cost_usd REAL DEFAULT 0 NOT NULL,
    outcome TEXT,
    affect TEXT,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id, parent_id) REFERENCES decisions(agent_id, id),
    FOREIGN KEY (agent_id, percept_id) REFERENCES percepts(agent_id, id)
);
CREATE TABLE dreams (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    TEXT TEXT NOT NULL,
    status TEXT NOT NULL,
    source TEXT NOT NULL,
    since TEXT NOT NULL,
    ended_at TEXT,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE facts (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    percept_id TEXT NOT NULL,
    subject TEXT DEFAULT '' NOT NULL,
    TEXT TEXT NOT NULL,
    at TEXT NOT NULL,
    areas TEXT DEFAULT '[]' NOT NULL,
    public INTEGER,
    asked_at TEXT,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id, percept_id) REFERENCES percepts(agent_id, id)
);
CREATE TABLE goals (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    dream_id TEXT NOT NULL,
    TEXT TEXT NOT NULL,
    done_when TEXT NOT NULL,
    due TEXT NOT NULL,
    status TEXT NOT NULL,
    progress TEXT DEFAULT '' NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    skill TEXT DEFAULT '' NOT NULL,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id, dream_id) REFERENCES dreams(agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE intentions (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    subject TEXT NOT NULL,
    percept_id TEXT,
    TEXT TEXT NOT NULL,
    due_at TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    kind TEXT DEFAULT 'remind' NOT NULL,
    project_id TEXT,
    result TEXT,
    done_at TEXT,
    ability TEXT DEFAULT '' NOT NULL,
    args TEXT DEFAULT '{}' NOT NULL,
    attempts INTEGER DEFAULT 0 NOT NULL,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id, percept_id) REFERENCES percepts(agent_id, id),
    FOREIGN KEY (agent_id, project_id) REFERENCES projects(agent_id, id)
);
CREATE TABLE judgment_calls (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    type TEXT NOT NULL,
    mode TEXT NOT NULL,
    situation TEXT NOT NULL,
    options TEXT NOT NULL,
    pick INTEGER NOT NULL,
    why TEXT DEFAULT '' NOT NULL,
    confidence REAL DEFAULT 0 NOT NULL,
    ref TEXT,
    status TEXT NOT NULL,
    waiting TEXT DEFAULT '' NOT NULL,
    waiting_since TEXT,
    owner_pick INTEGER,
    owner_choice TEXT DEFAULT '' NOT NULL,
    owner_reason TEXT DEFAULT '' NOT NULL,
    agreed INTEGER,
    answered_by TEXT DEFAULT '' NOT NULL,
    message TEXT,
    case_id TEXT,
    created_at TEXT NOT NULL,
    asked_at TEXT,
    answered_at TEXT,
    outcome TEXT DEFAULT '' NOT NULL,
    verdict TEXT DEFAULT '' NOT NULL,
    outcome_note TEXT DEFAULT '' NOT NULL,
    outcome_by TEXT DEFAULT '' NOT NULL,
    outcome_at TEXT,
    stakes TEXT DEFAULT '' NOT NULL,
    reversible INTEGER DEFAULT 1 NOT NULL,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id, case_id) REFERENCES judgment_cases(agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE judgment_cases (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    key TEXT NOT NULL,
    type TEXT NOT NULL,
    situation TEXT NOT NULL,
    options TEXT DEFAULT '[]' NOT NULL,
    choice TEXT NOT NULL,
    reason TEXT DEFAULT '' NOT NULL,
    status TEXT NOT NULL,
    source TEXT NOT NULL,
    at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    outcome TEXT DEFAULT '' NOT NULL,
    embedding BLOB,
    embedded_with TEXT DEFAULT '' NOT NULL,
    UNIQUE (agent_id, key),
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE labels (
    agent_id TEXT NOT NULL,
    decision_id TEXT NOT NULL,
    question TEXT NOT NULL,
    value TEXT NOT NULL,
    source TEXT NOT NULL,
    at TEXT NOT NULL,
    PRIMARY KEY (agent_id, decision_id, question),
    FOREIGN KEY (agent_id, decision_id) REFERENCES decisions(agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE learning_plans (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    goal_id TEXT NOT NULL,
    skill TEXT NOT NULL,
    stage INTEGER DEFAULT 0 NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (agent_id, goal_id),
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id, goal_id) REFERENCES goals(agent_id, id)
);
CREATE TABLE lessons (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    skill TEXT NOT NULL,
    TEXT TEXT NOT NULL,
    source TEXT NOT NULL,
    work_id TEXT,
    at TEXT NOT NULL,
    retired_at TEXT,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id, skill) REFERENCES skills(agent_id, name),
    FOREIGN KEY (agent_id, work_id) REFERENCES works(agent_id, id)
);
CREATE TABLE people (
    agent_id TEXT NOT NULL,
    key TEXT NOT NULL,
    name TEXT DEFAULT '' NOT NULL,
    relation TEXT DEFAULT '' NOT NULL,
    circle TEXT DEFAULT '' NOT NULL,
    source TEXT NOT NULL,
    first_seen TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (agent_id, key),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE percepts (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    source TEXT NOT NULL,
    external_id TEXT NOT NULL,
    thread TEXT DEFAULT '' NOT NULL,
    speaker TEXT DEFAULT '' NOT NULL,
    kind TEXT NOT NULL,
    TEXT TEXT DEFAULT '' NOT NULL,
    payload TEXT,
    at TEXT NOT NULL,
    topic_id TEXT,
    UNIQUE (agent_id, source, external_id),
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE plan_stages (
    agent_id TEXT NOT NULL,
    plan_id TEXT NOT NULL,
    n INTEGER NOT NULL,
    learn TEXT NOT NULL,
    practice TEXT NOT NULL,
    done_when TEXT NOT NULL,
    done_at TEXT,
    PRIMARY KEY (agent_id, plan_id, n),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id, plan_id) REFERENCES learning_plans(agent_id, id)
);
CREATE TABLE principle_cases (
    agent_id TEXT NOT NULL,
    principle_id TEXT NOT NULL,
    case_id TEXT NOT NULL,
    PRIMARY KEY (agent_id, principle_id, case_id),
    FOREIGN KEY (agent_id, case_id) REFERENCES judgment_cases(agent_id, id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id, principle_id) REFERENCES principles(agent_id, id) ON DELETE CASCADE
);
CREATE TABLE principles (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    TEXT TEXT NOT NULL,
    types TEXT DEFAULT '[]' NOT NULL,
    status TEXT NOT NULL,
    source TEXT NOT NULL,
    seed TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    kind TEXT DEFAULT 'principle' NOT NULL,
    rank INTEGER DEFAULT 0 NOT NULL,
    UNIQUE (agent_id, seed),
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE profile (
    agent_id TEXT NOT NULL,
    page TEXT NOT NULL,
    inputs TEXT NOT NULL,
    written_at TEXT NOT NULL,
    PRIMARY KEY (agent_id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE projects (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    title TEXT NOT NULL,
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    notes TEXT DEFAULT '' NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    source_id TEXT,
    position INTEGER DEFAULT 0 NOT NULL,
    goal_id TEXT,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id, goal_id) REFERENCES goals(agent_id, id),
    FOREIGN KEY (source_id) REFERENCES sources(id)
);
CREATE TABLE resources (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    plan_id TEXT NOT NULL,
    stage INTEGER NOT NULL,
    ability TEXT NOT NULL,
    query TEXT NOT NULL,
    why TEXT DEFAULT '' NOT NULL,
    status TEXT NOT NULL,
    title TEXT DEFAULT '' NOT NULL,
    work_id TEXT,
    added_at TEXT NOT NULL,
    done_at TEXT,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id, plan_id) REFERENCES learning_plans(agent_id, id),
    FOREIGN KEY (agent_id, work_id) REFERENCES works(agent_id, id)
);
CREATE TABLE secrets (
    agent_id TEXT NOT NULL,
    name TEXT NOT NULL,
    sealed BLOB NOT NULL,
    set_at TEXT NOT NULL,
    PRIMARY KEY (agent_id, name),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE sessions (
    id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    token_hash BLOB NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (token_hash),
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
);
CREATE TABLE skill_evidence (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    skill TEXT NOT NULL,
    work_id TEXT,
    score REAL NOT NULL,
    source TEXT NOT NULL,
    note TEXT DEFAULT '' NOT NULL,
    at TEXT NOT NULL,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id, skill) REFERENCES skills(agent_id, name),
    FOREIGN KEY (agent_id, work_id) REFERENCES works(agent_id, id)
);
CREATE TABLE skills (
    agent_id TEXT NOT NULL,
    name TEXT NOT NULL,
    level REAL NOT NULL,
    shown_by TEXT DEFAULT '[]' NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (agent_id, name),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE sources (
    id TEXT NOT NULL,
    kind TEXT NOT NULL,
    title TEXT NOT NULL,
    author TEXT DEFAULT '' NOT NULL,
    url TEXT NOT NULL,
    TEXT TEXT NOT NULL,
    chars INTEGER NOT NULL,
    fetched_at TEXT NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (url)
);
CREATE TABLE tastes (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    kind TEXT DEFAULT '' NOT NULL,
    likes INTEGER NOT NULL,
    TEXT TEXT NOT NULL,
    work_id TEXT,
    source TEXT NOT NULL,
    at TEXT NOT NULL,
    retired_at TEXT,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id, work_id) REFERENCES works(agent_id, id)
);
CREATE TABLE topics (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    person TEXT NOT NULL,
    title TEXT DEFAULT '' NOT NULL,
    summary TEXT DEFAULT '' NOT NULL,
    status TEXT DEFAULT '' NOT NULL,
    settled TEXT DEFAULT '[]' NOT NULL,
    state TEXT DEFAULT 'open' NOT NULL,
    started_at TEXT NOT NULL,
    last_at TEXT NOT NULL,
    folded_at TEXT,
    summed_at TEXT,
    embedding BLOB,
    embedded_with TEXT DEFAULT '' NOT NULL,
    CHECK (state IN ('open', 'quiet')),
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE web_found (
    agent_id TEXT NOT NULL,
    url TEXT NOT NULL,
    title TEXT NOT NULL,
    snippet TEXT DEFAULT '' NOT NULL,
    query TEXT NOT NULL,
    found_at TEXT NOT NULL,
    PRIMARY KEY (agent_id, url),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE web_log (
    agent_id TEXT NOT NULL,
    id INTEGER PRIMARY KEY,
    kind TEXT NOT NULL,
    what TEXT NOT NULL,
    cost_usd REAL DEFAULT 0 NOT NULL,
    at TEXT NOT NULL,
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE works (
    agent_id TEXT NOT NULL,
    id TEXT NOT NULL,
    intention_id TEXT,
    project_id TEXT,
    kind TEXT NOT NULL,
    title TEXT NOT NULL,
    TEXT TEXT NOT NULL,
    public INTEGER,
    created_at TEXT NOT NULL,
    public_by TEXT,
    review_asked_at TEXT,
    embedding BLOB,
    embedded_with TEXT DEFAULT '' NOT NULL,
    data TEXT,
    data_kind TEXT DEFAULT '' NOT NULL,
    PRIMARY KEY (agent_id, id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id, intention_id) REFERENCES intentions(agent_id, id),
    FOREIGN KEY (agent_id, project_id) REFERENCES projects(agent_id, id)
);
CREATE INDEX actions_status ON actions (agent_id, status, proposed_at);
CREATE INDEX agent_runs_wake ON agent_runs (wake_at) WHERE (worker IS NULL);
CREATE INDEX agent_versions_section ON agent_versions (agent_id, section, at DESC, id DESC);
CREATE INDEX agents_account ON agents (account_id, created_at);
CREATE INDEX approvals_pending ON approvals (agent_id, asked_at) WHERE (status = 'pending');
CREATE INDEX autonomy_rules_class ON autonomy_rules (agent_id, class, at DESC);
CREATE INDEX characters_account ON characters (account_id, key) WHERE (account_id IS NOT NULL);
CREATE INDEX chat_messages_topic ON chat_messages (agent_id, topic_id, id) WHERE (topic_id IS NOT NULL);
CREATE INDEX connection_agents_agent ON connection_agents (agent_id);
CREATE INDEX decisions_at ON decisions (agent_id, at DESC);
CREATE INDEX decisions_percept ON decisions (agent_id, percept_id);
CREATE INDEX facts_at ON facts (agent_id, at DESC);
CREATE INDEX facts_subject ON facts (agent_id, subject, at DESC);
CREATE INDEX intentions_due ON intentions (agent_id, due_at) WHERE (status = 'pending');
CREATE INDEX judgment_calls_answered ON judgment_calls (agent_id, type, answered_at DESC) WHERE (agreed IS NOT NULL);
CREATE INDEX judgment_calls_status ON judgment_calls (agent_id, status, created_at DESC);
CREATE INDEX lessons_active ON lessons (agent_id, skill, at DESC) WHERE (retired_at IS NULL);
CREATE INDEX percepts_by_speaker ON percepts (agent_id, speaker, kind, at DESC);
CREATE INDEX percepts_thread ON percepts (agent_id, source, thread, at DESC);
CREATE INDEX percepts_topic ON percepts (agent_id, topic_id, at DESC) WHERE (topic_id IS NOT NULL);
CREATE INDEX resources_plan ON resources (agent_id, plan_id, stage);
CREATE INDEX sessions_account ON sessions (account_id);
CREATE INDEX skill_evidence_skill ON skill_evidence (agent_id, skill, at DESC);
CREATE INDEX tastes_active ON tastes (agent_id, at DESC) WHERE (retired_at IS NULL);
CREATE INDEX topics_recent ON topics (agent_id, person, last_at DESC);
CREATE INDEX web_log_kind_at ON web_log (agent_id, kind, at DESC);
CREATE INDEX works_created ON works (agent_id, created_at DESC);

-- +goose Down
DROP TABLE works;
DROP TABLE web_log;
DROP TABLE web_found;
DROP TABLE topics;
DROP TABLE tastes;
DROP TABLE sources;
DROP TABLE skills;
DROP TABLE skill_evidence;
DROP TABLE sessions;
DROP TABLE secrets;
DROP TABLE resources;
DROP TABLE projects;
DROP TABLE profile;
DROP TABLE principles;
DROP TABLE principle_cases;
DROP TABLE plan_stages;
DROP TABLE percepts;
DROP TABLE people;
DROP TABLE lessons;
DROP TABLE learning_plans;
DROP TABLE labels;
DROP TABLE judgment_cases;
DROP TABLE judgment_calls;
DROP TABLE intentions;
DROP TABLE goals;
DROP TABLE facts;
DROP TABLE dreams;
DROP TABLE decisions;
DROP TABLE connections;
DROP TABLE connection_agents;
DROP TABLE connect_starts;
DROP TABLE chat_messages;
DROP TABLE chat_files;
DROP TABLE characters;
DROP TABLE autonomy_rules;
DROP TABLE approvals;
DROP TABLE agents;
DROP TABLE agent_versions;
DROP TABLE agent_skills;
DROP TABLE agent_skill_files;
DROP TABLE agent_runs;
DROP TABLE actions;
DROP TABLE accounts;
DROP TABLE account_usage;
