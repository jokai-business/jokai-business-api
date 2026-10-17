CREATE TABLE organizations (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200), created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE users (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL REFERENCES organizations(id),
 subject TEXT NOT NULL UNIQUE, email TEXT NOT NULL, name TEXT NOT NULL, active BOOLEAN NOT NULL DEFAULT true,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), UNIQUE(organization_id,id), UNIQUE(organization_id,email)
);
CREATE TABLE roles (key TEXT PRIMARY KEY, name TEXT NOT NULL);
CREATE TABLE permissions (key TEXT PRIMARY KEY);
CREATE TABLE role_permissions (role_key TEXT NOT NULL REFERENCES roles(key), permission_key TEXT NOT NULL REFERENCES permissions(key), PRIMARY KEY(role_key,permission_key));
CREATE TABLE user_roles (
 organization_id UUID NOT NULL, user_id UUID NOT NULL, role_key TEXT NOT NULL REFERENCES roles(key),
 PRIMARY KEY(organization_id,user_id,role_key), FOREIGN KEY(organization_id,user_id) REFERENCES users(organization_id,id)
);
CREATE TABLE audit_logs (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL REFERENCES organizations(id),
 actor_id UUID NOT NULL, action TEXT NOT NULL, resource_id UUID NOT NULL,
 request_id TEXT NOT NULL, session_id TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 before_value JSONB, after_value JSONB,
 FOREIGN KEY(organization_id,actor_id) REFERENCES users(organization_id,id)
);
CREATE UNIQUE INDEX audit_login_session ON audit_logs(organization_id,actor_id,session_id) WHERE action='login';
CREATE INDEX users_org_created ON users(organization_id,created_at,id);
CREATE INDEX audit_org_created ON audit_logs(organization_id,created_at DESC,id);
INSERT INTO roles VALUES
 ('organization_admin','Organization Admin'),('sales_manager','Sales Manager'),('sales_user','Sales User'),
 ('delivery_user','Delivery User'),('finance_user','Finance User'),('executive','Executive');
INSERT INTO permissions(key) VALUES
 ('customer.read'),('customer.create'),('customer.update'),('opportunity.read'),('opportunity.create'),('opportunity.update'),
 ('quote.read'),('quote.create'),('quote.approve'),('project.read'),('project.update'),('invoice.read'),('invoice.create'),('invoice.update'),
 ('payment.read'),('payment.create'),('document.read'),('document.upload'),('ai.use'),('user.manage'),('audit.read');
INSERT INTO role_permissions SELECT 'organization_admin',key FROM permissions;
INSERT INTO role_permissions SELECT 'executive',key FROM permissions WHERE key LIKE '%.read' OR key='ai.use';
INSERT INTO role_permissions SELECT 'sales_manager',key FROM permissions WHERE key LIKE 'customer.%' OR key LIKE 'opportunity.%' OR key LIKE 'quote.%' OR key IN ('document.read','document.upload','ai.use');
INSERT INTO role_permissions SELECT 'sales_user',key FROM permissions WHERE key LIKE 'customer.%' OR key LIKE 'opportunity.%' OR key IN ('quote.read','quote.create','document.read','document.upload','ai.use');
INSERT INTO role_permissions SELECT 'delivery_user',key FROM permissions WHERE key IN ('customer.read','project.read','project.update','document.read','document.upload','ai.use');
INSERT INTO role_permissions SELECT 'finance_user',key FROM permissions WHERE key LIKE 'invoice.%' OR key LIKE 'payment.%' OR key IN ('customer.read','project.read','quote.read','document.read','document.upload','ai.use');
