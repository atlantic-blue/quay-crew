package store

import (
	"context"
	"fmt"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/jackc/pgx/v5"
)

// The questions a reading of a plan could not settle, and the settling of one by a later reader.

// RecordJobQuestion writes down one thing the reading doing this job could not settle.
//
// The number is read and written in one statement rather than in a read before it, so two readings
// racing cannot take the same number. The job has to be running, which is the rule a step keeps: a
// question cannot be written against a job nobody is doing.
func (p *Postgres) RecordJobQuestion(ctx context.Context, id string, asking job.Question, event *job.Event) (*job.Job, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("record job question: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		insert into job_questions (job, seq, text, asked_by, asked_in, status)
		select $1, coalesce((select max(seq) from job_questions where job = $1), 0) + 1, $2, $3, $4, $5
		where exists (select 1 from jobs where id = $1 and phase = $6)`,
		id, asking.Text, asking.AskedBy, asking.AskedIn, job.QuestionOpen, job.PhaseRunning)
	if err != nil {
		return nil, fmt.Errorf("record job question: %w", err)
	}
	// Nothing was written, which is a job nobody is running: the row is what tells the two apart.
	if tag.RowsAffected() == 0 {
		found, err := p.GetJob(ctx, id)
		if err != nil {
			return nil, err
		}
		if found.Phase != job.PhaseRunning {
			return nil, job.ErrNotRunning
		}
		return found, nil
	}
	if err := appendJobEvent(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("record job question: %w", err)
	}
	return p.GetJob(ctx, id)
}

// SettleJobQuestion answers a row an earlier reader left open.
//
// The condition that the row is open lives in the statement, so two readers settling at once leave
// one answer rather than the later one overwriting the earlier.
func (p *Postgres) SettleJobQuestion(ctx context.Context, id string, seq int, answer, settledBy string, event *job.Event) (*job.Job, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("settle job question: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		update job_questions set status = $4, answer = $5, settled_by = $6, settled_at = now()
		where job = $1 and seq = $2 and status = $3
		  and exists (select 1 from jobs where id = $1 and phase = $7)`,
		id, seq, job.QuestionOpen, job.QuestionSettled, answer, settledBy, job.PhaseRunning)
	if err != nil {
		return nil, fmt.Errorf("settle job question: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, p.whyTheRowDidNotSettle(ctx, id, seq)
	}
	if err := appendJobEvent(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("settle job question: %w", err)
	}
	return p.GetJob(ctx, id)
}

// whyTheRowDidNotSettle names which of the three it was, because a settle that changed nothing and a
// settle against a row nobody wrote are different mistakes by the caller.
func (p *Postgres) whyTheRowDidNotSettle(ctx context.Context, id string, seq int) error {
	found, err := p.GetJob(ctx, id)
	if err != nil {
		return err
	}
	if found.Phase != job.PhaseRunning {
		return job.ErrNotRunning
	}
	held, there := job.TheQuestion(found.Questions, seq)
	if !there {
		return job.ErrNoSuchQuestion
	}
	if !held.Open() {
		return job.ErrQuestionSettled
	}
	return fmt.Errorf("settle job question: the row did not move and nothing says why")
}

// CarryJobQuestions writes rows onto a job that did not write them: down onto the reading that comes
// next, and back up onto the plan the readings are of.
//
// It is the engine's call and never a session's, which is why it asks nothing about the phase. The
// job a run carries is held back while its steps are out, and a reading that finished is not running
// either, so a rule about running would refuse both ends of the carry.
//
// A row already there by number is settled rather than inserted, and only while it is open, so the
// same reading merged twice leaves one row with one answer.
func (p *Postgres) CarryJobQuestions(ctx context.Context, id string, rows []job.Question) (*job.Job, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("carry job questions: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, one := range rows {
		status := one.Status
		if status == "" {
			status = job.QuestionOpen
		}
		var settledAt *time.Time
		if !one.SettledAt.IsZero() {
			when := one.SettledAt
			settledAt = &when
		}
		if _, err := tx.Exec(ctx, `
			insert into job_questions (job, seq, text, asked_by, asked_in, status, answer, settled_by, settled_at)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			on conflict (job, seq) do update
			set status = excluded.status, answer = excluded.answer,
				settled_by = excluded.settled_by, settled_at = excluded.settled_at
			where job_questions.status = $10 and excluded.status <> $10`,
			id, one.Seq, one.Text, one.AskedBy, one.AskedIn, status, one.Answer, one.SettledBy,
			settledAt, job.QuestionOpen); err != nil {
			return nil, fmt.Errorf("carry job questions: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("carry job questions: %w", err)
	}
	return p.GetJob(ctx, id)
}

// jobQuestions is what the readings of this job's plan could not settle, in the order they asked.
func (p *Postgres) jobQuestions(ctx context.Context, id string) ([]job.Question, error) {
	rows, err := p.pool.Query(ctx, `
		select job, seq, text, asked_by, asked_in, status, answer, settled_by, asked_at, settled_at
		from job_questions where job = $1 order by seq`, id)
	if err != nil {
		return nil, fmt.Errorf("read job questions: %w", err)
	}
	defer rows.Close()

	var questions []job.Question
	for rows.Next() {
		var one job.Question
		var settledAt *time.Time
		if err := rows.Scan(&one.Job, &one.Seq, &one.Text, &one.AskedBy, &one.AskedIn,
			&one.Status, &one.Answer, &one.SettledBy, &one.AskedAt, &settledAt); err != nil {
			return nil, fmt.Errorf("scan job question: %w", err)
		}
		if settledAt != nil {
			one.SettledAt = *settledAt
		}
		questions = append(questions, one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read job questions: %w", err)
	}
	return questions, nil
}

// insertJobQuestions writes the rows a job was handed in the same transaction as the job itself, so
// a reading is never declared with the list it was handed missing.
func insertJobQuestions(ctx context.Context, tx pgx.Tx, declared *job.Job) error {
	for _, one := range declared.Questions {
		status := one.Status
		if status == "" {
			status = job.QuestionOpen
		}
		if _, err := tx.Exec(ctx, `
			insert into job_questions (job, seq, text, asked_by, asked_in, status)
			values ($1, $2, $3, $4, $5, $6)
			on conflict (job, seq) do nothing`,
			declared.ID, one.Seq, one.Text, one.AskedBy, one.AskedIn, status); err != nil {
			return fmt.Errorf("create job: %w", err)
		}
	}
	return nil
}
