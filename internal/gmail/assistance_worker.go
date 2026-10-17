package gmail

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

type assistanceJob struct {
	org, uid, id, kind, hash, generation, provider, model, prompt string
	version                                                       int
	requestedAt                                                   time.Time
}

func (s *Service) RunAssistance(ctx context.Context, logger *slog.Logger) {
	for {
		worked, e := s.processAssistance(ctx)
		if ctx.Err() != nil {
			return
		}
		if e != nil {
			logger.Error("Email AI worker failed")
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
func (s *Service) processAssistance(ctx context.Context) (bool, error) {
	// Never auto-retry an uncertain provider call (including crashes or timeouts).
	_, e := s.DB.Exec(ctx, `UPDATE gmail_assistance SET status='failed',version=version+1,lease_until=NULL,completed_at=now() WHERE (status='processing' AND lease_until<now()) OR (status='queued' AND requested_at<now()-interval '10 minutes')`)
	if e != nil {
		return false, e
	}
	_, e = s.DB.Exec(ctx, `DELETE FROM gmail_assistance j WHERE requested_at<now()-interval '30 days' OR NOT EXISTS(SELECT 1 FROM gmail_messages m WHERE m.organization_id=j.organization_id AND m.user_id=j.user_id AND m.id=j.message_id AND m.received_at>=now()-interval '30 days')`)
	if e != nil {
		return false, e
	}
	if s.LLM == nil {
		return false, nil
	}
	var j assistanceJob
	e = s.DB.QueryRow(ctx, `UPDATE gmail_assistance SET status='processing',version=version+1,lease_until=now()+interval '90 seconds' WHERE (organization_id,user_id,message_id,kind) IN (SELECT organization_id,user_id,message_id,kind FROM gmail_assistance WHERE status='queued' ORDER BY requested_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING organization_id,user_id,message_id,kind,source_hash,connection_generation::text,provider,model,prompt_version,version,requested_at`).Scan(&j.org, &j.uid, &j.id, &j.kind, &j.hash, &j.generation, &j.provider, &j.model, &j.prompt, &j.version, &j.requestedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	a := user.Context{User: user.User{ID: j.uid}, Organization: user.Organization{ID: j.org}}
	src, e := s.assistanceSource(ctx, a, j)
	text := ""
	if e == nil {
		req, prompt, err := ai.Email(j.kind, src.Subject, src.Sender, src.Body, src.Limited)
		e = err
		if e == nil && (j.provider != s.AIProvider || j.model != s.AIModel || prompt != j.prompt) {
			e = ErrAIConflict
		}
		if e == nil {
			callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			text, e = s.LLM.Generate(callCtx, req)
			cancel()
		}
	}
	text = strings.TrimSpace(text)
	if e == nil && !ai.ValidText(text) {
		e = ErrAIConflict
	}
	return true, s.finishAssistance(ctx, a, j, text, e)
}
func (s *Service) assistanceSource(ctx context.Context, a user.Context, j assistanceJob) (emailSource, error) {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return emailSource{}, e
	}
	defer tx.Rollback(ctx)
	if e = lock(ctx, tx, j.org, j.uid); e != nil {
		return emailSource{}, e
	}
	src, e := source(ctx, tx, a, j.id)
	if e != nil {
		return src, e
	}
	if src.hash() != j.hash || src.Generation != j.generation {
		return src, ErrAIConflict
	}
	return src, tx.Commit(ctx)
}
func (s *Service) finishAssistance(ctx context.Context, a user.Context, j assistanceJob, text string, failure error) error {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// Same membership lock as disconnect/reconnect/sync, then recheck the exact source.
	if e = lock(ctx, tx, j.org, j.uid); e != nil {
		failure = e
	}
	src, sourceErr := source(ctx, tx, a, j.id)
	if sourceErr != nil || src.hash() != j.hash || src.Generation != j.generation {
		failure = ErrAIConflict
	}
	status := "ready"
	action := "gmail.ai_completed"
	if failure != nil {
		status = "failed"
		text = ""
		action = "gmail.ai_failed"
	}
	tag, e := tx.Exec(ctx, `UPDATE gmail_assistance SET status=$6,output=$7,version=version+1,lease_until=NULL,completed_at=now() WHERE organization_id=$1 AND user_id=$2 AND message_id=$3 AND kind=$4 AND version=$5 AND status='processing' AND requested_at=$8 AND source_hash=$9 AND connection_generation=$10`, j.org, j.uid, j.id, j.kind, j.version, status, text, j.requestedAt, j.hash, j.generation)
	if e != nil {
		return e
	}
	if tag.RowsAffected() > 0 {
		if e = audit(ctx, tx, j.org, j.uid, action, "ai-worker"); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
