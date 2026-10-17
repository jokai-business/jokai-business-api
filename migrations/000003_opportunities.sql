INSERT INTO permissions(key) VALUES ('pipeline.manage');
INSERT INTO role_permissions(role_key,permission_key) VALUES ('organization_admin','pipeline.manage'),('sales_manager','pipeline.manage');
CREATE TABLE pipeline_stages (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL REFERENCES organizations(id),
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 100), position INTEGER NOT NULL CHECK(position BETWEEN 0 AND 10000),
 is_closed BOOLEAN NOT NULL DEFAULT false, is_won BOOLEAN NOT NULL DEFAULT false CHECK(NOT is_won OR is_closed),
 version INTEGER NOT NULL DEFAULT 1, UNIQUE(organization_id,id)
);
CREATE INDEX stages_org_position ON pipeline_stages(organization_id,position,id);
CREATE FUNCTION seed_pipeline() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 INSERT INTO pipeline_stages(organization_id,name,position,is_closed,is_won) VALUES
 (NEW.id,'Lead',10,false,false),(NEW.id,'Qualified',20,false,false),(NEW.id,'Proposal',30,false,false),
 (NEW.id,'Negotiation',40,false,false),(NEW.id,'Won',50,true,true),(NEW.id,'Lost',60,true,false);
 RETURN NEW;
END $$;
CREATE TRIGGER organization_pipeline AFTER INSERT ON organizations FOR EACH ROW EXECUTE FUNCTION seed_pipeline();
INSERT INTO pipeline_stages(organization_id,name,position,is_closed,is_won)
 SELECT o.id,s.name,s.position,s.is_closed,s.is_won FROM organizations o CROSS JOIN
 (VALUES ('Lead',10,false,false),('Qualified',20,false,false),('Proposal',30,false,false),('Negotiation',40,false,false),('Won',50,true,true),('Lost',60,true,false)) AS s(name,position,is_closed,is_won);
CREATE TABLE opportunities (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL REFERENCES organizations(id),
 customer_id UUID NOT NULL, title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200), requirement TEXT NOT NULL DEFAULT '',
 stage_id UUID NOT NULL, expected_value NUMERIC(16,2) NOT NULL DEFAULT 0 CHECK(expected_value>=0), currency TEXT NOT NULL CHECK(currency ~ '^[A-Z]{3}$'),
 probability INTEGER NOT NULL CHECK(probability BETWEEN 0 AND 100), expected_close_date DATE,
 owner_id UUID NOT NULL, source TEXT NOT NULL CHECK(source IN ('Manual','Gmail','Excel','Document')), next_action TEXT NOT NULL DEFAULT '',
 version INTEGER NOT NULL DEFAULT 1, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(organization_id,id), FOREIGN KEY(organization_id,customer_id) REFERENCES customers(organization_id,id),
 FOREIGN KEY(organization_id,owner_id) REFERENCES users(organization_id,id), FOREIGN KEY(organization_id,stage_id) REFERENCES pipeline_stages(organization_id,id)
);
CREATE INDEX opportunities_org_created ON opportunities(organization_id,created_at DESC,id);
CREATE INDEX opportunities_org_customer ON opportunities(organization_id,customer_id);
CREATE INDEX opportunities_org_stage ON opportunities(organization_id,stage_id);
CREATE INDEX opportunities_org_owner ON opportunities(organization_id,owner_id);
CREATE TABLE opportunity_activities (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL, opportunity_id UUID NOT NULL, actor_id UUID NOT NULL,
 kind TEXT NOT NULL, body TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY(organization_id,opportunity_id) REFERENCES opportunities(organization_id,id),
 FOREIGN KEY(organization_id,actor_id) REFERENCES users(organization_id,id)
);
CREATE INDEX opportunity_activities_org_deal ON opportunity_activities(organization_id,opportunity_id,created_at DESC,id);
