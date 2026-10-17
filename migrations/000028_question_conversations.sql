ALTER TABLE business_questions ADD COLUMN history JSONB NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(history)='array' AND jsonb_array_length(history)<=4 AND octet_length(history::text)<=200000);
ALTER TABLE business_questions ADD COLUMN conversation_started_at TIMESTAMPTZ NOT NULL DEFAULT now();
UPDATE business_questions SET conversation_started_at=requested_at;
