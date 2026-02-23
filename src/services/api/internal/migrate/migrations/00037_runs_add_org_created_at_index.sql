-- +goose Up

CREATE INDEX ix_runs_org_id_created_at_id ON runs(org_id, created_at DESC, id DESC) WHERE deleted_at IS NULL;

-- +goose Down

DROP INDEX IF EXISTS ix_runs_org_id_created_at_id;
