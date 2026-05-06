-- +goose Up
ALTER TABLE links
  ADD COLUMN IF NOT EXISTS tenant_id TEXT NOT NULL DEFAULT 'default';

ALTER TABLE clicks
  ADD COLUMN IF NOT EXISTS tenant_id TEXT NOT NULL DEFAULT 'default';

-- Backfill clicks.tenant_id from links if available
UPDATE clicks c
SET tenant_id = l.tenant_id
FROM links l
WHERE c.code = l.code
  AND (c.tenant_id IS NULL OR c.tenant_id = '' OR c.tenant_id = 'default');

-- Composite uniqueness for tenant-scoped relations
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'links_tenant_code_unique'
  ) THEN
    ALTER TABLE links ADD CONSTRAINT links_tenant_code_unique UNIQUE (tenant_id, code);
  END IF;
END $$;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'clicks_tenant_code_fk'
  ) THEN
    ALTER TABLE clicks
      ADD CONSTRAINT clicks_tenant_code_fk
      FOREIGN KEY (tenant_id, code) REFERENCES links(tenant_id, code)
      ON DELETE CASCADE;
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_links_tenant_created ON links(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_clicks_tenant_code_ts ON clicks(tenant_id, code, ts DESC);

-- +goose Down
ALTER TABLE clicks DROP CONSTRAINT IF EXISTS clicks_tenant_code_fk;
ALTER TABLE links DROP CONSTRAINT IF EXISTS links_tenant_code_unique;

DROP INDEX IF EXISTS idx_clicks_tenant_code_ts;
DROP INDEX IF EXISTS idx_links_tenant_created;

ALTER TABLE clicks DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE links DROP COLUMN IF EXISTS tenant_id;
