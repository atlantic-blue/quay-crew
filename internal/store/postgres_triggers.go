package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/flow"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/jackc/pgx/v5"
)

// The pending trigger queue. A trigger row is written in the transaction of whatever caused it, read
// off an indexed query, and claimed with a conditional write in one statement, which is the same
// mechanism the system already uses for waits, for dispatch idempotency and for job events.
//
// The claim is the part that has to be exact. Two pollers reading the same pending row must leave one
// holder, so the condition and the write are one statement and never a read followed by an update.

// triggerColumns is the one column order every read below selects, so scanTrigger reads them all the
// same way. Written once rather than in each query, because one wrong prefix in a list of nine reads
// the wrong column silently.
const triggerColumns = `id, graph_name, workspace, project, payload, source, coalesce(cause, ''),
	status, coalesce(run, ''), reason, lease_owner, lease_until, attempts, raised_at`

// RaiseTrigger writes down that something happened, so a run of the flow it names starts on the next
// tick of the poller. One statement, so a caller can put it in the transaction of whatever caused it.
func (p *Postgres) RaiseTrigger(ctx context.Context, trigger *flow.Trigger) error {
	payload, err := json.Marshal(orEmpty(trigger.Payload))
	if err != nil {
		return fmt.Errorf("encode trigger payload: %w", err)
	}
	_, err = p.pool.Exec(ctx, `
		insert into pending_triggers (id, graph_name, workspace, project, payload, source, cause, status)
		values ($1, $2, $3, $4, $5, $6, nullif($7, ''), $8)`,
		trigger.ID, trigger.GraphName, trigger.Workspace, trigger.Project, payload,
		trigger.Source, trigger.Cause, flow.TriggerPending)
	if err != nil {
		return fmt.Errorf("raise trigger: %w", err)
	}
	return nil
}

// PendingTriggers are the triggers nothing has started a run from and nobody is holding, oldest
// first. Only those: a system with a million triggers behind it does the work of the few that arrived
// since the last tick, which is what the partial index is for.
func (p *Postgres) PendingTriggers(ctx context.Context, limit int) ([]*flow.Trigger, error) {
	query := `select ` + triggerColumns + ` from pending_triggers
		where status = $1 and (lease_until is null or lease_until <= now())
		order by raised_at, id`
	if limit > 0 {
		query += fmt.Sprintf(" limit %d", limit)
	}
	rows, err := p.pool.Query(ctx, query, flow.TriggerPending)
	if err != nil {
		return nil, fmt.Errorf("pending triggers: %w", err)
	}
	defer rows.Close()
	out := make([]*flow.Trigger, 0)
	for rows.Next() {
		trigger, err := scanTrigger(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, trigger)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pending triggers: %w", err)
	}
	return out, nil
}

// ClaimTrigger takes a lease on one trigger, and applies only where it is still pending and nobody
// else's claim is live. The condition is in the same statement as the write, so two pollers finding
// one pending trigger leave one holder and one ErrTriggerTaken.
func (p *Postgres) ClaimTrigger(ctx context.Context, id string, lease job.Lease) (*flow.Trigger, error) {
	row := p.pool.QueryRow(ctx, `
		update pending_triggers set lease_owner = $2, lease_until = $3, attempts = attempts + 1,
		    updated_at = now()
		where id = $1 and status = $4 and (lease_until is null or lease_until <= now())
		returning `+triggerColumns,
		id, lease.Owner, lease.Until, flow.TriggerPending)
	trigger, err := scanTrigger(row)
	if errors.Is(err, pgx.ErrNoRows) {
		// Held by somebody else, already acted on, or gone. Which one it is decides nothing the
		// caller does: the row is not this poller's.
		var held bool
		if err := p.pool.QueryRow(ctx,
			`select exists (select 1 from pending_triggers where id = $1)`, id).Scan(&held); err == nil && !held {
			return nil, ErrNotFound
		}
		return nil, flow.ErrTriggerTaken
	}
	if err != nil {
		return nil, err
	}
	return trigger, nil
}

// FailTrigger records that a claimed trigger started no run, and why.
//
// It applies only while the row is still pending, so a trigger that did start a run is never marked
// failed underneath it. The claim is let go with it, because there is nothing left to hold.
func (p *Postgres) FailTrigger(ctx context.Context, id, reason string) error {
	tag, err := p.pool.Exec(ctx, `
		update pending_triggers set status = $2, reason = $3, lease_owner = '', lease_until = null,
		    updated_at = now()
		where id = $1 and status = $4`,
		id, flow.TriggerFailed, reason, flow.TriggerPending)
	if err != nil {
		return fmt.Errorf("fail trigger: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetTrigger reads one trigger back, which is where what became of it is read.
func (p *Postgres) GetTrigger(ctx context.Context, id string) (*flow.Trigger, error) {
	row := p.pool.QueryRow(ctx, `select `+triggerColumns+` from pending_triggers where id = $1`, id)
	trigger, err := scanTrigger(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return trigger, nil
}

// scannable is a row from either a query or a single row read, so the column order above is read
// once for both.
type scannable interface {
	Scan(dest ...any) error
}

// scanTrigger reads one trigger row, in the order triggerColumns selects.
func scanTrigger(row scannable) (*flow.Trigger, error) {
	var trigger flow.Trigger
	var payload []byte
	var until *time.Time
	if err := row.Scan(&trigger.ID, &trigger.GraphName, &trigger.Workspace, &trigger.Project,
		&payload, &trigger.Source, &trigger.Cause, &trigger.Status, &trigger.Run, &trigger.Reason,
		&trigger.Lease.Owner, &until, &trigger.Attempts, &trigger.RaisedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan trigger: %w", err)
	}
	if until != nil {
		trigger.Lease.Until = *until
	}
	if err := json.Unmarshal(payload, &trigger.Payload); err != nil {
		return nil, fmt.Errorf("read trigger payload: %w", err)
	}
	return &trigger, nil
}
