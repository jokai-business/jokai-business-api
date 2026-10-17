ALTER TABLE organizations ADD COLUMN document_max_mib INTEGER NOT NULL DEFAULT 10 CHECK (document_max_mib BETWEEN 1 AND 10);
ALTER TABLE organizations ADD COLUMN document_allow_pdf BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE organizations ADD COLUMN document_allow_png BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE organizations ADD COLUMN document_allow_jpeg BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE organizations ADD COLUMN document_policy_version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE organizations ADD CONSTRAINT document_policy_has_type CHECK (document_allow_pdf OR document_allow_png OR document_allow_jpeg);
