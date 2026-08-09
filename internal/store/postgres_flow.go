package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/atlantic-blue/quay-crew/internal/flow"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ImportFlowGraph stores a graph at a version. A version that exists is refused rather than
// replaced, because a run is pinned to the version it started with and a pin that can move is not
// one.
func (p *Postgres) ImportFlowGraph(ctx context.Context, name string, version int, definition string) error {
	_, err := p.pool.Exec(ctx, `
		insert into flow_graphs (name, version, definition) values ($1, $2, $3)`,
		name, version, definition)
	if isUniqueViolation(err) {
		return fmt.Errorf("store: graph %s version %d is already imported, and a version never changes; import the next one", name, version)
	}
	if err != nil {
		return fmt.Errorf("import flow graph: %w", err)
	}
	return nil
}

// LatestFlowGraph returns the newest version of a graph.
func (p *Postgres) LatestFlowGraph(ctx context.Context, name string) (int, string, error) {
	var version int
	var definition string
	err := p.pool.QueryRow(ctx, `
		select version, definition from flow_graphs
		where name = $1 order by version desc limit 1`, name).Scan(&version, &definition)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", ErrNotFound
	}
	if err != nil {
		return 0, "", fmt.Errorf("latest flow graph: %w", err)
	}
	return version, definition, nil
}

// CreateFlowRun writes a fresh run.
func (p *Postgres) CreateFlowRun(ctx context.Context, run *flow.Run) error {
	state, attempts, err := runJSON(run)
	if err != nil {
		return err
	}
	if _, err := p.pool.Exec(ctx, `
		insert into flow_runs (id, workspace, project, graph_name, graph_version, node, status, state, attempts, transitions, spent, reason)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		run.ID, run.Workspace, run.Project, run.GraphName, run.GraphVersion,
		run.Node, run.Status, state, attempts, run.Transitions, run.Spent, run.Reason); err != nil {
		return fmt.Errorf("create flow run: %w", err)
	}
	return nil
}

// AdvanceFlowRun moves a run, appends the transition, and claims the dispatch key, in one
// transaction: either the run moved and the record and the claim exist, or nothing happened. A
// dispatch key already claimed refuses the whole movement, which is what keeps the same turn from
// ever being sent twice.
func (p *Postgres) AdvanceFlowRun(ctx context.Context, run *flow.Run, transition flow.Transition) error {
	state, attempts, err := runJSON(run)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("advance flow run: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Only a run the database still holds as running moves. That condition is what makes stopping
	// one take effect: the operator's stop lands first, and this finds nothing to update rather
	// than setting the run back to running underneath them.
	tag, err := tx.Exec(ctx, `
		update flow_runs set node = $2, status = $3, state = $4, attempts = $5,
		    transitions = $6, spent = $7, reason = $8, updated_at = now()
		where id = $1 and status = $9`,
		run.ID, run.Node, run.Status, state, attempts, run.Transitions, run.Spent, run.Reason,
		flow.StatusRunning)
	if err != nil {
		return fmt.Errorf("advance flow run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Which of the two it is decides what the caller should do, so it is asked rather than
		// guessed: a run that is gone is an error, a run that was halted is an answer.
		var status string
		if err := p.pool.QueryRow(ctx, `select status from flow_runs where id = $1`, run.ID).Scan(&status); err != nil {
			return ErrNotFound
		}
		return flow.ErrRunHalted
	}

	if _, err := tx.Exec(ctx, `
		insert into flow_run_events (run, seq, event, node)
		values ($1, (select coalesce(max(seq), 0) + 1 from flow_run_events where run = $1), $2, $3)`,
		run.ID, transition.Event, transition.Node); err != nil {
		return fmt.Errorf("record flow transition: %w", err)
	}

	if transition.Dispatch != nil {
		if _, err := tx.Exec(ctx, `
			insert into flow_dispatches (run, node, attempt) values ($1, $2, $3)`,
			run.ID, transition.Dispatch.Node, transition.Dispatch.Attempt); err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("store: run %s already dispatched node %s attempt %d, and the same turn is never sent twice",
					run.ID, transition.Dispatch.Node, transition.Dispatch.Attempt)
			}
			return fmt.Errorf("claim flow dispatch: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("advance flow run: %w", err)
	}
	return nil
}

// StopFlowRun halts a run that is still running, keeping the reason. A run that already ended is
// refused rather than overwritten: the record of how it ended is the useful part.
func (p *Postgres) StopFlowRun(ctx context.Context, id, reason string) (*flow.Run, error) {
	tag, err := p.pool.Exec(ctx, `
		update flow_runs set status = $2, reason = $3, updated_at = now()
		where id = $1 and status = $4`,
		id, flow.StatusStopped, reason, flow.StatusRunning)
	if err != nil {
		return nil, fmt.Errorf("stop flow run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		run, err := p.GetFlowRun(ctx, id)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("store: run %s is %s, and a run that already ended is not stopped again", id, run.Status)
	}
	return p.GetFlowRun(ctx, id)
}

// GetFlowRun reads one run back.
func (p *Postgres) GetFlowRun(ctx context.Context, id string) (*flow.Run, error) {
	run := flow.Run{ID: id}
	var state, attempts []byte
	err := p.pool.QueryRow(ctx, `
		select workspace, project, graph_name, graph_version, node, status, state, attempts,
		       transitions, spent, reason
		from flow_runs where id = $1`, id).Scan(
		&run.Workspace, &run.Project, &run.GraphName, &run.GraphVersion,
		&run.Node, &run.Status, &state, &attempts,
		&run.Transitions, &run.Spent, &run.Reason)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get flow run: %w", err)
	}
	if err := json.Unmarshal(state, &run.State); err != nil {
		return nil, fmt.Errorf("read flow run state: %w", err)
	}
	if err := json.Unmarshal(attempts, &run.Attempts); err != nil {
		return nil, fmt.Errorf("read flow run attempts: %w", err)
	}
	return &run, nil
}

// ListFlowRuns lists runs, newest first, narrowed to one project when project is set.
func (p *Postgres) ListFlowRuns(ctx context.Context, project string) ([]*flow.Run, error) {
	rows, err := p.pool.Query(ctx, `
		select id, workspace, project, graph_name, graph_version, node, status, state, attempts,
		       transitions, spent, reason
		from flow_runs where ($1 = '' or project = $1) order by created_at desc, id desc`, project)
	if err != nil {
		return nil, fmt.Errorf("list flow runs: %w", err)
	}
	defer rows.Close()
	out := make([]*flow.Run, 0)
	for rows.Next() {
		var run flow.Run
		var state, attempts []byte
		if err := rows.Scan(&run.ID, &run.Workspace, &run.Project, &run.GraphName, &run.GraphVersion,
			&run.Node, &run.Status, &state, &attempts,
			&run.Transitions, &run.Spent, &run.Reason); err != nil {
			return nil, fmt.Errorf("scan flow run: %w", err)
		}
		if err := json.Unmarshal(state, &run.State); err != nil {
			return nil, fmt.Errorf("read flow run state: %w", err)
		}
		if err := json.Unmarshal(attempts, &run.Attempts); err != nil {
			return nil, fmt.Errorf("read flow run attempts: %w", err)
		}
		out = append(out, &run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list flow runs: %w", err)
	}
	return out, nil
}

// ListFlowTransitions reads a run's movements back, in the order they happened.
func (p *Postgres) ListFlowTransitions(ctx context.Context, run string) ([]flow.RecordedTransition, error) {
	rows, err := p.pool.Query(ctx, `
		select seq, event, node from flow_run_events where run = $1 order by seq`, run)
	if err != nil {
		return nil, fmt.Errorf("list flow transitions: %w", err)
	}
	defer rows.Close()
	out := make([]flow.RecordedTransition, 0)
	for rows.Next() {
		var one flow.RecordedTransition
		if err := rows.Scan(&one.Seq, &one.Event, &one.Node); err != nil {
			return nil, fmt.Errorf("scan flow transition: %w", err)
		}
		out = append(out, one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list flow transitions: %w", err)
	}
	return out, nil
}

// runJSON encodes the run's two maps for their jsonb columns.
func runJSON(run *flow.Run) ([]byte, []byte, error) {
	state, err := json.Marshal(orEmpty(run.State))
	if err != nil {
		return nil, nil, fmt.Errorf("encode flow run state: %w", err)
	}
	attempts, err := json.Marshal(run.Attempts)
	if err != nil {
		return nil, nil, fmt.Errorf("encode flow run attempts: %w", err)
	}
	return state, attempts, nil
}

// orEmpty keeps a nil map from encoding as null, which jsonb would faithfully store and a reader
// would faithfully trip over.
func orEmpty(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// isUniqueViolation says an insert hit a primary key or unique constraint.
func isUniqueViolation(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && pgError.Code == "23505"
}
