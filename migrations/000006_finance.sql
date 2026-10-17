CREATE TABLE invoice_counters (organization_id UUID PRIMARY KEY REFERENCES organizations(id), next_number BIGINT NOT NULL);
CREATE TABLE invoices (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL REFERENCES organizations(id),
 quote_id UUID NOT NULL, customer_id UUID NOT NULL, project_id UUID, creator_id UUID NOT NULL,
 number TEXT NOT NULL, quote_number TEXT NOT NULL, company_name TEXT NOT NULL, customer_name TEXT NOT NULL, customer_email TEXT NOT NULL,
 currency TEXT NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), subtotal NUMERIC(16,2) NOT NULL CHECK(subtotal>=0), tax NUMERIC(16,2) NOT NULL CHECK(tax>=0), total NUMERIC(16,2) NOT NULL CHECK(total>0), paid NUMERIC(16,2) NOT NULL DEFAULT 0 CHECK(paid>=0 AND paid<=total),
 issue_date DATE NOT NULL, due_date DATE NOT NULL CHECK(due_date>=issue_date), notes TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL DEFAULT 'draft' CHECK(state IN ('draft','issued','sent')), version INTEGER NOT NULL DEFAULT 1,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(organization_id,id), UNIQUE(organization_id,quote_id), UNIQUE(organization_id,number),
 FOREIGN KEY(organization_id,quote_id) REFERENCES quotes(organization_id,id), FOREIGN KEY(organization_id,customer_id) REFERENCES customers(organization_id,id),
 FOREIGN KEY(organization_id,project_id) REFERENCES projects(organization_id,id), FOREIGN KEY(organization_id,creator_id) REFERENCES users(organization_id,id)
);
CREATE TABLE invoice_items (
 organization_id UUID NOT NULL, invoice_id UUID NOT NULL, position INTEGER NOT NULL,
 name TEXT NOT NULL, description TEXT NOT NULL, unit TEXT NOT NULL, quantity NUMERIC(10,3) NOT NULL, unit_price NUMERIC(14,2) NOT NULL,
 discount_percent NUMERIC(5,2) NOT NULL, subtotal NUMERIC(16,2) NOT NULL, discount_amount NUMERIC(16,2) NOT NULL, total NUMERIC(16,2) NOT NULL,
 PRIMARY KEY(organization_id,invoice_id,position), FOREIGN KEY(organization_id,invoice_id) REFERENCES invoices(organization_id,id)
);
CREATE TABLE payments (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL, invoice_id UUID NOT NULL, creator_id UUID NOT NULL,
 amount NUMERIC(16,2) NOT NULL CHECK(amount>0), payment_date DATE NOT NULL, reference TEXT NOT NULL, method TEXT NOT NULL CHECK(method IN ('bank_transfer','cash','card','other')), notes TEXT NOT NULL,
 idempotency_key UUID NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), UNIQUE(organization_id,invoice_id,idempotency_key),
 FOREIGN KEY(organization_id,invoice_id) REFERENCES invoices(organization_id,id), FOREIGN KEY(organization_id,creator_id) REFERENCES users(organization_id,id)
);
CREATE INDEX invoices_org_created ON invoices(organization_id,created_at DESC,id);
CREATE INDEX invoices_org_customer ON invoices(organization_id,customer_id);
CREATE INDEX invoices_org_project ON invoices(organization_id,project_id);
CREATE INDEX invoices_org_due ON invoices(organization_id,due_date) WHERE state<>'draft' AND paid<total;
CREATE INDEX payments_org_invoice ON payments(organization_id,invoice_id,created_at);
