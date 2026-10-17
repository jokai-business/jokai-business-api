ALTER TABLE organizations
 ADD COLUMN address TEXT NOT NULL DEFAULT '' CHECK(length(address)<=1000),
 ADD COLUMN contact_email TEXT NOT NULL DEFAULT '' CHECK(length(contact_email)<=254),
 ADD COLUMN phone TEXT NOT NULL DEFAULT '' CHECK(length(phone)<=50),
 ADD COLUMN timezone TEXT NOT NULL DEFAULT 'UTC',
 ADD COLUMN default_currency TEXT NOT NULL DEFAULT 'USD' CHECK(default_currency ~ '^[A-Z]{3}$'),
 ADD COLUMN settings_version INTEGER NOT NULL DEFAULT 1 CHECK(settings_version>0),
 ADD COLUMN settings_updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 ADD COLUMN logo_id UUID,
 ADD COLUMN logo_storage_id TEXT NOT NULL DEFAULT '',
 ADD COLUMN logo_sha256 TEXT NOT NULL DEFAULT '';
INSERT INTO permissions(key) VALUES ('settings.manage');
INSERT INTO role_permissions(role_key,permission_key) VALUES('organization_admin','settings.manage');
