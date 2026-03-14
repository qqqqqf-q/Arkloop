-- +goose Up

ALTER TABLE sub_agents RENAME COLUMN org_id TO account_id;
ALTER INDEX idx_sub_agents_org_id RENAME TO idx_sub_agents_account_id;
ALTER TABLE sub_agents RENAME CONSTRAINT sub_agents_org_id_fkey TO sub_agents_account_id_fkey;

-- +goose Down

ALTER TABLE sub_agents RENAME CONSTRAINT sub_agents_account_id_fkey TO sub_agents_org_id_fkey;
ALTER INDEX idx_sub_agents_account_id RENAME TO idx_sub_agents_org_id;
ALTER TABLE sub_agents RENAME COLUMN account_id TO org_id;
