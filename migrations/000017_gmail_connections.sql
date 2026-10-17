CREATE TABLE gmail_connections (
 organization_id UUID NOT NULL, user_id UUID NOT NULL,
 email TEXT NOT NULL, refresh_token BYTEA NOT NULL, connected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(organization_id,user_id),
 FOREIGN KEY(organization_id,user_id) REFERENCES users(organization_id,id)
);
CREATE TABLE gmail_oauth_states (
 organization_id UUID NOT NULL, user_id UUID NOT NULL,
 state_hash TEXT NOT NULL UNIQUE, browser_hash TEXT NOT NULL, verifier BYTEA NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY(organization_id,user_id),
 FOREIGN KEY(organization_id,user_id) REFERENCES users(organization_id,id)
);
CREATE INDEX gmail_oauth_expiry ON gmail_oauth_states(expires_at);
