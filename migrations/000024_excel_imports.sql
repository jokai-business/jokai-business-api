CREATE TABLE excel_imports (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),organization_id UUID NOT NULL,user_id UUID NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('customer','opportunity')),file_hash TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'preview' CHECK(status IN ('preview','imported')),
 rows JSONB NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT now(),confirmed_at TIMESTAMPTZ,
 FOREIGN KEY(organization_id,user_id) REFERENCES users(organization_id,id) ON DELETE CASCADE
);
CREATE INDEX excel_import_owner ON excel_imports(organization_id,user_id,created_at);
CREATE UNIQUE INDEX excel_import_once ON excel_imports(organization_id,kind,file_hash) WHERE status='imported';
