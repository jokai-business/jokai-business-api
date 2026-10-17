package gmail

import (
	"ai-business-api/internal/ai"
	"ai-business-api/internal/user"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

var ErrAIDisabled = errors.New("AI is not configured")
var ErrAIConflict = errors.New("AI source or result changed")
var ErrAINotFound = errors.New("private email not found")
var ErrAIRate = errors.New("AI request limit")

type AIStatus struct {
	Available bool   `json:"available"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
}
type Assistance struct {
	Kind          string     `json:"kind"`
	Status        string     `json:"status"`
	Version       int        `json:"version"`
	Stale         bool       `json:"stale"`
	Provider      string     `json:"provider"`
	Model         string     `json:"model"`
	PromptVersion string     `json:"promptVersion"`
	Output        string     `json:"output"`
	EditedText    string     `json:"editedText"`
	ReviewedAt    *time.Time `json:"reviewedAt"`
	RequestedAt   time.Time  `json:"requestedAt"`
	CompletedAt   *time.Time `json:"completedAt"`
}
type AssistanceView struct {
	SourceHash string       `json:"sourceHash"`
	Items      []Assistance `json:"items"`
}
type AssistanceRequest struct {
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Kind       string `json:"kind"`
	Version    int    `json:"version"`
	SourceHash string `json:"sourceHash"`
	Confirmed  bool   `json:"confirmed"`
}
type DraftReview struct {
	Version   int    `json:"version"`
	Text      string `json:"text"`
	Confirmed bool   `json:"confirmed"`
}
type emailSource struct {
	Subject, Sender, Body, Generation string
	Limited                           bool
}

func (s *Service) AIStatus() AIStatus { return AIStatus{s.LLM != nil, s.AIProvider, s.AIModel} }
func source(ctx context.Context, tx pgx.Tx, a user.Context, id string) (emailSource, error) {
	var v emailSource
	e := tx.QueryRow(ctx, `SELECT m.subject,m.sender,m.body_text,m.text_limited,c.sync_generation::text FROM gmail_messages m JOIN gmail_connections c USING(organization_id,user_id) WHERE m.organization_id=$1 AND m.user_id=$2 AND m.id=$3 AND m.received_at>=now()-interval '30 days'`, a.Organization.ID, a.User.ID, id).Scan(&v.Subject, &v.Sender, &v.Body, &v.Limited, &v.Generation)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrAINotFound
	}
	return v, e
}
func (v emailSource) hash() string { b, _ := json.Marshal(v); return hash(string(b)) }
func validMessageID(id string) bool {
	return len(id) > 0 && len(id) <= 256 && !strings.ContainsAny(id, "\x00\r\n/")
}
func (s *Service) Assistance(ctx context.Context, a user.Context, id string) (AssistanceView, error) {
	out := AssistanceView{Items: []Assistance{}}
	if !validMessageID(id) {
		return out, user.ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	src, e := source(ctx, tx, a, id)
	if e != nil {
		return out, e
	}
	out.SourceHash = src.hash()
	rows, e := tx.Query(ctx, `SELECT kind,status,version,source_hash,provider,model,prompt_version,output,edited_text,reviewed_at,requested_at,completed_at FROM gmail_assistance WHERE organization_id=$1 AND user_id=$2 AND message_id=$3 ORDER BY kind`, a.Organization.ID, a.User.ID, id)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v Assistance
		var h string
		if e = rows.Scan(&v.Kind, &v.Status, &v.Version, &h, &v.Provider, &v.Model, &v.PromptVersion, &v.Output, &v.EditedText, &v.ReviewedAt, &v.RequestedAt, &v.CompletedAt); e != nil {
			rows.Close()
			return out, e
		}
		v.Stale = h != out.SourceHash
		if v.Stale {
			v.Output = ""
			v.EditedText = ""
			v.ReviewedAt = nil
		}
		out.Items = append(out.Items, v)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Service) RequestAssistance(ctx context.Context, a user.Context, id string, in AssistanceRequest, rid string) error {
	if !validMessageID(id) || !in.Confirmed || in.Version < 0 || len(in.SourceHash) != 64 || (in.Kind != "summary" && in.Kind != "reply") {
		return user.ErrInvalid
	}
	if s.LLM == nil {
		return ErrAIDisabled
	}
	if in.Provider != s.AIProvider || in.Model != s.AIModel {
		return ErrAIConflict
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = lock(ctx, tx, a.Organization.ID, a.User.ID); e != nil {
		return e
	}
	src, e := source(ctx, tx, a, id)
	if e != nil {
		return e
	}
	if src.hash() != in.SourceHash {
		return ErrAIConflict
	}
	if strings.TrimSpace(src.Body) == "" {
		return user.ErrInvalid
	}
	var version int
	var status, previousHash string
	e = tx.QueryRow(ctx, `SELECT version,status,source_hash FROM gmail_assistance WHERE organization_id=$1 AND user_id=$2 AND message_id=$3 AND kind=$4`, a.Organization.ID, a.User.ID, id, in.Kind).Scan(&version, &status, &previousHash)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	// A matching ready result is reused; only failed or stale results may be replaced.
	if version != in.Version || status == "queued" || status == "processing" || (status == "ready" && previousHash == in.SourceHash) {
		return ErrAIConflict
	}
	var pending bool
	var recent int
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM gmail_assistance WHERE organization_id=$1 AND user_id=$2 AND status IN ('queued','processing'))`, a.Organization.ID, a.User.ID).Scan(&pending)
	if e != nil {
		return e
	}
	e = tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE organization_id=$1 AND actor_id=$2 AND action='gmail.ai_requested' AND created_at>now()-interval '1 hour'`, a.Organization.ID, a.User.ID).Scan(&recent)
	if e != nil {
		return e
	}
	if pending || recent >= 20 {
		return ErrAIRate
	}
	_, prompt, e := ai.Email(in.Kind, src.Subject, src.Sender, src.Body, src.Limited)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO gmail_assistance(organization_id,user_id,message_id,kind,status,source_hash,connection_generation,provider,model,prompt_version) VALUES($1,$2,$3,$4,'queued',$5,$6,$7,$8,$9) ON CONFLICT(organization_id,user_id,message_id,kind) DO UPDATE SET status='queued',version=gmail_assistance.version+1,source_hash=EXCLUDED.source_hash,connection_generation=EXCLUDED.connection_generation,provider=EXCLUDED.provider,model=EXCLUDED.model,prompt_version=EXCLUDED.prompt_version,output='',edited_text='',reviewed_at=NULL,completed_at=NULL,lease_until=NULL,requested_at=now()`, a.Organization.ID, a.User.ID, id, in.Kind, in.SourceHash, src.Generation, s.AIProvider, s.AIModel, prompt)
	if e != nil {
		return e
	}
	if e = audit(ctx, tx, a.Organization.ID, a.User.ID, "gmail.ai_requested", rid); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Service) ReviewDraft(ctx context.Context, a user.Context, id string, in DraftReview, rid string) error {
	in.Text = strings.TrimSpace(in.Text)
	if !validMessageID(id) || !in.Confirmed || in.Version < 1 || !ai.ValidText(in.Text) {
		return user.ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = lock(ctx, tx, a.Organization.ID, a.User.ID); e != nil {
		return e
	}
	src, e := source(ctx, tx, a, id)
	if e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, `UPDATE gmail_assistance SET edited_text=$5,reviewed_at=now(),version=version+1 WHERE organization_id=$1 AND user_id=$2 AND message_id=$3 AND kind='reply' AND version=$4 AND status='ready' AND source_hash=$6`, a.Organization.ID, a.User.ID, id, in.Version, in.Text, src.hash())
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrAIConflict
	}
	if e = audit(ctx, tx, a.Organization.ID, a.User.ID, "gmail.ai_draft_reviewed", rid); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
