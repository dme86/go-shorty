-- +goose Up
CREATE TABLE IF NOT EXISTS clicks (
  id          BIGSERIAL PRIMARY KEY,
  code        TEXT NOT NULL REFERENCES links(code) ON DELETE CASCADE,
  ts          TIMESTAMPTZ NOT NULL DEFAULT now(),
  ip_hash     BYTEA NULL,
  ua          TEXT NULL,
  referer     TEXT NULL,
  country     CHAR(2) NULL
);
CREATE INDEX IF NOT EXISTS idx_clicks_code_ts ON clicks(code, ts DESC);

-- +goose Down
DROP TABLE IF EXISTS clicks;
