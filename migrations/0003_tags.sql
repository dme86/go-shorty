-- +goose Up
ALTER TABLE links ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT '{}';
CREATE INDEX IF NOT EXISTS idx_links_tags ON links USING GIN (tags);

-- +goose Down
ALTER TABLE links DROP COLUMN IF EXISTS tags;
