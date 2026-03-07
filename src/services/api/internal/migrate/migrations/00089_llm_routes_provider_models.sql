-- +goose Up

ALTER TABLE llm_routes
    ADD COLUMN tags TEXT[] NOT NULL DEFAULT '{}'::text[];

WITH ranked_duplicates AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY credential_id, lower(model)
               ORDER BY priority DESC, is_default DESC, created_at ASC, id ASC
           ) AS row_num
    FROM llm_routes
)
DELETE FROM llm_routes r
USING ranked_duplicates d
WHERE r.id = d.id
  AND d.row_num > 1;

WITH ranked_defaults AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY credential_id
               ORDER BY priority DESC, created_at ASC, id ASC
           ) AS row_num
    FROM llm_routes
    WHERE is_default = TRUE
)
UPDATE llm_routes r
SET is_default = FALSE
FROM ranked_defaults d
WHERE r.id = d.id
  AND d.row_num > 1;

CREATE UNIQUE INDEX ux_llm_routes_credential_model_lower
    ON llm_routes (credential_id, lower(model));

CREATE UNIQUE INDEX ux_llm_routes_credential_default
    ON llm_routes (credential_id)
    WHERE is_default = TRUE;

-- +goose Down

DROP INDEX IF EXISTS ux_llm_routes_credential_default;
DROP INDEX IF EXISTS ux_llm_routes_credential_model_lower;

ALTER TABLE llm_routes
    DROP COLUMN IF EXISTS tags;
