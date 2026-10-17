INSERT INTO permissions(key) VALUES ('product.read'),('product.manage');
INSERT INTO role_permissions SELECT role_key,'product.read' FROM role_permissions WHERE permission_key='quote.read';
INSERT INTO role_permissions VALUES ('organization_admin','product.manage'),('sales_manager','product.manage');
CREATE TABLE products (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL REFERENCES organizations(id),
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200), description TEXT NOT NULL DEFAULT '', unit TEXT NOT NULL CHECK(length(unit) BETWEEN 1 AND 40),
 active BOOLEAN NOT NULL DEFAULT true, version INTEGER NOT NULL DEFAULT 1, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), UNIQUE(organization_id,id)
);
CREATE INDEX products_org_name ON products(organization_id,name,id);
CREATE TABLE product_prices (
 organization_id UUID NOT NULL, product_id UUID NOT NULL, currency TEXT NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), amount NUMERIC(14,2) NOT NULL CHECK(amount>=0),
 PRIMARY KEY(organization_id,product_id,currency), FOREIGN KEY(organization_id,product_id) REFERENCES products(organization_id,id)
);
CREATE TABLE quote_counters (organization_id UUID PRIMARY KEY REFERENCES organizations(id), next_number BIGINT NOT NULL);
CREATE TABLE quotes (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL REFERENCES organizations(id),
 customer_id UUID NOT NULL, opportunity_id UUID NOT NULL, creator_id UUID NOT NULL,
 number TEXT NOT NULL, company_name TEXT NOT NULL, customer_name TEXT NOT NULL, customer_email TEXT NOT NULL, opportunity_title TEXT NOT NULL,
 currency TEXT NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), status TEXT NOT NULL CHECK(status IN ('draft','submitted','approved','rejected','sent','accepted')),
 expiry_date DATE NOT NULL, terms TEXT NOT NULL, subtotal NUMERIC(16,2) NOT NULL, discount_total NUMERIC(16,2) NOT NULL, total NUMERIC(16,2) NOT NULL,
 version INTEGER NOT NULL DEFAULT 1, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(organization_id,id), UNIQUE(organization_id,number),
 FOREIGN KEY(organization_id,customer_id) REFERENCES customers(organization_id,id), FOREIGN KEY(organization_id,opportunity_id) REFERENCES opportunities(organization_id,id), FOREIGN KEY(organization_id,creator_id) REFERENCES users(organization_id,id)
);
CREATE INDEX quotes_org_created ON quotes(organization_id,created_at DESC,id);
CREATE INDEX quotes_org_opportunity ON quotes(organization_id,opportunity_id);
CREATE INDEX quotes_org_customer ON quotes(organization_id,customer_id);
CREATE TABLE quote_items (
 organization_id UUID NOT NULL, quote_id UUID NOT NULL, position INTEGER NOT NULL, product_id UUID NOT NULL,
 name TEXT NOT NULL, description TEXT NOT NULL, unit TEXT NOT NULL, quantity NUMERIC(10,3) NOT NULL CHECK(quantity>0), unit_price NUMERIC(14,2) NOT NULL CHECK(unit_price>=0),
 discount_percent NUMERIC(5,2) NOT NULL CHECK(discount_percent BETWEEN 0 AND 100), subtotal NUMERIC(16,2) NOT NULL, discount_amount NUMERIC(16,2) NOT NULL, total NUMERIC(16,2) NOT NULL,
 PRIMARY KEY(organization_id,quote_id,position), FOREIGN KEY(organization_id,quote_id) REFERENCES quotes(organization_id,id), FOREIGN KEY(organization_id,product_id) REFERENCES products(organization_id,id)
);
CREATE TABLE approval_requests (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL, quote_id UUID NOT NULL, requested_by UUID NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('pending','approved','rejected')), decided_by UUID, note TEXT NOT NULL DEFAULT '', requested_at TIMESTAMPTZ NOT NULL DEFAULT now(), decided_at TIMESTAMPTZ,
 FOREIGN KEY(organization_id,quote_id) REFERENCES quotes(organization_id,id), FOREIGN KEY(organization_id,requested_by) REFERENCES users(organization_id,id), FOREIGN KEY(organization_id,decided_by) REFERENCES users(organization_id,id)
);
CREATE UNIQUE INDEX approval_pending ON approval_requests(organization_id,quote_id) WHERE status='pending';
CREATE INDEX approval_quote ON approval_requests(organization_id,quote_id,requested_at DESC);
