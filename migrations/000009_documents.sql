INSERT INTO permissions(key) VALUES ('document.read'),('document.upload'),('document.review'),('document.sales'),('document.corporate'),('document.pmo'),('document.finance') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions SELECT 'organization_admin',key FROM permissions WHERE key LIKE 'document.%' ON CONFLICT DO NOTHING;
INSERT INTO role_permissions VALUES
 ('sales_manager','document.read'),('sales_manager','document.upload'),('sales_manager','document.review'),('sales_manager','document.sales'),
 ('sales_user','document.read'),('sales_user','document.upload'),('sales_user','document.sales'),
 ('delivery_user','document.read'),('delivery_user','document.upload'),('delivery_user','document.review'),('delivery_user','document.pmo'),
 ('finance_user','document.read'),('finance_user','document.upload'),('finance_user','document.review'),('finance_user','document.finance'),
 ('executive','document.sales'),('executive','document.pmo'),('executive','document.finance') ON CONFLICT DO NOTHING;
CREATE TABLE documents (
 id UUID PRIMARY KEY, organization_id UUID NOT NULL REFERENCES organizations(id),
 title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200),
 workspace TEXT NOT NULL CHECK(workspace IN ('sales','corporate','pmo','finance')),
 customer_id UUID, filename TEXT NOT NULL, content_type TEXT NOT NULL,
 size_bytes BIGINT NOT NULL CHECK(size_bytes BETWEEN 1 AND 10485760), sha256 TEXT NOT NULL,
 notes TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','accepted','rejected')),
 uploaded_by UUID NOT NULL, reviewed_by UUID, review_note TEXT NOT NULL DEFAULT '',
 version INTEGER NOT NULL DEFAULT 1, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), reviewed_at TIMESTAMPTZ,
 UNIQUE(organization_id,id),
 FOREIGN KEY(organization_id,customer_id) REFERENCES customers(organization_id,id),
 FOREIGN KEY(organization_id,uploaded_by) REFERENCES users(organization_id,id),
 FOREIGN KEY(organization_id,reviewed_by) REFERENCES users(organization_id,id)
);
CREATE INDEX documents_org_created ON documents(organization_id,created_at DESC,id);
CREATE INDEX documents_org_workspace ON documents(organization_id,workspace,status);
CREATE INDEX documents_org_customer ON documents(organization_id,customer_id);
