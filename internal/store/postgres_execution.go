package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/jackc/pgx/v5"
)

// executionColumns is the row every read of an execution selects, in one place so a reader and a
// listing cannot drift into scanning different things. Every column here has to appear in
// scanExecution below, in this order: miss either and this store reads zero where the memory store
// passes.
const executionColumns = `id, job, stage, number, claim, phase, session, attempts, answer, outcome,
	reason, branch, pull_request, spent_tokens, lease_owner, lease_until, trace_id, parent_span_id,
	created_at, updated_at, started_at, finished_at`

// CreateExecution writes one run of one stage and the record of it in one transaction.
func (p *Postgres) CreateExecution(ctx context.Context, run *job.Execution, event *job.Event) error {
	if err := run.Validate(); err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("create execution: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := takeTheRunsClaim(ctx, tx, run); err != nil {
		return err
	}
	phase := run.Phase
	if phase == "" {
		phase = job.PhasePending
	}
	if _, err := tx.Exec(ctx, `
		insert into executions (id, job, stage, number, claim, phase, session, attempts, answer,
			outcome, reason, branch, pull_request, spent_tokens, lease_owner, lease_until, trace_id,
			parent_span_id, started_at, finished_at, created_at, updated_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18,
			$19, $20, coalesce($21::timestamptz, now()), coalesce($22::timestamptz, now()))`,
		run.ID, run.Job, run.Stage, run.Number, run.Claim, phase, run.Session, run.Attempts,
		run.Answer, run.Outcome, run.Reason, run.Branch, run.PullRequest, run.SpentTokens,
		run.LeaseOwner, run.LeaseUntil, run.TraceID, run.ParentSpanID, run.StartedAt, run.FinishedAt,
		stampOrNow(run.CreatedAt), stampOrNow(run.UpdatedAt)); err != nil {
		return fmt.Errorf("create execution: %w", err)
	}
	if err := appendJobEvent(ctx, tx, event); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("create execution: %w", err)
	}
	return nil
}

// takeTheRunsClaim refuses a run that claims a piece of work another run is still holding.
//
// The same shape as a job's claim and for the same reasons: a lock on the claim itself, inside the
// transaction that writes the row, because two ticks arriving together would otherwise both read no
// holder and both write one. There is no unique index doing this instead, because holding runs out.
func takeTheRunsClaim(ctx context.Context, tx pgx.Tx, run *job.Execution) error {
	if run.Claim == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('execution:' || $1))`,
		run.Claim); err != nil {
		return fmt.Errorf("create execution: %w", err)
	}
	row := tx.QueryRow(ctx, `
		select id, stage, number, created_at from executions
		where claim = $1 and phase = any($2::text[])
			and updated_at > now() - make_interval(secs => $3::double precision)
		order by created_at asc, id asc limit 1`,
		run.Claim, job.LivePhases(), job.ClaimLife.Seconds())
	held := &job.Held{Claim: run.Claim}
	var stage string
	var number int
	switch err := row.Scan(&held.Holder, &stage, &number, &held.TakenAt); {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("create execution: %w", err)
	default:
		held.Title = fmt.Sprintf("the %s stage of job %s, number %d", stage, run.Job, number)
		return held
	}
}

// GetExecution reads one run back, whole.
func (p *Postgres) GetExecution(ctx context.Context, id string) (*job.Execution, error) {
	row := p.pool.QueryRow(ctx, `select `+executionColumns+` from executions where id = $1`, id)
	found, err := scanExecution(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return found, err
}

// ListExecutions is the runs of one job, oldest first, and of one of its stages where the filter
// names one.
func (p *Postgres) ListExecutions(ctx context.Context, filter job.ExecutionFilter) ([]*job.Execution, error) {
	return p.executionsWhere(ctx, `where ($1 = '' or job = $1) and ($2 = '' or stage = $2)
		order by created_at asc, id asc`, 0, filter.Job, filter.Stage)
}

// RunnableExecutions is the runs a controller may start: pending, oldest first.
func (p *Postgres) RunnableExecutions(ctx context.Context, limit int) ([]*job.Execution, error) {
	return p.executionsWhere(ctx, `where phase = $1 order by created_at asc, id asc`, limit,
		job.PhasePending)
}

// HeldExecutions is the runs this controller holds: running, under a lease of its own that has not
// run out.
func (p *Postgres) HeldExecutions(ctx context.Context, owner string, limit int) ([]*job.Execution, error) {
	return p.executionsWhere(ctx, `where phase = $1 and lease_owner = $2 and lease_until > now()
		order by created_at asc, id asc`, limit, job.PhaseRunning, owner)
}

// ExpiredExecutions is the runs whose holder went away: running, under a lease that has run out or
// was never written.
func (p *Postgres) ExpiredExecutions(ctx context.Context, limit int) ([]*job.Execution, error) {
	return p.executionsWhere(ctx, `where phase = $1 and (lease_until is null or lease_until <= now())
		order by created_at asc, id asc`, limit, job.PhaseRunning)
}

// executionsWhere reads the rows one condition matches, applying the limit where there is one.
func (p *Postgres) executionsWhere(ctx context.Context, where string, limit int,
	args ...any) ([]*job.Execution, error) {
	query := `select ` + executionColumns + ` from executions ` + where
	if limit > 0 {
		query += fmt.Sprintf(" limit %d", limit)
	}
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list executions: %w", err)
	}
	defer rows.Close()

	listed := make([]*job.Execution, 0)
	for rows.Next() {
		found, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		listed = append(listed, found)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list executions: %w", err)
	}
	return listed, nil
}

// StartExecution claims one run and takes a lease on it, and applies only while it is pending.
func (p *Postgres) StartExecution(ctx context.Context, id string, lease job.Lease,
	event *job.Event) (*job.Execution, error) {
	return p.moveExecution(ctx, id, "start execution", job.ErrNotPending, event, `
		update executions set phase = $2, attempts = attempts + 1, lease_owner = $3, lease_until = $4,
			started_at = now(), updated_at = now()
		where id = $1 and phase = $5`,
		job.PhaseRunning, lease.Owner, lease.Until, job.PhasePending)
}

// TakeOverExecution takes the lease on a run whose holder went away, and applies only where the
// lease has run out, in the same statement, so two controllers finding it leave one holder.
func (p *Postgres) TakeOverExecution(ctx context.Context, id string, lease job.Lease) (*job.Execution, error) {
	return p.moveExecution(ctx, id, "take over execution", job.ErrNotRunning, nil, `
		update executions set lease_owner = $2, lease_until = $3, updated_at = now()
		where id = $1 and phase = $4 and (lease_until is null or lease_until <= now())`,
		lease.Owner, lease.Until, job.PhaseRunning)
}

// RenewExecutionLease moves this controller's hold on further. Only the holder renews.
func (p *Postgres) RenewExecutionLease(ctx context.Context, id string, lease job.Lease) error {
	_, err := p.moveExecution(ctx, id, "renew execution lease", job.ErrNotRunning, nil, `
		update executions set lease_until = $2, updated_at = now()
		where id = $1 and lease_owner = $3`, lease.Until, lease.Owner)
	return err
}

// RecordExecutionSession writes the session the system made for a run.
func (p *Postgres) RecordExecutionSession(ctx context.Context, id, session string) error {
	_, err := p.moveExecution(ctx, id, "record execution session", ErrNotFound, nil, `
		update executions set session = $2, updated_at = now() where id = $1`, session)
	return err
}

// RecordExecutionBranch writes where this run's commits ended up.
func (p *Postgres) RecordExecutionBranch(ctx context.Context, id, branch string) error {
	_, err := p.moveExecution(ctx, id, "record execution branch", ErrNotFound, nil, `
		update executions set branch = $2, updated_at = now() where id = $1`, branch)
	return err
}

// LandExecution writes what came of the run and lets go of the lease, and applies only while it runs.
func (p *Postgres) LandExecution(ctx context.Context, id string, landed job.ExecutionLanding,
	event *job.Event) (*job.Execution, error) {
	return p.moveExecution(ctx, id, "land execution", job.ErrNotRunning, event, `
		update executions set phase = $2, answer = $3, outcome = $4, reason = $5, pull_request = $6,
			spent_tokens = $7, lease_owner = '', lease_until = null, finished_at = now(),
			updated_at = now()
		where id = $1 and phase = $8`,
		landed.Phase, landed.Answer, landed.Outcome, landed.Reason, landed.PullRequest,
		landed.SpentTokens, job.PhaseRunning)
}

// moveExecution runs one conditional update and the record of it in one transaction, and reads the
// row back. A statement that changed nothing is the condition not holding, which is the caller's
// refusal rather than a fault.
func (p *Postgres) moveExecution(ctx context.Context, id, what string, refusal error,
	event *job.Event, statement string, args ...any) (*job.Execution, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, statement, append([]any{id}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	if tag.RowsAffected() == 0 {
		// The row is either not there at all or not in the state the statement wanted, and those are
		// two different answers to the caller.
		if _, err := p.GetExecution(ctx, id); err != nil {
			return nil, err
		}
		return nil, refusal
	}
	if err := appendJobEvent(ctx, tx, event); err != nil {
		return nil, err
	}
	found, err := scanExecution(tx.QueryRow(ctx,
		`select `+executionColumns+` from executions where id = $1`, id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return found, nil
}

// scanExecution reads one run off a row. The order is executionColumns above, and the two are read
// together or not at all.
func scanExecution(row rowScanner) (*job.Execution, error) {
	var found job.Execution
	if err := row.Scan(&found.ID, &found.Job, &found.Stage, &found.Number, &found.Claim,
		&found.Phase, &found.Session, &found.Attempts, &found.Answer, &found.Outcome, &found.Reason,
		&found.Branch, &found.PullRequest, &found.SpentTokens, &found.LeaseOwner, &found.LeaseUntil,
		&found.TraceID, &found.ParentSpanID, &found.CreatedAt, &found.UpdatedAt, &found.StartedAt,
		&found.FinishedAt); err != nil {
		return nil, err
	}
	return &found, nil
}

// StopExecution halts a run that has not ended, keeping the reason. Conditional on the phase, in the
// same statement, so a run that ended a moment ago is refused rather than overwritten.
func (p *Postgres) StopExecution(ctx context.Context, id, reason string,
	event *job.Event) (*job.Execution, error) {
	return p.moveExecution(ctx, id, "stop execution", job.ErrNotRunning, event, `
		update executions set phase = $2, reason = $3, lease_owner = '', lease_until = null,
			finished_at = now(), updated_at = now()
		where id = $1 and phase = any($4::text[])`,
		job.PhaseStopped, reason, job.LivePhases())
}
