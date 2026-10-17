-- Existing originals remain on the local volume; no file is moved by this migration.
ALTER TABLE documents ADD COLUMN storage_id TEXT NOT NULL DEFAULT 'local'
 CHECK(storage_id='local' OR storage_id ~ '^s3:[a-f0-9]{64}$');
