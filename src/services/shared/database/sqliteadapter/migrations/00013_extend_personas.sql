-- Extend personas table: add columns for full persona support in desktop mode

-- +goose Up

ALTER TABLE personas ADD COLUMN soul_md TEXT NOT NULL DEFAULT '';
ALTER TABLE personas ADD COLUMN user_selectable INTEGER NOT NULL DEFAULT 0;
ALTER TABLE personas ADD COLUMN selector_name TEXT;
ALTER TABLE personas ADD COLUMN selector_order INTEGER;
ALTER TABLE personas ADD COLUMN roles_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE personas ADD COLUMN title_summarize_json TEXT;
ALTER TABLE personas ADD COLUMN updated_at TEXT NOT NULL DEFAULT (datetime('now'));
ALTER TABLE personas ADD COLUMN sync_mode TEXT NOT NULL DEFAULT 'none';
ALTER TABLE personas ADD COLUMN mirrored_file_dir TEXT;
ALTER TABLE personas ADD COLUMN last_synced_at TEXT;

-- +goose Down

ALTER TABLE personas DROP COLUMN soul_md;
ALTER TABLE personas DROP COLUMN user_selectable;
ALTER TABLE personas DROP COLUMN selector_name;
ALTER TABLE personas DROP COLUMN selector_order;
ALTER TABLE personas DROP COLUMN roles_json;
ALTER TABLE personas DROP COLUMN title_summarize_json;
ALTER TABLE personas DROP COLUMN updated_at;
ALTER TABLE personas DROP COLUMN sync_mode;
ALTER TABLE personas DROP COLUMN mirrored_file_dir;
ALTER TABLE personas DROP COLUMN last_synced_at;
