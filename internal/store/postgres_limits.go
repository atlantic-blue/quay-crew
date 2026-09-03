package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/jackc/pgx/v5"
)

// WorkspaceLimits reads what a workspace lets its sessions declare.
//
// A workspace with no row takes the defaults, and the default for max_declared is zero: no session may
// declare job until an operator raises it. Default deny, so a system that was never configured grants
// nothing rather than everything.
func (p *Postgres) WorkspaceLimits(ctx context.Context, workspace string) (job.Limits, error) {
	limits := job.Limits{Workspace: workspace}
	// The column is mebibytes, because that is the unit an operator sets and the room view prints.
	// Bytes is what the arithmetic uses, so the turn happens here rather than in every reader.
	var memoryMiB int
	err := p.pool.QueryRow(ctx, `
		select max_declared, max_running, budget_tokens, lease_seconds, reclaim_seconds, archive_seconds,
			request_memory_mib, request_processor_percent, context_ceiling_percent, waiting_seconds
		from workspace_limits where workspace = $1`, workspace).Scan(
		&limits.MaxDeclared, &limits.MaxRunning, &limits.BudgetTokens, &limits.LeaseSeconds,
		&limits.ReclaimSeconds, &limits.ArchiveSeconds, &memoryMiB, &limits.RequestProcessor,
		&limits.ContextCeilingPercent, &limits.WaitingSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return job.Limits{Workspace: workspace}, nil
	}
	if err != nil {
		return job.Limits{}, fmt.Errorf("workspace limits: %w", err)
	}
	limits.RequestMemoryBytes = int64(memoryMiB) << 20
	return limits, nil
}

// SetWorkspaceLimits writes the ceiling, whole. Every field is written as it arrives, so reading
// first and sending the row back is how one of them is changed.
func (p *Postgres) SetWorkspaceLimits(ctx context.Context, limits job.Limits) (job.Limits, error) {
	if _, err := p.pool.Exec(ctx, `
		insert into workspace_limits
			(workspace, max_declared, max_running, budget_tokens, lease_seconds, reclaim_seconds,
			archive_seconds, request_memory_mib, request_processor_percent, context_ceiling_percent,
			waiting_seconds)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		on conflict (workspace) do update set
			max_declared = excluded.max_declared, max_running = excluded.max_running,
			budget_tokens = excluded.budget_tokens, lease_seconds = excluded.lease_seconds,
			reclaim_seconds = excluded.reclaim_seconds, archive_seconds = excluded.archive_seconds,
			request_memory_mib = excluded.request_memory_mib,
			request_processor_percent = excluded.request_processor_percent,
			context_ceiling_percent = excluded.context_ceiling_percent,
			waiting_seconds = excluded.waiting_seconds,
			updated_at = now()`,
		limits.Workspace, limits.MaxDeclared, limits.MaxRunning, limits.BudgetTokens, limits.LeaseSeconds,
		limits.ReclaimSeconds, limits.ArchiveSeconds, limits.RequestMemoryBytes>>20,
		limits.RequestProcessor, limits.ContextCeilingPercent, limits.WaitingSeconds); err != nil {
		return job.Limits{}, fmt.Errorf("set workspace limits: %w", err)
	}
	return p.WorkspaceLimits(ctx, limits.Workspace)
}
