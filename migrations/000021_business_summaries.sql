CREATE TABLE business_summaries (
 organization_id UUID NOT NULL,user_id UUID NOT NULL,kind TEXT NOT NULL CHECK(kind IN ('customer','opportunity')),source_id UUID NOT NULL,
 job_id UUID NOT NULL UNIQUE DEFAULT gen_random_uuid(),version INTEGER NOT NULL DEFAULT 1,
 status TEXT NOT NULL CHECK(status IN ('queued','processing','ready','failed')),
 source_hash TEXT NOT NULL,provider TEXT NOT NULL,model TEXT NOT NULL,prompt_version TEXT NOT NULL,
 output TEXT NOT NULL DEFAULT '' CHECK(octet_length(output)<=8000),requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),completed_at TIMESTAMPTZ,lease_until TIMESTAMPTZ,
 PRIMARY KEY(organization_id,user_id,kind,source_id),FOREIGN KEY(organization_id,user_id) REFERENCES users(organization_id,id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX business_summary_pending ON business_summaries(organization_id,user_id) WHERE status IN ('queued','processing');
CREATE INDEX business_summary_queue ON business_summaries(requested_at) WHERE status='queued';
-- Customer summaries may contain personal email context. Remove derived results
-- together with mailbox disconnect/reconnect, including in-flight jobs.
CREATE FUNCTION clear_customer_mail_summaries() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 DELETE FROM business_summaries WHERE organization_id=OLD.organization_id AND user_id=OLD.user_id AND kind='customer';
 RETURN NULL;
END $$;
CREATE TRIGGER gmail_clear_customer_summaries AFTER DELETE OR UPDATE OF connected_at ON gmail_connections FOR EACH ROW EXECUTE FUNCTION clear_customer_mail_summaries();
