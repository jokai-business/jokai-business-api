package document

import (
	"ai-business-api/internal/ai"
	"ai-business-api/internal/customer"
	"ai-business-api/internal/user"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"time"
)

type ExtractionResult struct {
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
type ExtractionView struct {
	Available  bool              `json:"available"`
	Provider   string            `json:"provider"`
	Model      string            `json:"model"`
	SourceHash string            `json:"sourceHash"`
	Context    ExtractionSource  `json:"context"`
	Result     *ExtractionResult `json:"result"`
}
type ExtractionInput struct {
	Version    int    `json:"version"`
	SourceHash string `json:"sourceHash"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Confirmed  bool   `json:"confirmed"`
}

func (s *Service) GetExtraction(ctx context.Context, a user.Context, id string) (ExtractionView, error) {
	out := ExtractionView{Available: s.LLM != nil, Provider: s.AIProvider, Model: s.AIModel}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	out.Context, e = extractionSource(ctx, tx, a, id)
	if e != nil {
		return out, e
	}
	_, out.SourceHash, e = extractionFingerprint(out.Context)
	if e != nil {
		return out, e
	}
	var r ExtractionResult
	var hash string
	e = tx.QueryRow(ctx, `SELECT status,version,source_hash,output,provider,model,prompt_version,requested_at,completed_at FROM document_ai_extractions WHERE organization_id=$1 AND user_id=$2 AND document_id=$3 AND requested_at>now()-interval '30 days'`, a.Organization.ID, a.User.ID, id).Scan(&r.Status, &r.Version, &hash, &r.Output, &r.Provider, &r.Model, &r.PromptVersion, &r.RequestedAt, &r.CompletedAt)
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
func (s *Service) RequestExtraction(ctx context.Context, a user.Context, id string, in ExtractionInput, rid string) error {
	if !customer.UUID(id) || !in.Confirmed || in.Version < 0 || len(in.SourceHash) != 64 {
		return user.ErrInvalid
	}
	if s.LLM == nil {
		return ErrExtractionDisabled
	}
	if in.Provider != s.AIProvider || in.Model != s.AIModel {
		return ErrExtractionChanged
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
	c, e := extractionSource(ctx, tx, a, id)
	if e != nil {
		return e
	}
	_, h, e := extractionFingerprint(c)
	if e != nil {
		return e
	}
	if h != in.SourceHash {
		return ErrExtractionChanged
	}
	// Remove expired personal rows before applying the optimistic version check.
	_, e = tx.Exec(ctx, `DELETE FROM document_ai_extractions WHERE organization_id=$1 AND user_id=$2 AND requested_at<=now()-interval '30 days'`, a.Organization.ID, a.User.ID)
	if e != nil {
		return e
	}
	var version int
	var status, oldHash string
	e = tx.QueryRow(ctx, `SELECT version,status,source_hash FROM document_ai_extractions WHERE organization_id=$1 AND user_id=$2 AND document_id=$3`, a.Organization.ID, a.User.ID, id).Scan(&version, &status, &oldHash)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if version != in.Version || status == "queued" || status == "processing" || (status == "ready" && oldHash == h) {
		return ErrExtractionChanged
	}
	var pending bool
	var recent int
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM document_ai_extractions WHERE organization_id=$1 AND user_id=$2 AND status IN ('queued','processing'))`, a.Organization.ID, a.User.ID).Scan(&pending)
	if e != nil {
		return e
	}
	e = tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE organization_id=$1 AND actor_id=$2 AND action='document.ai_requested' AND created_at>now()-interval '1 hour'`, a.Organization.ID, a.User.ID).Scan(&recent)
	if e != nil {
		return e
	}
	if pending || recent >= 20 {
		return ErrExtractionLimit
	}
	_, prompt, e := ai.InvoiceExtraction("")
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO document_ai_extractions(organization_id,user_id,document_id,status,source_hash,provider,model,prompt_version)VALUES($1,$2,$3,'queued',$4,$5,$6,$7) ON CONFLICT(organization_id,user_id,document_id) DO UPDATE SET status='queued',job_id=gen_random_uuid(),version=document_ai_extractions.version+1,source_hash=EXCLUDED.source_hash,provider=EXCLUDED.provider,model=EXCLUDED.model,prompt_version=EXCLUDED.prompt_version,output='',requested_at=now(),completed_at=NULL,lease_until=NULL`, a.Organization.ID, a.User.ID, id, h, s.AIProvider, s.AIModel, prompt)
	if e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) && pg.Code == "23505" {
			return ErrExtractionChanged
		}
		return e
	}
	if e = audit(ctx, tx, a, "document.ai_requested", id, rid, nil); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
