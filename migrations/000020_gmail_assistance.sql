CREATE TABLE gmail_assistance (
 organization_id UUID NOT NULL,user_id UUID NOT NULL,message_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('summary','reply')),
 status TEXT NOT NULL CHECK(status IN ('queued','processing','ready','failed')),
 version INTEGER NOT NULL DEFAULT 1,
 source_hash TEXT NOT NULL,connection_generation UUID NOT NULL,
 provider TEXT NOT NULL,model TEXT NOT NULL,prompt_version TEXT NOT NULL,
 output TEXT NOT NULL DEFAULT '' CHECK(octet_length(output)<=8000),
 edited_text TEXT NOT NULL DEFAULT '' CHECK(octet_length(edited_text)<=8000),
 reviewed_at TIMESTAMPTZ,requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),completed_at TIMESTAMPTZ,lease_until TIMESTAMPTZ,
 PRIMARY KEY(organization_id,user_id,message_id,kind),
 FOREIGN KEY(organization_id,user_id) REFERENCES gmail_connections(organization_id,user_id) ON DELETE CASCADE
);
CREATE INDEX gmail_assistance_queue ON gmail_assistance(requested_at) WHERE status='queued';
CREATE UNIQUE INDEX gmail_assistance_one_pending ON gmail_assistance(organization_id,user_id) WHERE status IN ('queued','processing');
CREATE INDEX gmail_assistance_retention ON gmail_assistance(requested_at);
