-- +goose Up

ALTER TABLE llm_routes
    ADD COLUMN cost_per_1k_cache_write DOUBLE PRECISION,
    ADD COLUMN cost_per_1k_cache_read  DOUBLE PRECISION;

-- +goose Down

ALTER TABLE llm_routes
    DROP COLUMN IF EXISTS cost_per_1k_cache_write,
    DROP COLUMN IF EXISTS cost_per_1k_cache_read;
