-- +goose Up
ALTER TABLE subscriptions ADD COLUMN current_period_start TEXT NOT NULL DEFAULT (datetime('now'));
ALTER TABLE subscriptions ADD COLUMN current_period_end   TEXT NOT NULL DEFAULT (datetime('now', '+100 years'));
ALTER TABLE subscriptions ADD COLUMN cancelled_at         TEXT;
