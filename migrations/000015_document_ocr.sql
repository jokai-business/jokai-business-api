CREATE TABLE document_ocr (
 organization_id UUID NOT NULL, document_id UUID NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('queued','processing','ready','failed','approved','rejected')),
 requested_by UUID NOT NULL, requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 3),
 version INTEGER NOT NULL DEFAULT 1, lease_until TIMESTAMPTZ,
 source_sha256 TEXT NOT NULL, engine TEXT NOT NULL DEFAULT 'PaddleOCR',
 raw_text TEXT NOT NULL DEFAULT '' CHECK(octet_length(raw_text)<=8000),
 reviewed_text TEXT NOT NULL DEFAULT '' CHECK(octet_length(reviewed_text)<=8000),
 pages INTEGER NOT NULL DEFAULT 0 CHECK(pages BETWEEN 0 AND 10),
 failure TEXT NOT NULL DEFAULT '', completed_at TIMESTAMPTZ,
 reviewed_by UUID, reviewed_at TIMESTAMPTZ, review_note TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(organization_id,document_id),
 FOREIGN KEY(organization_id,document_id) REFERENCES documents(organization_id,id),
 FOREIGN KEY(organization_id,requested_by) REFERENCES users(organization_id,id),
 FOREIGN KEY(organization_id,reviewed_by) REFERENCES users(organization_id,id)
);
CREATE INDEX document_ocr_work ON document_ocr(requested_at) WHERE status IN ('queued','processing');
