-- +goose Up
-- Folders on this computer the owner added, for the agents they chose: read only, or
-- read and make files (each change asked first unless an agent's settings say
-- otherwise). The path is as the owner picked it; what's inside is never copied here.
CREATE TABLE folders (
    id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    name TEXT NOT NULL,
    path TEXT NOT NULL,
    can_write INTEGER DEFAULT 0 NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (account_id, path),
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
);
CREATE TABLE folder_agents (
    folder_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    PRIMARY KEY (folder_id, agent_id),
    FOREIGN KEY (folder_id) REFERENCES folders(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE INDEX folder_agents_agent ON folder_agents (agent_id);

-- +goose Down
DROP TABLE folder_agents;
DROP TABLE folders;
