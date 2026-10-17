ALTER TABLE gmail_connections
 ADD COLUMN sync_enabled BOOLEAN NOT NULL DEFAULT false,
 ADD COLUMN sync_status TEXT NOT NULL DEFAULT 'idle' CHECK(sync_status IN ('idle','syncing','failed')),
 ADD COLUMN sync_generation UUID NOT NULL DEFAULT gen_random_uuid(),
 ADD COLUMN sync_lease_until TIMESTAMPTZ,
 ADD COLUMN next_sync_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 ADD COLUMN last_synced_at TIMESTAMPTZ,
 ADD COLUMN sync_limited BOOLEAN NOT NULL DEFAULT false;
CREATE TABLE gmail_messages (
 organization_id UUID NOT NULL,user_id UUID NOT NULL,id TEXT NOT NULL,
 subject TEXT NOT NULL,sender TEXT NOT NULL,sender_email TEXT NOT NULL,snippet TEXT NOT NULL,
 body_text TEXT NOT NULL,unread BOOLEAN NOT NULL,received_at TIMESTAMPTZ NOT NULL,text_limited BOOLEAN NOT NULL,
 PRIMARY KEY(organization_id,user_id,id),
 FOREIGN KEY(organization_id,user_id) REFERENCES gmail_connections(organization_id,user_id) ON DELETE CASCADE
);
CREATE INDEX gmail_messages_recent ON gmail_messages(organization_id,user_id,received_at DESC,id);
CREATE INDEX gmail_sync_due ON gmail_connections(next_sync_at) WHERE sync_enabled;
