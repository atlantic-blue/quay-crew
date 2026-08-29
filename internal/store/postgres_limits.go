package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/jackc/pgx/v5"
)

// WorkspaceLimits reads what a workspace lets its sessions declare.
//
// A workspace with no row takes the defaults, and the default for max_depth is zero: no session may
// declare job until an operator raises it. Default deny, so a crew that was never configured grants
// nothing rather than everything.
func (p *Postgres) WorkspaceLimits(ctx context.Context, workspace string) (job.Limits, error) {
	limits := job.Limits{Workspace: workspace}
	err := p.pool.QueryRow(ctx, `
		select max_depth, max_running, budget_tokens, lease_seconds, reclaim_seconds, archive_seconds
		from workspace_limits where workspace = $1`, workspace).Scan(
		&limits.MaxDepth, &limits.MaxRunning, &limits.BudgetTokens, &limits.LeaseSeconds,
		&limits.ReclaimSeconds, &limits.ArchiveSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return job.Limits{Workspace: workspace}, nil
	}
	if err != nil {
		return job.Limits{}, fmt.Errorf("workspace limits: %w", err)
	}
	return limits, nil
}

// SetWorkspaceLimits writes the ceiling, whole. Every field is written as it arrives, so reading
// first and sending the row back is how one of them is changed.
func (p *Postgres) SetWorkspaceLimits(ctx context.Context, limits job.Limits) (job.Limits, error) {
	if _, err := p.pool.Exec(ctx, `
		insert into workspace_limits
			(workspace, max_depth, max_running, budget_tokens, lease_seconds, reclaim_seconds, archive_seconds)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (workspace) do update set
			max_depth = excluded.max_depth, max_running = excluded.max_running,
			budget_tokens = excluded.budget_tokens, lease_seconds = excluded.lease_seconds,
			reclaim_seconds = excluded.reclaim_seconds, archive_seconds = excluded.archive_seconds,
			updated_at = now()`,
		limits.Workspace, limits.MaxDepth, limits.MaxRunning, limits.BudgetTokens, limits.LeaseSeconds,
		limits.ReclaimSeconds, limits.ArchiveSeconds); err != nil {
		return job.Limits{}, fmt.Errorf("set workspace limits: %w", err)
	}
	return p.WorkspaceLimits(ctx, limits.Workspace)
}
