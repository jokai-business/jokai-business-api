CREATE TABLE customers (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL REFERENCES organizations(id),
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200), email TEXT NOT NULL DEFAULT '', phone TEXT NOT NULL DEFAULT '',
 website TEXT NOT NULL DEFAULT '', industry TEXT NOT NULL DEFAULT '', country TEXT NOT NULL DEFAULT '',
 owner_id UUID NOT NULL, status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','inactive','prospect')),
 notes TEXT NOT NULL DEFAULT '', version INTEGER NOT NULL DEFAULT 1,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(organization_id,id), FOREIGN KEY(organization_id,owner_id) REFERENCES users(organization_id,id)
);
CREATE INDEX customers_org_created ON customers(organization_id,created_at DESC,id);
CREATE INDEX customers_org_status ON customers(organization_id,status);
CREATE INDEX customers_org_owner ON customers(organization_id,owner_id);
CREATE TABLE contacts (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL REFERENCES organizations(id), customer_id UUID NOT NULL,
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200), email TEXT NOT NULL DEFAULT '', phone TEXT NOT NULL DEFAULT '',
 title TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY(organization_id,customer_id) REFERENCES customers(organization_id,id)
);
CREATE INDEX contacts_org_customer ON contacts(organization_id,customer_id,created_at,id);
