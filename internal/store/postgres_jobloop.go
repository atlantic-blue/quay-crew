package store

import (
	"context"
	"fmt"

	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/jackc/pgx/v5"
)

// What each attempt at a job said, and what the system does when three of them at one step say the
// same thing.

// LoopJob writes that a job went in circles and takes the route the job declared, in one transaction.
//
// The condition is in the statement: running, and held by the controller writing it. Two controllers
// cannot both escalate one job, and a controller that lost the row cannot move it at all.
//
// Where the job is handed to another role it goes back to pending and leaves its conversation behind,
// because a role is read only when a session is born. Where it had escalated once already it stops:
// escalating a second time would be the system going round the same loop with more steps in it.
func (p *Postgres) LoopJob(ctx context.Context, id string, looped job.Loop, event *job.Event) (*job.Job, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("loop job: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		update jobs set phase = $2, looped_step = $3,
			-- The route taken, unless the job had already escalated: that row keeps the route it took
			-- the first time, which is what a reader needs to see why this one stopped.
			escalated_to = case when $4 <> '' then $4 else escalated_to end,
			question = $5, reason = $6,
			-- A job going somewhere else is not continuing past the failure of the attempt that ended.
			resuming = '',
			-- A job handed to another role starts a conversation of its own. What the conversation it
			-- went in circles in was stays on the attempts it made there.
			session = case when $7 then '' else session end,
			started_at = case when $2 = $9 then null else started_at end,
			finished_at = case when $2 = $10 then now() else finished_at end,
			observed_version = case when $2 = $10 then version else observed_version end,
			lease_owner = '', lease_until = null, updated_at = now()
		where id = $1 and phase = $8 and lease_owner = $11`,
		id, looped.Phase, looped.Step, looped.To, looped.Question, looped.Reason, looped.Handed,
		job.PhaseRunning, job.PhasePending, job.PhaseStopped, looped.Owner)
	if err != nil {
		return nil, fmt.Errorf("loop job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		found, err := p.GetJob(ctx, id)
		if err != nil {
			return nil, err
		}
		if found.Phase != job.PhaseRunning {
			return nil, job.ErrNotRunning
		}
		return nil, job.ErrHeld
	}
	if err := insertJobAttempt(ctx, tx, id, looped.Attempt); err != nil {
		return nil, err
	}
	if err := appendJobEvent(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("loop job: %w", err)
	}
	return p.GetJob(ctx, id)
}

// insertJobAttempt writes down what one attempt said, inside a transaction somebody else owns.
//
// Keyed on the task, and the same task twice leaves one row. The same task is read again by whichever
// controller holds the job next, and an attempt counted twice would manufacture a loop out of one
// piece of work. The sequence is taken from the rows already there rather than from the caller, so
// two controllers cannot write one number twice.
func insertJobAttempt(ctx context.Context, tx pgx.Tx, id string, attempt *job.Attempt) error {
	if attempt == nil || attempt.Task == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		insert into job_attempts (job, task, seq, step, session, said, similarity)
		values ($1, $2, (select count(*) + 1 from job_attempts where job = $1), $3, $4, $5, $6)
		on conflict (job, task) do nothing`,
		id, attempt.Task, attempt.Step, attempt.Session, attempt.Said, attempt.Similarity); err != nil {
		return fmt.Errorf("record job attempt: %w", err)
	}
	return nil
}

// jobAttempts is what each attempt at one job said, in the order the attempts were made.
func (p *Postgres) jobAttempts(ctx context.Context, id string) ([]job.Attempt, error) {
	rows, err := p.pool.Query(ctx, `
		select job, task, seq, step, session, said, similarity, occurred_at
		from job_attempts where job = $1 order by seq`, id)
	if err != nil {
		return nil, fmt.Errorf("read job attempts: %w", err)
	}
	defer rows.Close()

	var attempts []job.Attempt
	for rows.Next() {
		var one job.Attempt
		if err := rows.Scan(&one.Job, &one.Task, &one.Seq, &one.Step, &one.Session, &one.Said,
			&one.Similarity, &one.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan job attempt: %w", err)
		}
		attempts = append(attempts, one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read job attempts: %w", err)
	}
	return attempts, nil
}
