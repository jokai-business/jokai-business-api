CREATE TABLE reminders (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL, user_id UUID NOT NULL,
 title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200), notes TEXT NOT NULL DEFAULT '', due_at TIMESTAMPTZ NOT NULL,
 status TEXT NOT NULL DEFAULT 'scheduled' CHECK(status IN ('scheduled','delivered','cancelled')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), delivered_at TIMESTAMPTZ,
 UNIQUE(organization_id,id), FOREIGN KEY(organization_id,user_id) REFERENCES users(organization_id,id)
);
CREATE INDEX reminders_due ON reminders(organization_id,due_at) WHERE status='scheduled';
CREATE INDEX reminders_personal ON reminders(organization_id,user_id,created_at DESC,id);
CREATE TABLE notifications (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), organization_id UUID NOT NULL, user_id UUID NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('task_due','task_overdue','invoice_due','invoice_overdue','quote_review','scope_review','reminder')),
 source_id UUID NOT NULL, dedup_key TEXT NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL DEFAULT '', href TEXT NOT NULL,
 required_permission TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT now(), read_at TIMESTAMPTZ, resolved_at TIMESTAMPTZ,
 UNIQUE(organization_id,user_id,dedup_key), FOREIGN KEY(organization_id,user_id) REFERENCES users(organization_id,id)
);
CREATE INDEX notifications_personal ON notifications(organization_id,user_id,created_at DESC,id);
CREATE INDEX notifications_unread ON notifications(organization_id,user_id) WHERE read_at IS NULL AND resolved_at IS NULL;
