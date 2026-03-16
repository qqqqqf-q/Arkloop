-- Project and team tables: teams, team_memberships, projects

-- +goose Up

CREATE TABLE teams (
    id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    org_id     TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_teams_org_id ON teams(org_id);

CREATE TABLE team_memberships (
    team_id    TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT 'member',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (team_id, user_id)
);

CREATE INDEX idx_team_memberships_user_id ON team_memberships(user_id);

CREATE TABLE projects (
    id            TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    org_id        TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    team_id       TEXT REFERENCES teams(id) ON DELETE SET NULL,
    name          TEXT NOT NULL,
    description   TEXT,
    visibility    TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'team', 'org')),
    owner_user_id TEXT,
    deleted_at    TEXT,
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_projects_org_id ON projects(org_id);
CREATE INDEX idx_projects_team_id ON projects(team_id) WHERE team_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS idx_projects_team_id;
DROP INDEX IF EXISTS idx_projects_org_id;
DROP TABLE IF EXISTS projects;
DROP INDEX IF EXISTS idx_team_memberships_user_id;
DROP TABLE IF EXISTS team_memberships;
DROP INDEX IF EXISTS idx_teams_org_id;
DROP TABLE IF EXISTS teams;
