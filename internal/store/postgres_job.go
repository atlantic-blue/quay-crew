package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/jackc/pgx/v5"
)

// jobColumns is the row every read of a job selects, in one place so a reader and a
// listing cannot drift into scanning different things.
const jobColumns = `id, workspace, project, title, brief, role, role_version, mode, expect_file,
	expect_contains, after_jobs, deadline, budget_tokens, labels, requires, coalesce(parent, ''), depth, version,
	phase, session, attempts, answer, reason, question, told, resuming, spent_tokens, observed_version,
	lease_owner, lease_until, trace_id, parent_span_id, repository, pull_request, product, steers, claim,
	escalation, looped_step, escalated_to, plan, plan_approved, ungated, reviewed, tested,
	created_at, updated_at, started_at, finished_at`

// CreateJob writes a job and the record of its declaration in one transaction.
//
// One transaction because the store is the source of truth here. A row with no record of how it came
// to exist, or a record of a declaration that is not there, are both states nothing can explain
// afterwards.
func (p *Postgres) CreateJob(ctx context.Context, declared *job.Job, event *job.Event) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := takeTheClaim(ctx, tx, declared); err != nil {
		return err
	}
	if err := insertJob(ctx, tx, declared); err != nil {
		return err
	}
	if err := appendJobEvent(ctx, tx, event); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

// takeTheClaim refuses a declaration that claims a piece of work another job is still holding.
//
// Inside the transaction that writes the row, and behind a lock taken on the claim itself, because
// two declarations arriving together would otherwise both read no holder and both write one: neither
// transaction can see the other's row until it commits. The lock is held to the end of the
// transaction, so the second one reads what the first wrote.
//
// The lock is on the claim rather than on the table, so declarations of different work never wait for
// each other. Two different claims that hash to the same number wait for one another and both then
// succeed, which costs a moment and is not a wrong answer.
//
// There is no unique index doing this instead, because holding runs out: a job that settles releases
// its claim, and so does one that nothing has moved for longer than a claim lives. An index cannot
// say either.
func takeTheClaim(ctx context.Context, tx pgx.Tx, declared *job.Job) error {
	if declared.Claim == "" {
		return nil
	}
	// The two joined by a colon, which a workspace identifier cannot contain, so one workspace's claim
	// never hashes to another's by accident of where the two words were cut.
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1 || ':' || $2))`,
		declared.Workspace, declared.Claim); err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	// The oldest holder, so the answer is the same one every time.
	row := tx.QueryRow(ctx, `
		select id, title, created_at from jobs
		where workspace = $1 and claim = $2 and phase = any($3::text[])
			and updated_at > now() - make_interval(secs => $4::double precision)
		order by created_at asc, id asc limit 1`,
		declared.Workspace, declared.Claim, job.LivePhases(), job.ClaimLife.Seconds())
	held := &job.Held{Claim: declared.Claim}
	switch err := row.Scan(&held.Holder, &held.Title, &held.TakenAt); {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("create job: %w", err)
	default:
		return held
	}
}

// insertJob writes one job inside a transaction somebody else owns, which is what lets a
// declaration land with whatever asked for it: a caller's call, or the movement of a flow run.
//
// The whole record, status fields included, because the store keeps what it is handed. Writing only
// the declared half would leave the two stores disagreeing about the same call, which is how a double
// that accepts more than the real thing manufactures a green suite.
func insertJob(ctx context.Context, tx pgx.Tx, declared *job.Job) error {
	labels, err := json.Marshal(labelsOrEmpty(declared.Labels))
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into jobs (id, workspace, project, title, brief, role, role_version, mode, expect_file,
			expect_contains, after_jobs, deadline, budget_tokens, labels, requires, parent, depth, version, phase,
			session, attempts, answer, reason, question, told, spent_tokens, observed_version, started_at,
			finished_at, lease_owner, lease_until, trace_id, parent_span_id, repository, pull_request, product,
			resuming, claim, escalation, ungated, reviewed, tested, created_at, updated_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18,
			$19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38,
			$39, $40, $41, $42, coalesce($43::timestamptz, now()), coalesce($44::timestamptz, now()))`,
		declared.ID, declared.Workspace, declared.Project, declared.Title, declared.Brief,
		declared.Role, declared.RoleVersion, declared.Mode, declared.ExpectFile, declared.ExpectContains,
		afterOrEmpty(declared.After), declared.Deadline, declared.BudgetTokens, string(labels),
		afterOrEmpty(declared.Requires), nullIfEmpty(declared.Parent), declared.Depth, declared.Version, declared.Phase,
		declared.Session, declared.Attempts, declared.Answer, declared.Reason, declared.Question,
		declared.Told, declared.SpentTokens, declared.ObservedVersion, declared.StartedAt, declared.FinishedAt,
		declared.LeaseOwner, declared.LeaseUntil, declared.TraceID, declared.ParentSpanID,
		declared.Repository, declared.PullRequest, declared.Product, declared.Resuming,
		declared.Claim, declared.Escalation,
		declared.Ungated, declared.Reviewed, declared.Tested,
		stampOrNow(declared.CreatedAt), stampOrNow(declared.UpdatedAt)); err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

// stampOrNow is a moment the caller stamped on a row, and nothing where it stamped none, which the
// statement reads as the database's clock.
//
// The in memory store already keeps a moment it is handed and stamps only what arrives empty, so
// this is the same store keeping what it is given. Nothing in the system stamps a job it declares,
// and what it buys is a test that can write a job whose last movement was three hours ago: a claim
// running out is otherwise unprovable without waiting for it.
func stampOrNow(at time.Time) *time.Time {
	if at.IsZero() {
		return nil
	}
	return &at
}

// GetJob reads one job back, whole: its answer and the steps its session finished.
//
// The steps are read here and not in a listing, for the reason the answer is: a listing of a hundred
// lists is a listing nobody can read. This is also the read every movement of a job ends in, so the
// controller building a task from a job it has just claimed gets what that job already finished.
func (p *Postgres) GetJob(ctx context.Context, id string) (*job.Job, error) {
	row := p.pool.QueryRow(ctx, `select `+jobColumns+` from jobs where id = $1`, id)
	found, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}
	if found.Steps, err = p.jobSteps(ctx, id); err != nil {
		return nil, err
	}
	if found.Attempted, err = p.jobAttempts(ctx, id); err != nil {
		return nil, err
	}
	return found, nil
}

// ListJob returns what matches, newest first, without answers.
//
// Without answers because a listing of a hundred answers is a listing nobody can read. A caller that
// wants an answer asks for one job.
func (p *Postgres) ListJobs(ctx context.Context, filter job.Filter) ([]*job.Job, error) {
	query := `select ` + jobColumns + ` from jobs where 1 = 1`
	args := []any{}
	add := func(clause string, value any) {
		args = append(args, value)
		query += fmt.Sprintf(clause, len(args))
	}
	switch {
	case filter.Project != "":
		add(` and project = $%d`, filter.Project)
	case filter.Workspace != "":
		add(` and workspace = $%d`, filter.Workspace)
	}
	switch {
	case filter.Parent != "":
		add(` and parent = $%d`, filter.Parent)
	case filter.Root:
		query += ` and parent is null`
	}
	if filter.Phase != "" {
		add(` and phase = $%d`, filter.Phase)
	}
	if filter.LabelKey != "" {
		// The function rather than the ? operator, because a question mark in a statement sent with
		// numbered parameters is read as a placeholder by more than one thing on the way.
		if filter.LabelValue == "" {
			add(` and jsonb_exists(labels, $%d)`, filter.LabelKey)
		} else {
			add(` and labels @> $%d`, labelJSON(filter.LabelKey, filter.LabelValue))
		}
	}
	// The window carries the order with it. A caller asking what finished lately is asking about the
	// moment a job ended, and finished_at is not in step with created_at: a job declared this morning
	// can finish after one declared last week. The index is on finished_at desc, so this read is the
	// one the index was added for.
	if filter.FinishedSince != nil {
		add(` and finished_at >= $%d`, *filter.FinishedSince)
		query += ` order by finished_at desc, id desc`
	} else {
		query += ` order by created_at desc, id desc`
	}
	if filter.Limit > 0 {
		add(` limit $%d`, filter.Limit)
	}

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list job: %w", err)
	}
	defer rows.Close()

	listed := make([]*job.Job, 0)
	for rows.Next() {
		found, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("list job: %w", err)
		}
		found.Answer = ""
		listed = append(listed, found)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list job: %w", err)
	}
	return listed, nil
}

// StopJob halts job that has not ended, keeping the reason, and writes the record of the stop in
// the same transaction.
//
// Job that already ended is refused rather than overwritten: how it ended is the useful part, and a
// second stop would erase it.
func (p *Postgres) StopJob(ctx context.Context, id, reason string, event *job.Event) (*job.Job, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("stop job: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		update jobs set phase = $2, reason = $3, lease_owner = '', lease_until = null,
			finished_at = now(), updated_at = now()
		where id = $1 and phase in ($4, $5, $6, $7)`,
		id, job.PhaseStopped, reason,
		job.PhasePending, job.PhaseWaiting, job.PhaseRunning, job.PhaseAsking)
	if err != nil {
		return nil, fmt.Errorf("stop job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		found, err := p.GetJob(ctx, id)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("store: job %s is %s, and a job that already ended is not stopped again", id, found.Phase)
	}
	if err := appendJobEvent(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("stop job: %w", err)
	}
	return p.GetJob(ctx, id)
}

// AskJob puts a running job's question on the record and stops it there.
//
// The condition on the phase is in the same statement as the write, so a session asking about a job
// that has already ended writes nothing. The hold goes with it: nobody is holding an asking job,
// because there is nothing to come back for until a person answers.
//
// What it was last told is cleared, so the question on the row and the answer beside it are always
// about the same decision.
func (p *Postgres) AskJob(ctx context.Context, id, question string, event *job.Event) (*job.Job, error) {
	return p.moveJob(ctx, id, "ask job", job.ErrNotRunning, []*job.Event{event}, `
		update jobs set phase = $2, question = $3, told = '', resuming = '', lease_owner = '',
			lease_until = null, updated_at = now()
		where id = $1 and phase = $4`,
		job.PhaseAsking, question, job.PhaseRunning)
}

// AnswerJob writes what a person decided and puts the job back to pending, so a controller starts it
// again and hands the answer to the session that asked.
//
// Conditional on the asking phase in the same statement, so two people answering at once leave one
// answer and one task rather than two of each.
func (p *Postgres) AnswerJob(ctx context.Context, id, answer string, event *job.Event) (*job.Job, error) {
	// The start goes with the attempt that is over, so the moment on the row is the moment the
	// attempt carrying the answer began. The attempt that asked is on the record.
	return p.moveJob(ctx, id, "answer job", job.ErrNotAsking, []*job.Event{event}, `
		update jobs set phase = $2, told = $3, started_at = null, updated_at = now()
		where id = $1 and phase = $4`,
		job.PhasePending, answer, job.PhaseAsking)
}

// ProposeJobPlan writes the plan the crew wrote and puts the question about it to a person, in one
// movement.
//
// One statement, so a reader never finds a job asking with no plan on it, and never a plan on a
// running row that nobody was asked to approve. Conditional on the running phase for the reason
// asking is: a job nothing is running has nobody to write a plan.
func (p *Postgres) ProposeJobPlan(ctx context.Context, id, plan, question string,
	event *job.Event) (*job.Job, error) {
	return p.moveJob(ctx, id, "propose job plan", job.ErrNotRunning, []*job.Event{event}, `
		update jobs set phase = $2, plan = $3, question = $4, told = '', resuming = '', lease_owner = '',
			lease_until = null, updated_at = now()
		where id = $1 and phase = $5`,
		job.PhaseAsking, plan, question, job.PhaseRunning)
}

// ApproveJobPlan records that a person approved the plan and puts the job back to pending, so a
// controller starts the work against it.
//
// Conditional on the asking phase and on the plan not being approved yet, in the same statement, so
// two people approving at once leave one approval and one task.
func (p *Postgres) ApproveJobPlan(ctx context.Context, id string, event *job.Event) (*job.Job, error) {
	return p.moveJob(ctx, id, "approve job plan", job.ErrNotAsking, []*job.Event{event}, `
		update jobs set phase = $2, told = '', plan_approved = true, started_at = null, updated_at = now()
		where id = $1 and phase = $3 and plan_approved = false`,
		job.PhasePending, job.PhaseAsking)
}

// ListJobEvents returns one job's own history, in the order it was written.

// By the sequence rather than by the moment. Two records written in one transaction are stamped in
// the same microsecond, and an order broken by a random identifier is an order that reads back wrong
// about once in a few runs: "claimed" after "started", which is a controller that appears to have
// worked backwards.
func (p *Postgres) ListJobEvents(ctx context.Context, id string) ([]*job.Event, error) {
	rows, err := p.pool.Query(ctx, `
		select id, kind, job, workspace, project, parent, depth, detail, trace_id, occurred_at
		from job_events where job = $1 order by seq`, id)
	if err != nil {
		return nil, fmt.Errorf("list job events: %w", err)
	}
	defer rows.Close()

	events := make([]*job.Event, 0)
	for rows.Next() {
		var event job.Event
		if err := rows.Scan(&event.ID, &event.Kind, &event.Job, &event.Workspace, &event.Project,
			&event.Parent, &event.Depth, &event.Detail, &event.TraceID, &event.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan job event: %w", err)
		}
		events = append(events, &event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list job events: %w", err)
	}
	return events, nil
}

// appendJobEvent writes one record inside the transaction that carries the change it describes. The
// same event written twice leaves one row, which is what the primary key is for.
func appendJobEvent(ctx context.Context, tx pgx.Tx, event *job.Event) error {
	if event == nil {
		return nil
	}
	if event.ID == "" || event.Kind == "" {
		return errors.New("store: a job event needs an id and a kind")
	}
	if _, err := tx.Exec(ctx, `
		insert into job_events (id, kind, job, workspace, project, parent, depth, detail, trace_id, occurred_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		on conflict (id) do nothing`,
		event.ID, event.Kind, event.Job, event.Workspace, event.Project,
		event.Parent, event.Depth, event.Detail, event.TraceID, event.OccurredAt); err != nil {
		return fmt.Errorf("append job event: %w", err)
	}
	return nil
}

// rowScanner is what QueryRow and Rows both offer, so one function reads a job from either.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (*job.Job, error) {
	var found job.Job
	var labels []byte
	if err := row.Scan(&found.ID, &found.Workspace, &found.Project, &found.Title, &found.Brief,
		&found.Role, &found.RoleVersion, &found.Mode, &found.ExpectFile, &found.ExpectContains,
		&found.After, &found.Deadline, &found.BudgetTokens, &labels, &found.Requires, &found.Parent, &found.Depth,
		&found.Version, &found.Phase, &found.Session, &found.Attempts, &found.Answer, &found.Reason,
		&found.Question, &found.Told, &found.Resuming, &found.SpentTokens, &found.ObservedVersion,
		&found.LeaseOwner, &found.LeaseUntil, &found.TraceID, &found.ParentSpanID,
		&found.Repository, &found.PullRequest, &found.Product, &found.Steers, &found.Claim,
		&found.Escalation, &found.LoopedStep, &found.EscalatedTo, &found.Plan, &found.PlanApproved,
		&found.Ungated, &found.Reviewed, &found.Tested,
		&found.CreatedAt, &found.UpdatedAt, &found.StartedAt, &found.FinishedAt); err != nil {
		return nil, err
	}
	if len(labels) > 0 {
		if err := json.Unmarshal(labels, &found.Labels); err != nil {
			return nil, fmt.Errorf("read labels: %w", err)
		}
	}
	if len(found.Labels) == 0 {
		found.Labels = nil
	}
	if len(found.After) == 0 {
		found.After = nil
	}
	if len(found.Requires) == 0 {
		found.Requires = nil
	}
	return &found, nil
}

// labelJSON builds the one pair a listing matches on, as text so the parameter is read as jsonb
// rather than as bytes. The value is a label, held to 63 characters at the write, so it cannot fail
// to encode.
func labelJSON(key, value string) string {
	encoded, _ := json.Marshal(map[string]string{key: value})
	return string(encoded)
}

func labelsOrEmpty(labels map[string]string) map[string]string {
	if labels == nil {
		return map[string]string{}
	}
	return labels
}

func afterOrEmpty(after []string) []string {
	if after == nil {
		return []string{}
	}
	return after
}

// nullIfEmpty keeps a root's parent null rather than an empty string, so the foreign key holds and
// "job with no parent" is a query rather than a convention.
func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// RunnableJob is the job a controller may start: pending with nothing it waits for, oldest
// declared first.
//
// A job under a parent and a job in a role are both started. A role because the controller runs it as
// that role, and a parent because a flow run declares every step under its own job, so a controller
// that started roots only would leave every step of every automation pending forever. What is still
// left out is job that waits for something, because nothing honours ordering yet. The tree budget is
// enforced for none of these and for a root either, so nothing is honoured less here than anywhere
// else.
func (p *Postgres) RunnableJob(ctx context.Context, limit int) ([]*job.Job, error) {
	return p.jobMatching(ctx, `
		where phase = $1 and cardinality(after_jobs) = 0
		order by created_at, id`, limit, job.PhasePending)
}

// HeldJob is the job this controller is holding, and only this one: another controller's job is
// not this one's to move. Job with no session yet is left out, because there is no task to read back.
func (p *Postgres) HeldJob(ctx context.Context, owner string, limit int) ([]*job.Job, error) {
	return p.jobMatching(ctx, `
		where phase = $1 and session <> '' and lease_owner = $2 and lease_until > now()
		order by created_at, id`, limit, job.PhaseRunning, owner)
}

// ExpiredJob is the job whose holder went away: running, under a lease that has run out or was
// never written.
func (p *Postgres) ExpiredJob(ctx context.Context, limit int) ([]*job.Job, error) {
	return p.jobMatching(ctx, `
		where phase = $1 and (lease_until is null or lease_until <= now())
		order by created_at, id`, limit, job.PhaseRunning)
}

// AnythingMoving says whether any job is running or asking: whether this system is doing anything
// at all.
//
// A probe rather than a count. The controller asks it on every tick and what it needs is a yes or a
// no, so this stops at the first row and costs one lookup on jobs_phase_idx however many finished
// jobs the table holds.
func (p *Postgres) AnythingMoving(ctx context.Context) (bool, error) {
	var moving bool
	if err := p.pool.QueryRow(ctx,
		`select exists (select 1 from jobs where phase = any($1))`,
		[]string{job.PhaseRunning, job.PhaseAsking}).Scan(&moving); err != nil {
		return false, fmt.Errorf("read whether anything is moving: %w", err)
	}
	return moving, nil
}

// TurnedAwayJob is the job the machine had no room for: pending, carrying a reason, oldest declared
// first. Only the system writes a reason on a pending job, and it writes one only when it holds the
// job back, so the reason is the whole condition.
func (p *Postgres) TurnedAwayJob(ctx context.Context, limit int) ([]*job.Job, error) {
	return p.jobMatching(ctx, `
		where phase = $1 and reason <> ''
		order by created_at, id`, limit, job.PhasePending)
}

// jobMatching runs one of the controller's queries, capped.
func (p *Postgres) jobMatching(ctx context.Context, where string, limit int, args ...any) ([]*job.Job, error) {
	query := `select ` + jobColumns + ` from jobs ` + where
	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" limit $%d", len(args))
	}
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read job: %w", err)
	}
	defer rows.Close()

	found := make([]*job.Job, 0)
	for rows.Next() {
		one, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("read job: %w", err)
		}
		found = append(found, one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read job: %w", err)
	}
	return found, nil
}

// StartJob claims one job and records the record of the claim in the same transaction.
//
// The update is conditional on the phase in the same statement, which is the compare and set that
// keeps two controllers from both starting the same job. A row that moved on first is refused, and
// the refusal is not a failure: it means somebody else has it.
func (p *Postgres) StartJob(ctx context.Context, id string, lease job.Lease, events []*job.Event) (*job.Job, error) {
	// The reason is cleared with the same statement that starts the job. It described the pending
	// phase the job is leaving, and a job that was held for want of room and then admitted must not
	// run carrying "there is not enough memory".
	return p.moveJob(ctx, id, "start job", job.ErrNotPending, events, `
		update jobs set phase = $2, attempts = attempts + 1, lease_owner = $3, lease_until = $4,
			reason = '', started_at = now(), updated_at = now()
		where id = $1 and phase = $5`,
		job.PhaseRunning, lease.Owner, lease.Until, job.PhasePending)
}

// HoldJob says on a pending job why it is not being started, without moving it.
//
// The phase stays pending on purpose. The job is still the next thing this system will run, and what
// an operator needs is the difference between waiting its turn and waiting for a machine, which is
// a sentence rather than a phase. It applies only to a pending job, so a hold can never overwrite
// how a job ended.
func (p *Postgres) HoldJob(ctx context.Context, id, reason string, event *job.Event) (*job.Job, error) {
	events := []*job.Event{}
	if event != nil {
		events = append(events, event)
	}
	return p.moveJob(ctx, id, "hold job", job.ErrNotPending, events, `
		update jobs set reason = $2, updated_at = now()
		where id = $1 and phase = $3`,
		reason, job.PhasePending)
}

// TakeOverJob takes the lease on a job whose holder went away.
//
// The condition on the lease is in the same statement as the write, so two controllers finding the
// same abandoned row leave one holder. That is the compare and set a log cannot give.
func (p *Postgres) TakeOverJob(ctx context.Context, id string, lease job.Lease, events []*job.Event) (*job.Job, error) {
	return p.moveJob(ctx, id, "take over job", job.ErrHeld, events, `
		update jobs set lease_owner = $2, lease_until = $3, updated_at = now()
		where id = $1 and phase = $4 and (lease_until is null or lease_until <= now())`,
		lease.Owner, lease.Until, job.PhaseRunning)
}

// ReleaseJob puts job back to pending. Only running job with no session under a lease that has
// run out, which is the one state that says for certain no task was ever sent.
func (p *Postgres) ReleaseJob(ctx context.Context, id string, events []*job.Event) (*job.Job, error) {
	return p.moveJob(ctx, id, "release job", job.ErrHeld, events, `
		update jobs set phase = $2, lease_owner = '', lease_until = null,
			started_at = null, updated_at = now()
		where id = $1 and phase = $3 and session = '' and (lease_until is null or lease_until <= now())`,
		job.PhasePending, job.PhaseRunning)
}

// RequeueJob puts a running job back to pending because the system could not start it.
//
// The condition on the lease owner is in the same statement as the write, so a controller that lost
// the row cannot put another controller's job back under it.
func (p *Postgres) RequeueJob(ctx context.Context, id string, back job.Requeue, events []*job.Event) (*job.Job, error) {
	return p.moveJob(ctx, id, "requeue job", job.ErrHeld, events, `
		update jobs set phase = $2, reason = $3, lease_owner = '', lease_until = null,
			started_at = null, updated_at = now()
		where id = $1 and phase = $4 and lease_owner = $5`,
		job.PhasePending, back.Reason, job.PhaseRunning, back.Owner)
}

// RenewLease moves the holder's hold on. Only the holder renews, so a controller that lost a row
// cannot take it back by renewing.
func (p *Postgres) RenewLease(ctx context.Context, id string, lease job.Lease) error {
	tag, err := p.pool.Exec(ctx, `
		update jobs set lease_until = $3, updated_at = now()
		where id = $1 and lease_owner = $2`, id, lease.Owner, lease.Until)
	if err != nil {
		return fmt.Errorf("renew lease: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := p.GetJob(ctx, id); err != nil {
			return err
		}
		return job.ErrHeld
	}
	return nil
}

// moveJob runs one conditional movement and the records that describe it, in one transaction.
//
// The condition lives in the statement rather than in a read before it, which is what makes two
// controllers racing over one row leave one winner. A statement that changed nothing means the row
// was not in the state this movement is for, and that is refused rather than reported as a failure.
func (p *Postgres) moveJob(ctx context.Context, id, what string, refusal error,
	events []*job.Event, statement string, args ...any) (*job.Job, error) {
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
		if _, err := p.GetJob(ctx, id); err != nil {
			return nil, err
		}
		return nil, refusal
	}
	for _, event := range events {
		if err := appendJobEvent(ctx, tx, event); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return p.GetJob(ctx, id)
}

// RecordJobSession writes which conversation a job runs in. Not a movement, so it writes
// no record of its own.
func (p *Postgres) RecordJobSession(ctx context.Context, id, session string) error {
	tag, err := p.pool.Exec(ctx,
		`update jobs set session = $2, updated_at = now() where id = $1`, id, session)
	if err != nil {
		return fmt.Errorf("record job session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ReplaceJobProduct writes the one sentence a job serves over what it carried, with its record, in
// one transaction.
//
// Held to no phase. The job this is called on is the one carrying a flow run, which is held back
// while its steps work, so a condition on the running phase would refuse the only case there is.
func (p *Postgres) ReplaceJobProduct(ctx context.Context, id, product string, event *job.Event) (*job.Job, error) {
	return p.moveJob(ctx, id, "replace the sentence a job serves", ErrNotFound, []*job.Event{event}, `
		update jobs set product = $2, version = version + 1, updated_at = now()
		where id = $1`,
		job.TidySentence(product))
}

// LandJob writes what came of the job, with its record, in one transaction. Conditional on the
// job still running, so what it ended as is written once.
func (p *Postgres) LandJob(ctx context.Context, id string, landed job.Landing, event *job.Event) (*job.Job, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("land job: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		update jobs set phase = $2, answer = $3, reason = $4, spent_tokens = $5,
			-- What a landing read off the answer, unless it read none and the row already carries one. A
			-- step that named the pull request wrote it before the answer landed, and a job that failed
			-- carries no answer at all, so an unconditional write here would erase the address.
			pull_request = case when $7 <> '' then $7 else pull_request end,
			-- What read this work before it settled, so a settled job says whether anything independent
			-- agreed with its answer rather than leaving a reader to open two conversations.
			reviewed = $8, tested = $9,
			observed_version = version, lease_owner = '', lease_until = null,
			finished_at = now(), updated_at = now()
		where id = $1 and phase = $6`,
		id, landed.Phase, landed.Answer, landed.Reason, landed.SpentTokens, job.PhaseRunning,
		landed.PullRequest, landed.Reviewed, landed.Tested)
	if err != nil {
		return nil, fmt.Errorf("land job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := p.GetJob(ctx, id); err != nil {
			return nil, err
		}
		return nil, job.ErrNotRunning
	}
	// What the attempt said, in the same transaction as what came of it. A landing with no attempt
	// behind it would leave the record unable to say whether this job was going anywhere.
	if err := insertJobAttempt(ctx, tx, id, landed.Attempt); err != nil {
		return nil, err
	}
	if err := appendJobEvent(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("land job: %w", err)
	}
	return p.GetJob(ctx, id)
}

// digestColumns is the row a history reads, in one place beside jobColumns so the two cannot drift.
//
// It is deliberately short. A history never selects the brief, the answer or the steps, which is the
// difference between a read a session can afford and one that fills its context before it starts.
const digestColumns = `id, project, title, role, phase, spent_tokens, pull_request, reason, steers,
	created_at, started_at, finished_at`

// JobHistory returns every job declared inside a window, as digests, newest first.
//
// Bounded by the window rather than by a limit, because the caller adds these up before it cuts them
// down: a total taken over a page would be a number that is wrong in the one way a reader cannot see.
func (p *Postgres) JobHistory(ctx context.Context, query job.HistoryQuery) ([]*job.Digest, error) {
	statement := `select ` + digestColumns + ` from jobs where created_at >= $1 and created_at < $2`
	args := []any{query.Window.Since, query.Window.Until}
	switch {
	case query.Project != "":
		args = append(args, query.Project)
		statement += fmt.Sprintf(` and project = $%d`, len(args))
	case query.Workspace != "":
		args = append(args, query.Workspace)
		statement += fmt.Sprintf(` and workspace = $%d`, len(args))
	}
	statement += ` order by created_at desc, id desc`

	rows, err := p.pool.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("job history: %w", err)
	}
	defer rows.Close()

	history := make([]*job.Digest, 0)
	for rows.Next() {
		one, err := scanDigest(rows)
		if err != nil {
			return nil, fmt.Errorf("job history: %w", err)
		}
		history = append(history, one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("job history: %w", err)
	}
	return history, nil
}

// scanDigest reads one history row. The two moments a job may not have yet are nullable, and an
// absent one stays the zero time, which is what the arithmetic reads as "not known".
func scanDigest(row rowScanner) (*job.Digest, error) {
	var one job.Digest
	var started, finished *time.Time
	if err := row.Scan(&one.ID, &one.Project, &one.Title, &one.Role, &one.Phase, &one.SpentToken,
		&one.PullRequest, &one.Reason, &one.Steers, &one.CreatedAt, &started, &finished); err != nil {
		return nil, err
	}
	if started != nil {
		one.StartedAt = started.UTC()
	}
	if finished != nil {
		one.FinishedAt = finished.UTC()
	}
	one.CreatedAt = one.CreatedAt.UTC()
	return &one, nil
}
