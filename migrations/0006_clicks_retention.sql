-- +goose Up
-- Optimizes small, incremental retention deletes without scanning all clicks.
CREATE INDEX IF NOT EXISTS idx_clicks_retention_ts_id ON clicks (ts, id);

-- +goose Down
DROP INDEX IF EXISTS idx_clicks_retention_ts_id;
