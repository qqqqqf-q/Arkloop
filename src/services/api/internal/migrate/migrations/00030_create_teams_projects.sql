-- +goose Up

CREATE TABLE teams (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_teams_org_id ON teams(org_id);

CREATE TABLE team_memberships (
    team_id    UUID        NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT        NOT NULL DEFAULT 'member',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, user_id)
);

CREATE INDEX idx_team_memberships_user_id ON team_memberships(user_id);

CREATE TABLE projects (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    team_id     UUID        REFERENCES teams(id) ON DELETE SET NULL,
    name        TEXT        NOT NULL,
    description TEXT,
    visibility  TEXT        NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'team', 'org')),
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_projects_org_id ON projects(org_id);
CREATE INDEX idx_projects_team_id ON projects(team_id) WHERE team_id IS NOT NULL;

ALTER TABLE threads
    ADD CONSTRAINT fk_threads_project_id
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL;

-- +goose Down

ALTER TABLE threads DROP CONSTRAINT IF EXISTS fk_threads_project_id;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS team_memberships;
DROP TABLE IF EXISTS teams;
