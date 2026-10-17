CREATE TABLE document_invoice_captures (
 id UUID NOT NULL DEFAULT gen_random_uuid(), organization_id UUID NOT NULL, document_id UUID NOT NULL,
 status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','recorded')),
 version INTEGER NOT NULL DEFAULT 1 CHECK(version>0),
 source_ocr_version INTEGER NOT NULL, source_sha256 TEXT NOT NULL,
 fields JSONB NOT NULL CHECK(jsonb_typeof(fields)='object'),
 suggestions JSONB NOT NULL CHECK(jsonb_typeof(suggestions)='object'),
 updated_by UUID NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 recorded_by UUID, recorded_at TIMESTAMPTZ,
 PRIMARY KEY(organization_id,document_id), UNIQUE(id),
 FOREIGN KEY(organization_id,document_id) REFERENCES documents(organization_id,id),
 FOREIGN KEY(organization_id,document_id) REFERENCES document_ocr(organization_id,document_id),
 FOREIGN KEY(organization_id,updated_by) REFERENCES users(organization_id,id),
 FOREIGN KEY(organization_id,recorded_by) REFERENCES users(organization_id,id),
 CHECK((status='recorded')=(recorded_at IS NOT NULL AND recorded_by IS NOT NULL))
);
