ALTER TABLE organizations ADD COLUMN quote_validity_days INTEGER NOT NULL DEFAULT 30 CHECK(quote_validity_days BETWEEN 1 AND 365);
ALTER TABLE organizations ADD COLUMN quote_default_terms TEXT NOT NULL DEFAULT '';
ALTER TABLE organizations ADD COLUMN sales_settings_version INTEGER NOT NULL DEFAULT 1;
