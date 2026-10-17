package businessai

import (
	"ai-business-api/internal/ai"
	"ai-business-api/internal/user"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Service struct {
	DB              *pgxpool.Pool
	LLM             ai.LLM
	Provider, Model string
}
type Result struct {
	Status        string     `json:"status"`
	Version       int        `json:"version"`
	Stale         bool       `json:"stale"`
	Output        string     `json:"output"`
	Provider      string     `json:"provider"`
	Model         string     `json:"model"`
	PromptVersion string     `json:"promptVersion"`
	RequestedAt   time.Time  `json:"requestedAt"`
	CompletedAt   *time.Time `json:"completedAt"`
}
type View struct {
	Available  bool    `json:"available"`
	Provider   string  `json:"provider"`
	Model      string  `json:"model"`
	SourceHash string  `json:"sourceHash"`
	Context    Context `json:"context"`
	Result     *Result `json:"result"`
}
type Input struct {
	Version    int    `json:"version"`
	SourceHash string `json:"sourceHash"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Confirmed  bool   `json:"confirmed"`
}

func (s *Service) Get(ctx context.Context, a user.Context, kind, id string) (View, error) {
	if !valid(kind, id) {
		return View{}, user.ErrInvalid
	}
	out := View{Available: s.LLM != nil, Provider: s.Provider, Model: s.Model}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	out.Context, e = source(ctx, tx, a, kind, id)
	if e != nil {
		return out, e
	}
	_, out.SourceHash, e = fingerprint(out.Context)
	if e != nil {
		return out, e
	}
	var r Result
	var hash string
	e = tx.QueryRow(ctx, `SELECT status,version,source_hash,output,provider,model,prompt_version,requested_at,completed_at FROM business_summaries WHERE organization_id=$1 AND user_id=$2 AND kind=$3 AND source_id=$4 AND requested_at>now()-interval '30 days'`, a.Organization.ID, a.User.ID, kind, id).Scan(&r.Status, &r.Version, &hash, &r.Output, &r.Provider, &r.Model, &r.PromptVersion, &r.RequestedAt, &r.CompletedAt)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	if e == nil {
		r.Stale = hash != out.SourceHash
		if r.Stale {
			r.Output = ""
		}
		out.Result = &r
	}
	return out, tx.Commit(ctx)
}
func audit(ctx context.Context, tx pgx.Tx, a user.Context, id, action, rid string) error {
	_, e := tx.Exec(ctx, `INSERT INTO audit_logs(organization_id,actor_id,resource_id,action,request_id)VALUES($1,$2,$3,$4,$5)`, a.Organization.ID, a.User.ID, id, action, rid)
	return e
}
func (s *Service) Request(ctx context.Context, a user.Context, kind, id string, in Input, rid string) error {
	if !valid(kind, id) || !in.Confirmed || in.Version < 0 || len(in.SourceHash) != 64 {
		return user.ErrInvalid
	}
	if s.LLM == nil {
		return ErrDisabled
	}
	if in.Provider != s.Provider || in.Model != s.Model {
		return ErrChanged
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var active bool
	e = tx.QueryRow(ctx, `SELECT active FROM users WHERE organization_id=$1 AND id=$2 FOR UPDATE`, a.Organization.ID, a.User.ID).Scan(&active)
	if e != nil || !active {
		return user.ErrForbidden
	}
	c, e := source(ctx, tx, a, kind, id)
	if e != nil {
		return e
	}
	_, h, e := fingerprint(c)
	if e != nil {
		return e
	}
	if h != in.SourceHash {
		return ErrChanged
	}
	// Remove expired personal rows before applying the optimistic version check.
	_, e = tx.Exec(ctx, `DELETE FROM business_summaries WHERE organization_id=$1 AND user_id=$2 AND requested_at<=now()-interval '30 days'`, a.Organization.ID, a.User.ID)
	if e != nil {
		return e
	}
	var version int
	var status, oldHash string
	e = tx.QueryRow(ctx, `SELECT version,status,source_hash FROM business_summaries WHERE organization_id=$1 AND user_id=$2 AND kind=$3 AND source_id=$4`, a.Organization.ID, a.User.ID, kind, id).Scan(&version, &status, &oldHash)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if version != in.Version || status == "queued" || status == "processing" || (status == "ready" && oldHash == h) {
		return ErrChanged
	}
	var pending bool
	var recent int
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM business_summaries WHERE organization_id=$1 AND user_id=$2 AND status IN ('queued','processing'))`, a.Organization.ID, a.User.ID).Scan(&pending)
	if e != nil {
		return e
	}
	e = tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE organization_id=$1 AND actor_id=$2 AND action='ai.summary_requested' AND created_at>now()-interval '1 hour'`, a.Organization.ID, a.User.ID).Scan(&recent)
	if e != nil {
		return e
	}
	if pending || recent >= 20 {
		return ErrLimit
	}
	_, prompt, e := ai.Business("")
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO business_summaries(organization_id,user_id,kind,source_id,status,source_hash,provider,model,prompt_version)VALUES($1,$2,$3,$4,'queued',$5,$6,$7,$8) ON CONFLICT(organization_id,user_id,kind,source_id) DO UPDATE SET status='queued',job_id=gen_random_uuid(),version=business_summaries.version+1,source_hash=EXCLUDED.source_hash,provider=EXCLUDED.provider,model=EXCLUDED.model,prompt_version=EXCLUDED.prompt_version,output='',requested_at=now(),completed_at=NULL,lease_until=NULL`, a.Organization.ID, a.User.ID, kind, id, h, s.Provider, s.Model, prompt)
	if e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) && pg.Code == "23505" {
			return ErrChanged
		}
		return e
	}
	if e = audit(ctx, tx, a, id, "ai.summary_requested", rid); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
