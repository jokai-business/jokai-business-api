package gmail

import (
	"ai-business-api/internal/ai"
	"ai-business-api/internal/user"
	"ai-business-api/platform/googlemail"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

var ErrDisabled = errors.New("Gmail is not configured")
var ErrState = errors.New("Gmail connection request expired or changed")

type Provider interface {
	AuthorizationURL(string, string) string
	Exchange(context.Context, string, string) (googlemail.Grant, error)
	Revoke(context.Context, string) error
}
type Service struct {
	LLM                 ai.LLM
	AIProvider, AIModel string
	DB                  *pgxpool.Pool
	Provider            Provider
	Vault               *googlemail.Vault
}
type Status struct {
	Available   bool       `json:"available"`
	Connected   bool       `json:"connected"`
	Email       string     `json:"email"`
	ConnectedAt *time.Time `json:"connectedAt"`
}
type Start struct {
	AuthorizationURL string `json:"authorizationUrl"`
	BrowserSecret    string `json:"-"`
}
type Disconnected struct {
	Revoked bool `json:"revoked"`
}

func (s *Service) available() bool       { return s.Provider != nil && s.Vault != nil }
func owner(org, uid, kind string) string { return org + ":" + uid + ":" + kind }
func hash(v string) string               { b := sha256.Sum256([]byte(v)); return hex.EncodeToString(b[:]) }
func random() (string, error) {
	b := make([]byte, 32)
	_, e := rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b), e
}
func (s *Service) Status(ctx context.Context, a user.Context) (Status, error) {
	v := Status{Available: s.available()}
	e := s.DB.QueryRow(ctx, `SELECT email,connected_at FROM gmail_connections WHERE organization_id=$1 AND user_id=$2`, a.Organization.ID, a.User.ID).Scan(&v.Email, &v.ConnectedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, nil
	}
	v.Connected = e == nil
	return v, e
}
func lock(ctx context.Context, tx pgx.Tx, org, uid string) error {
	var active bool
	e := tx.QueryRow(ctx, `SELECT active FROM users WHERE organization_id=$1 AND id=$2 FOR UPDATE`, org, uid).Scan(&active)
	if e != nil {
		return e
	}
	if !active {
		return user.ErrForbidden
	}
	return nil
}
func audit(ctx context.Context, tx pgx.Tx, org, uid, action, rid string) error {
	_, e := tx.Exec(ctx, `INSERT INTO audit_logs(organization_id,actor_id,action,resource_id,request_id) VALUES($1,$2,$3,$2,$4)`, org, uid, action, rid)
	return e
}
func (s *Service) Begin(ctx context.Context, a user.Context, rid string) (Start, error) {
	if !s.available() {
		return Start{}, ErrDisabled
	}
	state, e := random()
	if e != nil {
		return Start{}, e
	}
	browser, e := random()
	if e != nil {
		return Start{}, e
	}
	verifier, e := random()
	if e != nil {
		return Start{}, e
	}
	sealed, e := s.Vault.Seal(verifier, owner(a.Organization.ID, a.User.ID, "state"))
	if e != nil {
		return Start{}, e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Start{}, e
	}
	defer tx.Rollback(ctx)
	if e = lock(ctx, tx, a.Organization.ID, a.User.ID); e != nil {
		return Start{}, e
	}
	// One pending consent per member. New starts invalidate older tabs.
	_, e = tx.Exec(ctx, `INSERT INTO gmail_oauth_states(organization_id,user_id,state_hash,browser_hash,verifier,expires_at) VALUES($1,$2,$3,$4,$5,now()+interval '10 minutes') ON CONFLICT(organization_id,user_id) DO UPDATE SET state_hash=EXCLUDED.state_hash,browser_hash=EXCLUDED.browser_hash,verifier=EXCLUDED.verifier,expires_at=EXCLUDED.expires_at`, a.Organization.ID, a.User.ID, hash(state), hash(browser), sealed)
	if e != nil {
		return Start{}, e
	}
	if e = audit(ctx, tx, a.Organization.ID, a.User.ID, "gmail.connection_started", rid); e != nil {
		return Start{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Start{}, e
	}
	return Start{s.Provider.AuthorizationURL(state, verifier), browser}, nil
}
func (s *Service) Complete(ctx context.Context, state, browser, code string, denied bool, rid string) error {
	if !s.available() {
		return ErrDisabled
	}
	if len(state) != 43 || len(browser) != 43 || (!denied && (code == "" || len(code) > 4096)) {
		return ErrState
	}
	var org, uid string
	e := s.DB.QueryRow(ctx, `SELECT organization_id,user_id FROM gmail_oauth_states WHERE state_hash=$1 AND browser_hash=$2 AND expires_at>now()`, hash(state), hash(browser)).Scan(&org, &uid)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrState
	}
	if e != nil {
		return e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = lock(ctx, tx, org, uid); e != nil {
		return e
	}
	var encrypted []byte
	e = tx.QueryRow(ctx, `DELETE FROM gmail_oauth_states WHERE organization_id=$1 AND user_id=$2 AND state_hash=$3 AND browser_hash=$4 AND expires_at>now() RETURNING verifier`, org, uid, hash(state), hash(browser)).Scan(&encrypted)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrState
	}
	if e != nil {
		return e
	}
	// Consume rejected/failed consent too. Credentials and provider errors never enter audit logs.
	fail := func(cause error) error {
		if e := audit(ctx, tx, org, uid, "gmail.connection_failed", rid); e != nil {
			return e
		}
		if e := tx.Commit(ctx); e != nil {
			return e
		}
		return cause
	}
	if denied {
		return fail(ErrState)
	}
	verifier, e := s.Vault.Open(encrypted, owner(org, uid, "state"))
	if e != nil {
		return fail(googlemail.ErrProvider)
	}
	grant, e := s.Provider.Exchange(ctx, code, verifier)
	if e != nil {
		return fail(googlemail.ErrProvider)
	}
	encrypted, e = s.Vault.Seal(grant.RefreshToken, owner(org, uid, "token"))
	if e != nil {
		return fail(e)
	}
	_, e = tx.Exec(ctx, `INSERT INTO gmail_connections(organization_id,user_id,email,refresh_token) VALUES($1,$2,$3,$4) ON CONFLICT(organization_id,user_id) DO UPDATE SET email=EXCLUDED.email,refresh_token=EXCLUDED.refresh_token,connected_at=now(),sync_enabled=false,sync_status='idle',sync_generation=gen_random_uuid(),sync_lease_until=NULL,last_synced_at=NULL,next_sync_at=now(),sync_limited=false`, org, uid, grant.Email, encrypted)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM gmail_assistance WHERE organization_id=$1 AND user_id=$2`, org, uid); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM gmail_messages WHERE organization_id=$1 AND user_id=$2`, org, uid); e != nil {
		return e
	}
	if e = audit(ctx, tx, org, uid, "gmail.connected", rid); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Service) Disconnect(ctx context.Context, a user.Context, rid string) (Disconnected, error) {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Disconnected{}, e
	}
	defer tx.Rollback(ctx)
	if e = lock(ctx, tx, a.Organization.ID, a.User.ID); e != nil {
		return Disconnected{}, e
	}
	_, e = tx.Exec(ctx, `DELETE FROM gmail_oauth_states WHERE organization_id=$1 AND user_id=$2`, a.Organization.ID, a.User.ID)
	if e != nil {
		return Disconnected{}, e
	}
	var encrypted []byte
	e = tx.QueryRow(ctx, `DELETE FROM gmail_connections WHERE organization_id=$1 AND user_id=$2 RETURNING refresh_token`, a.Organization.ID, a.User.ID).Scan(&encrypted)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return Disconnected{}, e
	}
	result := Disconnected{Revoked: len(encrypted) == 0}
	// Serialize revocation with reconnect so an old grant cannot revoke a newly saved connection.
	if len(encrypted) > 0 && s.available() {
		if token, err := s.Vault.Open(encrypted, owner(a.Organization.ID, a.User.ID, "token")); err == nil {
			result.Revoked = s.Provider.Revoke(ctx, token) == nil
		}
	}
	if e = audit(ctx, tx, a.Organization.ID, a.User.ID, "gmail.disconnected", rid); e != nil {
		return Disconnected{}, e
	}
	return result, tx.Commit(ctx)
}
