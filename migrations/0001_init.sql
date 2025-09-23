-- +goose Up
CREATE TABLE IF NOT EXISTS links (
  code        TEXT PRIMARY KEY,
  long_url    TEXT NOT NULL CHECK (length(long_url) <= 2048),
  title       TEXT NULL,
  description TEXT NULL,
  image_url   TEXT NULL,
  site_name   TEXT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at  TIMESTAMPTZ NULL,
  is_active   BOOLEAN NOT NULL DEFAULT TRUE,
  max_clicks  BIGINT NULL CHECK (max_clicks >= 0),
  click_count BIGINT NOT NULL DEFAULT 0,
  custom      BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_links_code ON links(code);

-- +goose Down
DROP TABLE IF EXISTS links;
