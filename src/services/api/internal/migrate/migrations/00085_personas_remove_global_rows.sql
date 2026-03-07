-- +goose Up
DELETE FROM personas WHERE org_id IS NULL;

-- +goose Down
-- irreversible: deleted builtin rows must be restored manually if needed
SELECT 1;
