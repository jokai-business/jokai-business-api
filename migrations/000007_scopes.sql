INSERT INTO permissions(key) VALUES ('scope.read'),('scope.create'),('scope.update'),('scope.approve');
INSERT INTO role_permissions SELECT 'organization_admin',key FROM permissions WHERE key LIKE 'scope.%';
INSERT INTO role_permissions SELECT 'sales_manager',key FROM permissions WHERE key LIKE 'scope.%';
INSERT INTO role_permissions VALUES ('sales_user','scope.read'),('sales_user','scope.create'),('sales_user','scope.update'),('delivery_user','scope.read'),('delivery_user','scope.update'),('executive','scope.read');
CREATE TABLE service_scopes (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL REFERENCES organizations(id), opportunity_id UUID NOT NULL, customer_id UUID NOT NULL, creator_id UUID NOT NULL, edited_by UUID NOT NULL, submitted_by UUID,
 title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200), objectives TEXT NOT NULL DEFAULT '', customer_name TEXT NOT NULL, opportunity_title TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','in_review','changes_requested','approved')), revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0), version INTEGER NOT NULL DEFAULT 1,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(organization_id,id), UNIQUE(organization_id,opportunity_id),
 FOREIGN KEY(organization_id,opportunity_id) REFERENCES opportunities(organization_id,id), FOREIGN KEY(organization_id,customer_id) REFERENCES customers(organization_id,id),
 FOREIGN KEY(organization_id,edited_by) REFERENCES users(organization_id,id), FOREIGN KEY(organization_id,creator_id) REFERENCES users(organization_id,id), FOREIGN KEY(organization_id,submitted_by) REFERENCES users(organization_id,id)
);
CREATE TABLE scope_contributions (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL, scope_id UUID NOT NULL, team TEXT NOT NULL, service TEXT NOT NULL,
 deliverables TEXT NOT NULL, assumptions TEXT NOT NULL DEFAULT '', exclusions TEXT NOT NULL DEFAULT '', timeline TEXT NOT NULL DEFAULT '', updated_by UUID NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY(organization_id,scope_id) REFERENCES service_scopes(organization_id,id), FOREIGN KEY(organization_id,updated_by) REFERENCES users(organization_id,id)
);
CREATE TABLE scope_revisions (
 organization_id UUID NOT NULL, scope_id UUID NOT NULL, revision INTEGER NOT NULL, snapshot JSONB NOT NULL, approved_by UUID NOT NULL, approved_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(organization_id,scope_id,revision), FOREIGN KEY(organization_id,scope_id) REFERENCES service_scopes(organization_id,id), FOREIGN KEY(organization_id,approved_by) REFERENCES users(organization_id,id)
);
CREATE TABLE scope_reviews (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL, scope_id UUID NOT NULL, revision INTEGER NOT NULL, action TEXT NOT NULL, note TEXT NOT NULL, actor_id UUID NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY(organization_id,scope_id) REFERENCES service_scopes(organization_id,id), FOREIGN KEY(organization_id,actor_id) REFERENCES users(organization_id,id)
);
CREATE INDEX scopes_org_created ON service_scopes(organization_id,created_at DESC,id);
CREATE INDEX scopes_org_customer ON service_scopes(organization_id,customer_id);
CREATE INDEX contributions_scope ON scope_contributions(organization_id,scope_id,created_at,id);
CREATE INDEX reviews_scope ON scope_reviews(organization_id,scope_id,created_at DESC,id);
