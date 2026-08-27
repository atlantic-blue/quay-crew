package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/atlantic-blue/quay-crew/internal/work"
	"github.com/jackc/pgx/v5"
)

// WorkspaceLimits reads what a workspace lets its sessions declare.
//
// A workspace with no row takes the defaults, and the default for max_depth is zero: no session may
// declare work until an operator raises it. Default deny, so a crew that was never configured grants
// nothing rather than everything.
func (p *Postgres) WorkspaceLimits(ctx context.Context, workspace string) (work.Limits, error) {
	limits := work.Limits{Workspace: workspace}
	err := p.pool.QueryRow(ctx, `
		select max_depth, max_running, budget_tokens, lease_seconds
		from workspace_limits where workspace = $1`, workspace).Scan(
		&limits.MaxDepth, &limits.MaxRunning, &limits.BudgetTokens, &limits.LeaseSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return work.Limits{Workspace: workspace}, nil
	}
	if err != nil {
		return work.Limits{}, fmt.Errorf("workspace limits: %w", err)
	}
	return limits, nil
}

// SetWorkspaceLimits writes the ceiling, whole. Every field is written as it arrives, so reading
// first and sending the row back is how one of them is changed.
func (p *Postgres) SetWorkspaceLimits(ctx context.Context, limits work.Limits) (work.Limits, error) {
	if _, err := p.pool.Exec(ctx, `
		insert into workspace_limits (workspace, max_depth, max_running, budget_tokens, lease_seconds)
		values ($1, $2, $3, $4, $5)
		on conflict (workspace) do update set
			max_depth = excluded.max_depth, max_running = excluded.max_running,
			budget_tokens = excluded.budget_tokens, lease_seconds = excluded.lease_seconds,
			updated_at = now()`,
		limits.Workspace, limits.MaxDepth, limits.MaxRunning, limits.BudgetTokens, limits.LeaseSeconds); err != nil {
		return work.Limits{}, fmt.Errorf("set workspace limits: %w", err)
	}
	return p.WorkspaceLimits(ctx, limits.Workspace)
}
