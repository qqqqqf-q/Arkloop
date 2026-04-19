-- +goose Up
CREATE INDEX idx_outbox_cleanup ON channel_delivery_outbox (status, updated_at)
    WHERE status IN ('sent', 'dead');

-- +goose Down
DROP INDEX IF EXISTS idx_outbox_cleanup;
