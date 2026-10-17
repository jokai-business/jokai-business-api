ALTER TABLE notification_email_deliveries DROP CONSTRAINT notification_email_deliveries_attempts_check;
ALTER TABLE notification_email_deliveries ADD CONSTRAINT notification_email_attempts_check CHECK(attempts BETWEEN 0 AND 6);
ALTER TABLE notification_email_deliveries ADD COLUMN manual_retries INTEGER NOT NULL DEFAULT 0 CHECK(manual_retries BETWEEN 0 AND 1);
CREATE TABLE notification_email_tests (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL REFERENCES organizations(id),
 user_id UUID NOT NULL, recipient TEXT NOT NULL, idempotency_key UUID NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sent','failed','cancelled')),
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 1),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), sent_at TIMESTAMPTZ,
 UNIQUE(organization_id,idempotency_key), FOREIGN KEY(organization_id,user_id) REFERENCES users(organization_id,id)
);
CREATE INDEX notification_email_tests_recent ON notification_email_tests(organization_id,created_at DESC);
CREATE INDEX notification_email_tests_pending ON notification_email_tests(created_at) WHERE status='pending';
