-- +goose Up

ALTER TABLE activity_recorder_builder_state
    ADD COLUMN last_finish_status TEXT NOT NULL DEFAULT '';

ALTER TABLE activity_recorder_builder_state
    ADD COLUMN last_finish_reason TEXT NOT NULL DEFAULT '';

ALTER TABLE activity_recorder_builder_state
    ADD COLUMN last_sources_checked TEXT NOT NULL DEFAULT '[]';

ALTER TABLE activity_recorder_builder_state
    ADD COLUMN last_sources_unavailable TEXT NOT NULL DEFAULT '[]';

ALTER TABLE activity_recorder_builder_state
    ADD COLUMN last_memory_write_count INT NOT NULL DEFAULT 0;

ALTER TABLE activity_recorder_builder_state
    ADD COLUMN last_finished_at TEXT;

-- +goose Down

ALTER TABLE activity_recorder_builder_state
    DROP COLUMN last_finished_at;

ALTER TABLE activity_recorder_builder_state
    DROP COLUMN last_memory_write_count;

ALTER TABLE activity_recorder_builder_state
    DROP COLUMN last_sources_unavailable;

ALTER TABLE activity_recorder_builder_state
    DROP COLUMN last_sources_checked;

ALTER TABLE activity_recorder_builder_state
    DROP COLUMN last_finish_reason;

ALTER TABLE activity_recorder_builder_state
    DROP COLUMN last_finish_status;
