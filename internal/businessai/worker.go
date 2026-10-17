package businessai

import (
	"ai-business-api/internal/ai"
	"ai-business-api/internal/user"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"log/slog"
	"strings"
	"time"
)

type job struct{ org, uid, kind, id, jobID, hash, provider, model, prompt string }

func (s *Service) Run(ctx context.Context, logger *slog.Logger) {
	for {
		worked, e := s.process(ctx)
		questionWorked, questionError := s.processQuestion(ctx)
		worked = worked || questionWorked
		if e == nil {
			e = questionError
		}
		if ctx.Err() != nil {
			return
		}
		if e != nil {
			logger.Error("Business summary worker failed")
		}
		if worked && e == nil {
			continue
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (s *Service) snapshot(ctx context.Context, a user.Context, j job) (string, error) {
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	v, e := source(ctx, tx, a, j.kind, j.id)
	if e != nil {
		return "", e
	}
	input, h, e := fingerprint(v)
	if e != nil {
		return "", e
	}
	if h != j.hash {
		return "", ErrChanged
	}
	return input, tx.Commit(ctx)
}
func (s *Service) process(ctx context.Context) (bool, error) {
	_, e := s.DB.Exec(ctx, `DELETE FROM business_summaries WHERE requested_at<=now()-interval '30 days'`)
	if e != nil {
		return false, e
	}
	_, e = s.DB.Exec(ctx, `UPDATE business_summaries SET status='failed',version=version+1,lease_until=NULL,completed_at=now() WHERE (status='processing' AND lease_until<now()) OR (status='queued' AND requested_at<now()-interval '10 minutes')`)
	if e != nil {
		return false, e
	}
	if s.LLM == nil {
		return false, nil
	}
	var j job
	e = s.DB.QueryRow(ctx, `UPDATE business_summaries SET status='processing',version=version+1,lease_until=now()+interval '90 seconds' WHERE job_id IN(SELECT job_id FROM business_summaries WHERE status='queued' ORDER BY requested_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING organization_id,user_id,kind,source_id,job_id,source_hash,provider,model,prompt_version`).Scan(&j.org, &j.uid, &j.kind, &j.id, &j.jobID, &j.hash, &j.provider, &j.model, &j.prompt)
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
		req, prompt, err := ai.Business(input)
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
	status, action := "ready", "ai.summary_completed"
	if failure != nil {
		status = "failed"
		text = ""
		action = "ai.summary_failed"
	}
	tag, e := tx.Exec(ctx, `UPDATE business_summaries SET status=$2,output=$3,version=version+1,completed_at=now(),lease_until=NULL WHERE job_id=$1 AND status='processing'`, j.jobID, status, text)
	if e != nil {
		return true, e
	}
	if tag.RowsAffected() > 0 {
		if e = audit(ctx, tx, a, j.id, action, "summary-worker"); e != nil {
			return true, e
		}
	}
	return true, tx.Commit(ctx)
}
