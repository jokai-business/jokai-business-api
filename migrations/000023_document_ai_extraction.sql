CREATE TABLE document_ai_extractions (
 organization_id UUID NOT NULL, user_id UUID NOT NULL, document_id UUID NOT NULL,
 job_id UUID NOT NULL UNIQUE DEFAULT gen_random_uuid(),version INTEGER NOT NULL DEFAULT 1,
 status TEXT NOT NULL CHECK(status IN ('queued','processing','ready','failed')),
 source_hash TEXT NOT NULL,provider TEXT NOT NULL,model TEXT NOT NULL,prompt_version TEXT NOT NULL,
 output TEXT NOT NULL DEFAULT '' CHECK(octet_length(output)<=8000),requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),completed_at TIMESTAMPTZ,lease_until TIMESTAMPTZ,
 PRIMARY KEY(organization_id,user_id,document_id),
 FOREIGN KEY(organization_id,user_id) REFERENCES users(organization_id,id) ON DELETE CASCADE,
 FOREIGN KEY(organization_id,document_id) REFERENCES documents(organization_id,id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX document_ai_pending ON document_ai_extractions(organization_id,user_id) WHERE status IN ('queued','processing');
CREATE INDEX document_ai_queue ON document_ai_extractions(requested_at) WHERE status='queued';
ALTER TABLE document_invoice_captures ADD COLUMN ai_provenance JSONB;
