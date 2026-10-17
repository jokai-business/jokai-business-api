ALTER TABLE business_questions DROP CONSTRAINT business_questions_kind_check;
ALTER TABLE business_questions ADD CONSTRAINT business_questions_kind_check CHECK(kind IN ('customer','opportunity','organization'));
ALTER TABLE business_questions ADD CONSTRAINT business_questions_organization_source CHECK(kind<>'organization' OR source_id=organization_id);
