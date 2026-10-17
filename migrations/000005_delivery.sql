CREATE TABLE projects (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL REFERENCES organizations(id),
 customer_id UUID NOT NULL, opportunity_id UUID NOT NULL, quote_id UUID, owner_id UUID NOT NULL,
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200), notes TEXT NOT NULL DEFAULT '',
 start_date DATE, target_date DATE, status TEXT NOT NULL DEFAULT 'not_started' CHECK(status IN ('not_started','in_progress','blocked','completed')),
 version INTEGER NOT NULL DEFAULT 1, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK(target_date IS NULL OR start_date IS NULL OR target_date>=start_date),
 UNIQUE(organization_id,id), UNIQUE(organization_id,opportunity_id),
 FOREIGN KEY(organization_id,customer_id) REFERENCES customers(organization_id,id),
 FOREIGN KEY(organization_id,opportunity_id) REFERENCES opportunities(organization_id,id),
 FOREIGN KEY(organization_id,quote_id) REFERENCES quotes(organization_id,id),
 FOREIGN KEY(organization_id,owner_id) REFERENCES users(organization_id,id)
);
CREATE INDEX projects_org_created ON projects(organization_id,created_at DESC,id);
CREATE INDEX projects_org_customer ON projects(organization_id,customer_id);
CREATE INDEX projects_org_status ON projects(organization_id,status);
CREATE TABLE tasks (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL, project_id UUID NOT NULL,
 title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200), assignee_id UUID, due_date DATE,
 status TEXT NOT NULL DEFAULT 'todo' CHECK(status IN ('todo','in_progress','done')),
 priority TEXT NOT NULL DEFAULT 'normal' CHECK(priority IN ('low','normal','high','urgent')),
 notes TEXT NOT NULL DEFAULT '', version INTEGER NOT NULL DEFAULT 1,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(organization_id,id), FOREIGN KEY(organization_id,project_id) REFERENCES projects(organization_id,id),
 FOREIGN KEY(organization_id,assignee_id) REFERENCES users(organization_id,id)
);
CREATE INDEX tasks_org_project ON tasks(organization_id,project_id,created_at,id);
CREATE INDEX tasks_org_project_status ON tasks(organization_id,project_id,status);
CREATE INDEX tasks_org_assignee_due ON tasks(organization_id,assignee_id,due_date);
