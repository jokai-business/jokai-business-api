package businessai

import (
	"ai-business-api/internal/ai"
	"ai-business-api/internal/user"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"

	"strings"
	"time"
)

func (s *Service) processQuestion(ctx context.Context) (bool, error) {
	_, e := s.DB.Exec(ctx, `DELETE FROM business_questions WHERE conversation_started_at<=now()-interval '30 days'`)
	if e != nil {
		return false, e
	}
	_, e = s.DB.Exec(ctx, `UPDATE business_questions SET status='failed',version=version+1,lease_until=NULL,completed_at=now() WHERE (status='processing' AND lease_until<now()) OR (status='queued' AND requested_at<now()-interval '10 minutes')`)
	if e != nil {
		return false, e
	}
	if s.LLM == nil {
		return false, nil
	}
	var j job
	var question string
	var history []ai.QuestionTurn
	e = s.DB.QueryRow(ctx, `UPDATE business_questions SET status='processing',version=version+1,lease_until=now()+interval '90 seconds' WHERE job_id IN(SELECT job_id FROM business_questions WHERE status='queued' ORDER BY requested_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING organization_id,user_id,kind,source_id,job_id,source_hash,provider,model,prompt_version,question,history`).Scan(&j.org, &j.uid, &j.kind, &j.id, &j.jobID, &j.hash, &j.provider, &j.model, &j.prompt, &question, &history)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	a := user.Context{User: user.User{ID: j.uid}, Organization: user.Organization{ID: j.org}}
	input, failure := s.snapshot(ctx, a, j)
	text := ""
	if failure == nil {
		req, prompt, err := ai.ConversationQuestion(j.kind, input, question, history)
		failure = err
		if failure == nil && (j.provider != s.Provider || j.model != s.Model || j.prompt != prompt) {
			failure = ErrChanged
		}
		if failure == nil {
			callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			text, failure = s.LLM.Generate(callCtx, req)
			cancel()
		}
	}
	text = strings.TrimSpace(text)
	if failure == nil && !ai.ValidText(text) {
		failure = ErrChanged
	}
	// Recheck current records and permissions after the external call, in the same
	// repeatable-read snapshot used to publish. Later changes are hidden on reads.
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return true, e
	}
	defer tx.Rollback(ctx)
	v, err := source(ctx, tx, a, j.kind, j.id)
	if err != nil {
		failure = err
	} else {
		_, h, err := fingerprint(v)
		if err != nil || h != j.hash {
			failure = ErrChanged
		}
	}
	status, action := "ready", "ai.question_completed"
	if failure != nil {
		status = "failed"
		text = ""
		action = "ai.question_failed"
	}
	tag, e := tx.Exec(ctx, `UPDATE business_questions SET status=$2,output=$3,version=version+1,completed_at=now(),lease_until=NULL WHERE job_id=$1 AND status='processing'`, j.jobID, status, text)
	if e != nil {
		return true, e
	}
	if tag.RowsAffected() > 0 {
		if e = audit(ctx, tx, a, j.id, action, "question-worker"); e != nil {
			return true, e
		}
	}
	return true, tx.Commit(ctx)
}
