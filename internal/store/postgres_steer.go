package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/jackc/pgx/v5"
)

// RecordSteer writes one steer and adds it to the count on the job it landed on, in one transaction.
//
// One transaction because the score has to be defensible. A mark with no count reads as a job nobody
// steered, a count with no mark is a number nobody can check, and a number nobody can check is what
// this replaced.
func (p *Postgres) RecordSteer(ctx context.Context, steer *job.Steer) error {
	if steer == nil {
		return errors.New("store: a steer is not nothing")
	}
	if steer.ID == "" || steer.Job == "" {
		return errors.New("store: a steer needs an id and the job it landed on")
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("record steer: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The same steer twice leaves one, and counts once, which is what makes a retried call safe.
	tag, err := tx.Exec(ctx, `
		insert into job_steers (id, job, workspace, project, text, occurred_at)
		values ($1, $2, $3, $4, $5, coalesce($6, now()))
		on conflict (id) do nothing`,
		steer.ID, steer.Job, steer.Workspace, steer.Project, steer.Text,
		zeroAsNull(steer.OccurredAt))
	if err != nil {
		return fmt.Errorf("record steer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	counting, err := tx.Exec(ctx, `update jobs set steers = steers + 1 where id = $1`, steer.Job)
	if err != nil {
		return fmt.Errorf("count steer: %w", err)
	}
	if counting.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("record steer: %w", err)
	}
	return nil
}

// ListSteers returns one job's steers, oldest first.
//
// The identifier breaks a tie, so two steers stamped in the same instant read back in one order
// however often the query runs.
func (p *Postgres) ListSteers(ctx context.Context, of string) ([]*job.Steer, error) {
	rows, err := p.pool.Query(ctx, `
		select id, job, workspace, project, text, occurred_at
		from job_steers where job = $1 order by occurred_at, id`, of)
	if err != nil {
		return nil, fmt.Errorf("list steers: %w", err)
	}
	defer rows.Close()

	listed := []*job.Steer{}
	for rows.Next() {
		var one job.Steer
		if err := rows.Scan(&one.ID, &one.Job, &one.Workspace, &one.Project,
			&one.Text, &one.OccurredAt); err != nil {
			return nil, fmt.Errorf("list steers: %w", err)
		}
		listed = append(listed, &one)
	}
	if err := rows.Err(); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("list steers: %w", err)
	}
	return listed, nil
}

// zeroAsNull sends a moment nobody set as null, so the database's own clock stamps the row. The
// memory store stamps it the same way, because a store that leaves it empty passes every test the
// other fails.
func zeroAsNull(at time.Time) any {
	if at.IsZero() {
		return nil
	}
	return at
}
