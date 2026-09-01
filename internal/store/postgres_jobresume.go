package store

import (
	"context"
	"fmt"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// The steps a job finished, and the two answers to a job that failed: continue it, or refuse it.

// RecordJobStep writes down one thing the session doing this job finished.
//
// The record of the step lands in the same transaction as the step, the way every other write here
// does. Two conditions live in the statement rather than in a read before it. The job has to be running,
// so a step cannot be recorded against a job nobody is doing, and the same words already on the
// record leave one row rather than two: the record is the set of what is finished, and a session
// that is continued says again what it said before.
func (p *Postgres) RecordJobStep(ctx context.Context, id, summary, pullRequest string, event *job.Event) (*job.Job, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("record job step: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		insert into job_steps (job, seq, summary)
		select $1, coalesce((select max(seq) from job_steps where job = $1), 0) + 1, $2
		where exists (select 1 from jobs where id = $1 and phase = $3)
		on conflict (job, summary) do nothing`, id, summary, job.PhaseRunning)
	if err != nil {
		return nil, fmt.Errorf("record job step: %w", err)
	}
	// Nothing was written, which is either a step already on the record or a job nobody is running.
	// The two are told apart by the row, and only the second is a refusal. A step already there
	// writes no second record either: the record is the set of what is finished.
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
	// What the job produced, onto the row, in the same transaction. Only where the row carries none,
	// so the first address a job names is the one it keeps.
	if pullRequest != "" {
		if _, err := tx.Exec(ctx, `
			update jobs set pull_request = $2, updated_at = now()
			where id = $1 and pull_request = ''`, id, pullRequest); err != nil {
			return nil, fmt.Errorf("record job step: %w", err)
		}
	}
	if err := appendJobEvent(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("record job step: %w", err)
	}
	return p.GetJob(ctx, id)
}

// ResumeJob puts a job that failed back to pending, so a controller starts it again in the session
// it has been in all along.
//
// Only from failed, in the same statement, so two resumes leave one attempt rather than two tasks
// against one conversation. What it failed with moves onto the row as the failure this attempt is
// continuing past, and the reason is cleared: a pending job carrying a reason is one the system is
// holding back for want of a machine, and this one is going again.
//
// The moment it finished is left alone. It is when the attempt being continued ended, which is what
// the session is told to measure its base against, and the next landing writes it again.
func (p *Postgres) ResumeJob(ctx context.Context, id string, event *job.Event) (*job.Job, error) {
	return p.moveJob(ctx, id, "resume job", job.ErrNotFailed, []*job.Event{event}, `
		update jobs set phase = $2, resuming = reason, reason = '', started_at = null,
			lease_owner = '', lease_until = null, updated_at = now()
		where id = $1 and phase = $3`,
		job.PhasePending, job.PhaseFailed)
}

// RefuseJob ends a job that failed, on purpose, so nobody continues it.
//
// Only from failed, which is the phase a resume applies to: refusing is the other answer to the same
// question. It lands in stopped, because stopped is the system's word for an end somebody decided
// on, and a stopped job is never continued.
func (p *Postgres) RefuseJob(ctx context.Context, id, reason string, event *job.Event) (*job.Job, error) {
	return p.moveJob(ctx, id, "refuse job", job.ErrNotFailed, []*job.Event{event}, `
		update jobs set phase = $2, reason = $3, finished_at = now(), updated_at = now()
		where id = $1 and phase = $4`,
		job.PhaseStopped, reason, job.PhaseFailed)
}

// jobSteps is what one job's session said it finished, in the order it finished it.
func (p *Postgres) jobSteps(ctx context.Context, id string) ([]job.Step, error) {
	rows, err := p.pool.Query(ctx, `
		select job, seq, summary, finished_at from job_steps where job = $1 order by seq`, id)
	if err != nil {
		return nil, fmt.Errorf("read job steps: %w", err)
	}
	defer rows.Close()

	var steps []job.Step
	for rows.Next() {
		var one job.Step
		if err := rows.Scan(&one.Job, &one.Seq, &one.Summary, &one.FinishedAt); err != nil {
			return nil, fmt.Errorf("scan job step: %w", err)
		}
		steps = append(steps, one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read job steps: %w", err)
	}
	return steps, nil
}
