package store

import (
	"context"
	"fmt"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// What a session leaves behind when it reaches its workspace's context ceiling, and the movement that
// gives the rest of the job to a fresh conversation.

// RecordJobHandoff writes down the state a fresh session starts this job from.
//
// The record of it lands in the same transaction as the handoff, the way every other write here does.
// The phase lives in the statement rather than in a read before it: only a running job is handed over,
// because a note about work that already ended is not a handoff and no fresh session is given one.
func (p *Postgres) RecordJobHandoff(ctx context.Context, id, left, tried, session string,
	event *job.Event) (*job.Job, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("record job handoff: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		insert into job_handoffs (job, seq, remaining, tried, session)
		select $1, coalesce((select max(seq) from job_handoffs where job = $1), 0) + 1, $2, $3, $4
		where exists (select 1 from jobs where id = $1 and phase = $5)`,
		id, left, tried, session, job.PhaseRunning)
	if err != nil {
		return nil, fmt.Errorf("record job handoff: %w", err)
	}
	// Nothing was written, which is a job that is not running or a job that is not there. The two read
	// the same from the statement and never to a caller: one of them is a job that has simply ended,
	// and reporting it as missing would send somebody looking for a row that is right there.
	if tag.RowsAffected() == 0 {
		if _, err := p.GetJob(ctx, id); err != nil {
			return nil, err
		}
		return nil, job.ErrNotRunning
	}
	if err := appendJobEvent(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("record job handoff: %w", err)
	}
	return p.GetJob(ctx, id)
}

// HandOffJob puts a running job back to pending and lets go of the session that was doing it.
//
// Only from running and only for the controller holding the lease, in the same statement, which is
// the condition a requeue carries and for the same reason: a controller that lost the row must not
// take the job away from the session another controller is running it in.
//
// The session is cleared, which is the one place this differs from a resume. A resume goes back into
// the conversation that did the work because the work is in it; this exists because that conversation
// is full. The empty session is also what tells the system the newest handoff is still waiting to be
// taken up.
// Nothing is moved where nothing was written down. That condition is in the statement rather than in
// a read before it, for the reason the others are: the session is still answering while a controller
// decides, so a handoff written a moment later must not be missed and a job with none must not be
// moved on a stale read. A fresh session started from nothing pays for every discovery the last one
// made, which is more than the session at the ceiling would have cost.
func (p *Postgres) HandOffJob(ctx context.Context, id string, back job.Requeue,
	event *job.Event) (*job.Job, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("hand off job: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		update jobs set phase = $2, session = '', reason = '', lease_owner = '', lease_until = null,
			started_at = null, updated_at = now()
		where id = $1 and phase = $3 and lease_owner = $4
			and exists (select 1 from job_handoffs where job = jobs.id)`,
		id, job.PhasePending, job.PhaseRunning, back.Owner)
	if err != nil {
		return nil, fmt.Errorf("hand off job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Three causes read the same from the statement and never to a caller: the job is gone, it is
		// not this controller's to move, or the session wrote nothing down. Only the last one ends the
		// job, so they are told apart here.
		found, err := p.GetJob(ctx, id)
		if err != nil {
			return nil, err
		}
		if found.Phase == job.PhaseRunning && found.LeaseOwner == back.Owner && len(found.Handoffs) == 0 {
			return nil, job.ErrNothingHandedOver
		}
		return nil, job.ErrHeld
	}
	if err := appendJobEvent(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("hand off job: %w", err)
	}
	return p.GetJob(ctx, id)
}

// jobHandoffs is what one job's sessions left behind, in the order they wrote them.
func (p *Postgres) jobHandoffs(ctx context.Context, id string) ([]job.Handoff, error) {
	rows, err := p.pool.Query(ctx, `
		select job, seq, remaining, tried, session, written_at
		from job_handoffs where job = $1 order by seq`, id)
	if err != nil {
		return nil, fmt.Errorf("read job handoffs: %w", err)
	}
	defer rows.Close()

	var handoffs []job.Handoff
	for rows.Next() {
		var one job.Handoff
		if err := rows.Scan(&one.Job, &one.Seq, &one.Left, &one.Tried, &one.Session, &one.WrittenAt); err != nil {
			return nil, fmt.Errorf("scan job handoff: %w", err)
		}
		handoffs = append(handoffs, one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read job handoffs: %w", err)
	}
	return handoffs, nil
}
