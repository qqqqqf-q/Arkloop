-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE orgs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    CONSTRAINT uq_orgs_slug UNIQUE (slug)
);

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE TABLE org_memberships (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'member',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    CONSTRAINT uq_org_memberships_org_id_user_id UNIQUE (org_id, user_id)
);

CREATE INDEX ix_org_memberships_org_id ON org_memberships(org_id);
CREATE INDEX ix_org_memberships_user_id ON org_memberships(user_id);

-- +goose Down
DROP INDEX IF EXISTS ix_org_memberships_user_id;
DROP INDEX IF EXISTS ix_org_memberships_org_id;
DROP TABLE IF EXISTS org_memberships;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS orgs;
