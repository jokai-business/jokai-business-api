package businessai

import (
	"ai-business-api/internal/ai"
	"ai-business-api/internal/user"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"time"
)

type QuestionResult struct {
	Result
	Question              string            `json:"question"`
	History               []ai.QuestionTurn `json:"history"`
	ConversationStartedAt time.Time         `json:"conversationStartedAt"`
}
type QuestionView struct {
	Available  bool            `json:"available"`
	Provider   string          `json:"provider"`
	Model      string          `json:"model"`
	SourceHash string          `json:"sourceHash"`
	Context    Context         `json:"context"`
	Result     *QuestionResult `json:"result"`
}
type QuestionInput struct {
	Input
	Question string `json:"question"`
	FollowUp bool   `json:"followUp"`
}

func (s *Service) GetQuestion(ctx context.Context, a user.Context, kind, id string) (QuestionView, error) {
	out := QuestionView{Available: s.LLM != nil, Provider: s.Provider, Model: s.Model}
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
	var r QuestionResult
	var hash string
	e = tx.QueryRow(ctx, `SELECT status,version,source_hash,output,provider,model,prompt_version,requested_at,completed_at,question,history,conversation_started_at FROM business_questions WHERE organization_id=$1 AND user_id=$2 AND kind=$3 AND source_id=$4 AND conversation_started_at>now()-interval '30 days'`, a.Organization.ID, a.User.ID, kind, id).Scan(&r.Status, &r.Version, &hash, &r.Output, &r.Provider, &r.Model, &r.PromptVersion, &r.RequestedAt, &r.CompletedAt, &r.Question, &r.History, &r.ConversationStartedAt)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	if e == nil {
		r.Stale = hash != out.SourceHash
		if r.Stale {
			r.Output = ""
			r.Question = ""
			r.History = []ai.QuestionTurn{}
		}
		out.Result = &r
	}
	return out, tx.Commit(ctx)
}
func (s *Service) RequestQuestion(ctx context.Context, a user.Context, kind, id string, in QuestionInput, rid string) error {
	in.Question = strings.TrimSpace(in.Question)
	if len(in.Question) > 1500 || !ai.ValidText(in.Question) {
		return user.ErrInvalid
	}
	if !validQuestion(kind, id) || !in.Confirmed || in.Version < 0 || len(in.SourceHash) != 64 {
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
	sourceJSON, h, e := fingerprint(c)
	if e != nil {
		return e
	}
	if h != in.SourceHash {
		return ErrChanged
	}
	// Remove expired personal rows before applying the optimistic version check.
	_, e = tx.Exec(ctx, `DELETE FROM business_questions WHERE organization_id=$1 AND user_id=$2 AND conversation_started_at<=now()-interval '30 days'`, a.Organization.ID, a.User.ID)
	if e != nil {
		return e
	}
	var version int
	var status, previousHash, previousProvider, previousModel, previousQuestion, previousAnswer string
	history := []ai.QuestionTurn{}
	started := time.Now().UTC()
	e = tx.QueryRow(ctx, `SELECT version,status,source_hash,provider,model,question,output,history,conversation_started_at FROM business_questions WHERE organization_id=$1 AND user_id=$2 AND kind=$3 AND source_id=$4`, a.Organization.ID, a.User.ID, kind, id).Scan(&version, &status, &previousHash, &previousProvider, &previousModel, &previousQuestion, &previousAnswer, &history, &started)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if version != in.Version || status == "queued" || status == "processing" {
		return ErrChanged
	}
	if in.FollowUp {
		if status != "ready" || previousHash != h || previousProvider != s.Provider || previousModel != s.Model || len(history) >= 4 {
			return ErrChanged
		}
		history = append(history, ai.QuestionTurn{Question: previousQuestion, Answer: previousAnswer})
	} else {
		history = []ai.QuestionTurn{}
		started = time.Now().UTC()
	}
	historyJSON, e := json.Marshal(history)
	if e != nil {
		return e
	}
	var pending bool
	var recent int
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM business_questions WHERE organization_id=$1 AND user_id=$2 AND status IN ('queued','processing'))`, a.Organization.ID, a.User.ID).Scan(&pending)
	if e != nil {
		return e
	}
	e = tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE organization_id=$1 AND actor_id=$2 AND action='ai.question_requested' AND created_at>now()-interval '1 hour'`, a.Organization.ID, a.User.ID).Scan(&recent)
	if e != nil {
		return e
	}
	if pending || recent >= 20 {
		return ErrLimit
	}
	request, prompt, e := ai.ConversationQuestion(kind, sourceJSON, in.Question, history)
	if e != nil {
		return e
	}
	if len(request.Input) > 40000 {
		return ErrContext
	}
	_, e = tx.Exec(ctx, `INSERT INTO business_questions(organization_id,user_id,kind,source_id,status,source_hash,provider,model,prompt_version,question,history,conversation_started_at)VALUES($1,$2,$3,$4,'queued',$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(organization_id,user_id,kind,source_id) DO UPDATE SET status='queued',job_id=gen_random_uuid(),version=business_questions.version+1,source_hash=EXCLUDED.source_hash,provider=EXCLUDED.provider,model=EXCLUDED.model,prompt_version=EXCLUDED.prompt_version,question=EXCLUDED.question,history=EXCLUDED.history,conversation_started_at=EXCLUDED.conversation_started_at,output='',requested_at=now(),completed_at=NULL,lease_until=NULL`, a.Organization.ID, a.User.ID, kind, id, h, s.Provider, s.Model, prompt, in.Question, historyJSON, started)
	if e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) && pg.Code == "23505" {
			return ErrChanged
		}
		return e
	}
	if e = audit(ctx, tx, a, id, "ai.question_requested", rid); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
