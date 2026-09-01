package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/flow"
	"github.com/atlantic-blue/quay-krewe/internal/job"
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

// FlowGraph returns one exact version, which is what a run already under way is carried on with.
func (p *Postgres) FlowGraph(ctx context.Context, name string, version int) (string, error) {
	var definition string
	err := p.pool.QueryRow(ctx,
		`select definition from flow_graphs where name = $1 and version = $2`, name, version).Scan(&definition)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("flow graph: %w", err)
	}
	return definition, nil
}

// DueFlowRuns are the waiting runs whose time has come. Only those: a system with a thousand finished
// runs and one waiting does one row's job per tick, which is what the partial index is for.
func (p *Postgres) DueFlowRuns(ctx context.Context, now time.Time) ([]*flow.Run, error) {
	rows, err := p.pool.Query(ctx, `
		select id, workspace, project, graph_name, graph_version, node, status, state, attempts,
		       transitions, spent, reason, due_at, question
		from flow_runs
		where status = $1 and due_at is not null and due_at <= $2
		order by due_at`, flow.StatusWaiting, now)
	if err != nil {
		return nil, fmt.Errorf("due flow runs: %w", err)
	}
	defer rows.Close()
	return scanFlowRuns(rows)
}

// CreateFlowRun writes a fresh run and the job that carries it, in one transaction.
//
// One transaction because a run outside the job tree is a run neither the depth limit nor the
// budget counts, and a job carrying a run that was never written is a row nothing explains.
func (p *Postgres) CreateFlowRun(ctx context.Context, run *flow.Run, carrier *job.Job, records []*job.Event, trigger string) error {
	state, attempts, err := runJSON(run)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("create flow run: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := insertJob(ctx, tx, carrier); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into flow_runs (id, workspace, project, graph_name, graph_version, node, status, state, attempts, transitions, spent, reason, question, job)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		run.ID, run.Workspace, run.Project, run.GraphName, run.GraphVersion,
		run.Node, run.Status, state, attempts, run.Transitions, run.Spent, run.Reason, run.Question,
		carrier.ID); err != nil {
		return fmt.Errorf("create flow run: %w", err)
	}
	if err := appendRunRecords(ctx, tx, records); err != nil {
		return err
	}
	// The trigger this run answers is marked started here rather than after the commit, which is
	// what makes one trigger start exactly one run: a poller that died in the gap would otherwise
	// leave a run written and a row still pending, and the next poller would start a second run.
	if trigger != "" {
		tag, err := tx.Exec(ctx, `
			update pending_triggers set status = $2, run = $3, updated_at = now()
			where id = $1 and status = $4`,
			trigger, flow.TriggerStarted, run.ID, flow.TriggerPending)
		if err != nil {
			return fmt.Errorf("mark a trigger started: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("store: trigger %s is no longer waiting to start a run, so this run was not written", trigger)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("create flow run: %w", err)
	}
	return nil
}

// FlowRunCarrier is the job that carries a run.
func (p *Postgres) FlowRunCarrier(ctx context.Context, run string) (string, error) {
	var carrier string
	err := p.pool.QueryRow(ctx,
		`select coalesce(job, '') from flow_runs where id = $1`, run).Scan(&carrier)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("flow run carrier: %w", err)
	}
	return carrier, nil
}

// LandedFlowSteps are the runs whose step has ended: working runs whose step reached a terminal
// phase. The poller reads these and nothing else, so a system with a thousand finished runs does the
// job of the few that are out with a step.
//
// The step is read back by identifier rather than joined, because a join of two tables that both
// carry id, status, workspace and project has to qualify every column, and one wrong prefix in that
// list reads the wrong row silently. There are only ever as many rows here as there are runs with a
// step that just ended.
func (p *Postgres) LandedFlowSteps(ctx context.Context, limit int) ([]flow.Landed, error) {
	query := `
		select id, workspace, project, graph_name, graph_version, node, status, state, attempts,
		       transitions, spent, reason, due_at, question, step_job
		from flow_runs
		where status = $1 and step_job is not null
		  and exists (select 1 from jobs w where w.id = flow_runs.step_job and w.phase = any($2))
		order by updated_at`
	if limit > 0 {
		query += fmt.Sprintf(" limit %d", limit)
	}
	rows, err := p.pool.Query(ctx, query, flow.StatusWorking, terminalPhases())
	if err != nil {
		return nil, fmt.Errorf("landed flow steps: %w", err)
	}
	steps := map[string]string{}
	runs, err := func() ([]*flow.Run, error) {
		defer rows.Close()
		out := make([]*flow.Run, 0)
		for rows.Next() {
			var run flow.Run
			var state, attempts []byte
			var step string
			if err := rows.Scan(&run.ID, &run.Workspace, &run.Project, &run.GraphName, &run.GraphVersion,
				&run.Node, &run.Status, &state, &attempts,
				&run.Transitions, &run.Spent, &run.Reason, &run.DueAt, &run.Question, &step); err != nil {
				return nil, fmt.Errorf("scan landed flow step: %w", err)
			}
			if err := json.Unmarshal(state, &run.State); err != nil {
				return nil, fmt.Errorf("read flow run state: %w", err)
			}
			if err := json.Unmarshal(attempts, &run.Attempts); err != nil {
				return nil, fmt.Errorf("read flow run attempts: %w", err)
			}
			steps[run.ID] = step
			out = append(out, &run)
		}
		return out, rows.Err()
	}()
	if err != nil {
		return nil, err
	}

	landed := make([]flow.Landed, 0, len(runs))
	for _, run := range runs {
		step, err := p.GetJob(ctx, steps[run.ID])
		if err != nil {
			return nil, fmt.Errorf("landed flow step %s: %w", steps[run.ID], err)
		}
		landed = append(landed, flow.Landed{Run: *run, Step: step})
	}
	return landed, nil
}

// AdvanceFlowRun moves a run, appends the transition, and claims the dispatch key, in one
// transaction: either the run moved and the record and the claim exist, or nothing happened. A
// dispatch key already claimed refuses the whole movement, which is what keeps the same task from
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

	// The step this movement declares, or nothing where it dispatched nothing, which also clears the
	// step the run was out with.
	step := ""
	if transition.Job.Declared != nil {
		step = transition.Job.Declared.ID
		if err := insertJob(ctx, tx, transition.Job.Declared); err != nil {
			return err
		}
	}
	// Only a live run moves, and only one still out with the step this movement answers. The first
	// condition is what makes stopping a run take effect: the operator's stop lands first, and this
	// finds nothing to update rather than setting the run back to running underneath them. The second
	// is what makes two pollers reading one landed step move the run once.
	tag, err := tx.Exec(ctx, `
		update flow_runs set node = $2, status = $3, state = $4, attempts = $5,
		    transitions = $6, spent = $7, reason = $8, due_at = $9, question = $10,
		    step_job = nullif($13, ''), updated_at = now()
		where id = $1 and status = any($11) and ($12 = '' or coalesce(step_job, '') = $12)`,
		run.ID, run.Node, run.Status, state, attempts, run.Transitions, run.Spent, run.Reason,
		transition.Due, run.Question, liveRunStatuses(), transition.Answers, step)
	if err != nil {
		return fmt.Errorf("advance flow run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Which of the two it is decides what the caller should do, so it is asked rather than
		// guessed: a run that is gone is an error, a run that moved on is an answer.
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
				return fmt.Errorf("store: run %s already dispatched node %s attempt %d, and the same task is never sent twice",
					run.ID, transition.Dispatch.Node, transition.Dispatch.Attempt)
			}
			return fmt.Errorf("claim flow dispatch: %w", err)
		}
	}
	if on := transition.Job.Carrier; on != nil {
		if _, err := tx.Exec(ctx, `
			update jobs set phase = $2, question = $3,
			    answer = case when $4 = '' then answer else $4 end,
			    reason = case when $5 = '' then reason else $5 end,
			    finished_at = case when $6 then coalesce(finished_at, now()) else finished_at end,
			    updated_at = now()
			where id = $1`,
			on.Job, on.Phase, on.Question, on.Answer, on.Reason, job.Terminal(on.Phase)); err != nil {
			return fmt.Errorf("advance the job carrying a flow run: %w", err)
		}
	}
	if err := appendRunRecords(ctx, tx, transition.Job.Records); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("advance flow run: %w", err)
	}
	return nil
}

// liveRunStatuses is every status a run still moves out of. A run that ended is not moved, because a
// movement written over that would undo the record of how it ended.
func liveRunStatuses() []string {
	return []string{flow.StatusRunning, flow.StatusWaiting, flow.StatusAsking, flow.StatusWorking}
}

// appendRunRecords writes the records of one movement of a run, in the transaction that moved it.
func appendRunRecords(ctx context.Context, tx pgx.Tx, records []*job.Event) error {
	for _, record := range records {
		if record == nil {
			continue
		}
		if err := appendJobEvent(ctx, tx, record); err != nil {
			return err
		}
	}
	return nil
}

// StopFlowRun halts a run that is still running, keeping the reason. A run that already ended is
// refused rather than overwritten: the record of how it ended is the useful part.
func (p *Postgres) StopFlowRun(ctx context.Context, id, reason string) (*flow.Run, error) {
	tag, err := p.pool.Exec(ctx, `
		update flow_runs set status = $2, reason = $3, updated_at = now()
		where id = $1 and status = any($4)`,
		id, flow.StatusStopped, reason, liveRunStatuses())
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
		       transitions, spent, reason, due_at, question
		from flow_runs where id = $1`, id).Scan(
		&run.Workspace, &run.Project, &run.GraphName, &run.GraphVersion,
		&run.Node, &run.Status, &state, &attempts,
		&run.Transitions, &run.Spent, &run.Reason, &run.DueAt, &run.Question)
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
		       transitions, spent, reason, due_at, question
		from flow_runs where ($1 = '' or project = $1) order by created_at desc, id desc`, project)
	if err != nil {
		return nil, fmt.Errorf("list flow runs: %w", err)
	}
	defer rows.Close()
	return scanFlowRuns(rows)
}

// scanFlowRuns reads a set of run rows, in the one column order every query above selects.
func scanFlowRuns(rows pgx.Rows) ([]*flow.Run, error) {
	out := make([]*flow.Run, 0)
	for rows.Next() {
		var run flow.Run
		var state, attempts []byte
		if err := rows.Scan(&run.ID, &run.Workspace, &run.Project, &run.GraphName, &run.GraphVersion,
			&run.Node, &run.Status, &state, &attempts,
			&run.Transitions, &run.Spent, &run.Reason, &run.DueAt, &run.Question); err != nil {
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
		return nil, fmt.Errorf("read flow runs: %w", err)
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

// ScheduleFlow records that a graph runs in a project every so often. Re-recording the same pair
// moves its schedule rather than making a second one, so importing a graph twice does not double
// the rate it runs at.
func (p *Postgres) ScheduleFlow(ctx context.Context, graph, project string, every time.Duration, next time.Time) error {
	_, err := p.pool.Exec(ctx, `
		insert into flow_schedules (graph_name, project, every_ms, next_at) values ($1, $2, $3, $4)
		on conflict (graph_name, project) do update set every_ms = excluded.every_ms, next_at = excluded.next_at`,
		graph, project, every.Milliseconds(), next)
	if err != nil {
		return fmt.Errorf("schedule flow: %w", err)
	}
	return nil
}

// UnscheduleFlow stops a graph running on its own in a project.
func (p *Postgres) UnscheduleFlow(ctx context.Context, graph, project string) error {
	tag, err := p.pool.Exec(ctx,
		`delete from flow_schedules where graph_name = $1 and project = $2`, graph, project)
	if err != nil {
		return fmt.Errorf("unschedule flow: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DueFlowSchedules are the schedules whose time has come, each carrying the workspace its project
// belongs to, because a run needs both.
func (p *Postgres) DueFlowSchedules(ctx context.Context, now time.Time) ([]flow.Schedule, error) {
	rows, err := p.pool.Query(ctx, `
		select s.graph_name, s.project, pr.workspace, s.every_ms
		from flow_schedules s join projects pr on pr.id = s.project
		where s.next_at <= $1 and pr.deleted_at is null
		order by s.next_at`, now)
	if err != nil {
		return nil, fmt.Errorf("due flow schedules: %w", err)
	}
	defer rows.Close()
	out := make([]flow.Schedule, 0)
	for rows.Next() {
		var one flow.Schedule
		var everyMS int64
		if err := rows.Scan(&one.GraphName, &one.Project, &one.Workspace, &everyMS); err != nil {
			return nil, fmt.Errorf("scan flow schedule: %w", err)
		}
		one.Every = time.Duration(everyMS) * time.Millisecond
		out = append(out, one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("due flow schedules: %w", err)
	}
	return out, nil
}

// MarkFlowScheduled moves a schedule on to its next due time.
func (p *Postgres) MarkFlowScheduled(ctx context.Context, graph, project string, next time.Time) error {
	tag, err := p.pool.Exec(ctx,
		`update flow_schedules set next_at = $3 where graph_name = $1 and project = $2`, graph, project, next)
	if err != nil {
		return fmt.Errorf("mark flow scheduled: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
