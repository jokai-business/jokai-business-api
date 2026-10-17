ALTER TABLE document_ocr ADD COLUMN language TEXT NOT NULL DEFAULT 'en' CHECK(language IN ('en','km+en'));
