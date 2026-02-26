-- +goose Up

ALTER TABLE usage_records
    ADD COLUMN cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN cache_read_tokens     BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN cached_tokens         BIGINT NOT NULL DEFAULT 0;

-- +goose Down

ALTER TABLE usage_records
    DROP COLUMN IF EXISTS cache_creation_tokens,
    DROP COLUMN IF EXISTS cache_read_tokens,
    DROP COLUMN IF EXISTS cached_tokens;
