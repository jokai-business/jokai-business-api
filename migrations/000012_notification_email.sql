CREATE TABLE notification_preferences (
 organization_id UUID NOT NULL, user_id UUID NOT NULL,
 email_enabled BOOLEAN NOT NULL DEFAULT false, enabled_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(organization_id,user_id), FOREIGN KEY(organization_id,user_id) REFERENCES users(organization_id,id)
);
ALTER TABLE notifications ADD CONSTRAINT notifications_org_id_unique UNIQUE(organization_id,id);
CREATE TABLE notification_email_deliveries (
 organization_id UUID NOT NULL, notification_id UUID NOT NULL, user_id UUID NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sent','failed','cancelled')),
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 5),
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(), created_at TIMESTAMPTZ NOT NULL DEFAULT now(), sent_at TIMESTAMPTZ,
 PRIMARY KEY(organization_id,notification_id),
 FOREIGN KEY(organization_id,notification_id) REFERENCES notifications(organization_id,id),
 FOREIGN KEY(organization_id,user_id) REFERENCES users(organization_id,id)
);
CREATE INDEX notification_email_pending ON notification_email_deliveries(next_attempt_at) WHERE status='pending';
