-- +goose Up
-- Existing legacy tenant slugs (e.g. a-b-com) are ambiguous and MUST NOT be
-- automatically assigned to a new canonical tenant. Operators must verify
-- ownership of each link and explicitly rekey it using scripts/rekey-link.sql.
-- Permit atomic rekey of links and clicks in the same transaction.
ALTER TABLE clicks
  ALTER CONSTRAINT clicks_tenant_code_fk DEFERRABLE INITIALLY IMMEDIATE;

-- +goose Down
ALTER TABLE clicks
  ALTER CONSTRAINT clicks_tenant_code_fk NOT DEFERRABLE;
