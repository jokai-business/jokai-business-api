-- Preserve every preparer in a revision, even after another user edits the same contribution.
CREATE TABLE scope_preparers (
 organization_id UUID NOT NULL, scope_id UUID NOT NULL, revision INTEGER NOT NULL, user_id UUID NOT NULL,
 PRIMARY KEY(organization_id,scope_id,revision,user_id),
 FOREIGN KEY(organization_id,scope_id) REFERENCES service_scopes(organization_id,id),
 FOREIGN KEY(organization_id,user_id) REFERENCES users(organization_id,id)
);
INSERT INTO scope_preparers(organization_id,scope_id,revision,user_id)
 SELECT a.organization_id,a.resource_id,(a.after_value->>'revision')::integer,a.actor_id
 FROM audit_logs a JOIN service_scopes s ON s.organization_id=a.organization_id AND s.id=a.resource_id
 WHERE a.action IN ('scope.created','scope.updated','scope.contribution_added','scope.contribution_updated','scope.contribution_removed','scope.submit','scope.revise')
 AND a.after_value->>'revision' IS NOT NULL
 ON CONFLICT DO NOTHING;
