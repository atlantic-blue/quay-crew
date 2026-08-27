package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/atlantic-blue/quay-crew/internal/work"
	"github.com/jackc/pgx/v5"
)

// workColumns is the row every read of a piece of work selects, in one place so a reader and a
// listing cannot drift into scanning different things.
const workColumns = `id, workspace, project, title, brief, role, role_version, mode, expect_file,
	expect_contains, after_work, deadline, budget_tokens, labels, coalesce(parent, ''), depth, version,
	phase, session, attempts, answer, reason, question, spent_tokens, observed_version,
	lease_owner, lease_until, created_at, updated_at, started_at, finished_at`

// CreateWork writes a piece of work and the record of its declaration in one transaction.
//
// One transaction because the store is the source of truth here. A row with no record of how it came
// to exist, or a record of a declaration that is not there, are both states nothing can explain
// afterwards.
func (p *Postgres) CreateWork(ctx context.Context, declared *work.Work, event *work.Event) error {
	labels, err := json.Marshal(labelsOrEmpty(declared.Labels))
	if err != nil {
		return fmt.Errorf("create work: %w", err)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("create work: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The whole record, status fields included, because the store keeps what it is handed. Writing
	// only the declared half would leave the two stores disagreeing about the same call, which is how
	// a double that accepts more than the real thing manufactures a green suite.
	if _, err := tx.Exec(ctx, `
		insert into work (id, workspace, project, title, brief, role, role_version, mode, expect_file,
			expect_contains, after_work, deadline, budget_tokens, labels, parent, depth, version, phase,
			session, attempts, answer, reason, question, spent_tokens, observed_version, started_at, finished_at,
			lease_owner, lease_until)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18,
			$19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29)`,
		declared.ID, declared.Workspace, declared.Project, declared.Title, declared.Brief,
		declared.Role, declared.RoleVersion, declared.Mode, declared.ExpectFile, declared.ExpectContains,
		afterOrEmpty(declared.After), declared.Deadline, declared.BudgetTokens, string(labels),
		nullIfEmpty(declared.Parent), declared.Depth, declared.Version, declared.Phase,
		declared.Session, declared.Attempts, declared.Answer, declared.Reason, declared.Question,
		declared.SpentTokens, declared.ObservedVersion, declared.StartedAt, declared.FinishedAt,
		declared.LeaseOwner, declared.LeaseUntil); err != nil {
		return fmt.Errorf("create work: %w", err)
	}
	if err := appendWorkEvent(ctx, tx, event); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("create work: %w", err)
	}
	return nil
}

// GetWork reads one piece of work back, whole, answer included.
func (p *Postgres) GetWork(ctx context.Context, id string) (*work.Work, error) {
	row := p.pool.QueryRow(ctx, `select `+workColumns+` from work where id = $1`, id)
	found, err := scanWork(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get work: %w", err)
	}
	return found, nil
}

// ListWork returns what matches, newest first, without answers.
//
// Without answers because a listing of a hundred answers is a listing nobody can read. A caller that
// wants an answer asks for one piece of work.
func (p *Postgres) ListWork(ctx context.Context, filter work.Filter) ([]*work.Work, error) {
	query := `select ` + workColumns + ` from work where 1 = 1`
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
	query += ` order by created_at desc, id desc`

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list work: %w", err)
	}
	defer rows.Close()

	listed := make([]*work.Work, 0)
	for rows.Next() {
		found, err := scanWork(rows)
		if err != nil {
			return nil, fmt.Errorf("list work: %w", err)
		}
		found.Answer = ""
		listed = append(listed, found)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list work: %w", err)
	}
	return listed, nil
}

// StopWork halts work that has not ended, keeping the reason, and writes the record of the stop in
// the same transaction.
//
// Work that already ended is refused rather than overwritten: how it ended is the useful part, and a
// second stop would erase it.
func (p *Postgres) StopWork(ctx context.Context, id, reason string, event *work.Event) (*work.Work, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("stop work: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		update work set phase = $2, reason = $3, lease_owner = '', lease_until = null,
			finished_at = now(), updated_at = now()
		where id = $1 and phase in ($4, $5, $6, $7)`,
		id, work.PhaseStopped, reason,
		work.PhasePending, work.PhaseWaiting, work.PhaseRunning, work.PhaseAsking)
	if err != nil {
		return nil, fmt.Errorf("stop work: %w", err)
	}
	if tag.RowsAffected() == 0 {
		found, err := p.GetWork(ctx, id)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("store: work %s is %s, and work that already ended is not stopped again", id, found.Phase)
	}
	if err := appendWorkEvent(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("stop work: %w", err)
	}
	return p.GetWork(ctx, id)
}

// ListWorkEvents returns one piece of work's own history, oldest first.
func (p *Postgres) ListWorkEvents(ctx context.Context, id string) ([]*work.Event, error) {
	rows, err := p.pool.Query(ctx, `
		select id, kind, work, workspace, project, parent, depth, detail, occurred_at
		from work_events where work = $1 order by occurred_at, id`, id)
	if err != nil {
		return nil, fmt.Errorf("list work events: %w", err)
	}
	defer rows.Close()

	events := make([]*work.Event, 0)
	for rows.Next() {
		var event work.Event
		if err := rows.Scan(&event.ID, &event.Kind, &event.Work, &event.Workspace, &event.Project,
			&event.Parent, &event.Depth, &event.Detail, &event.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan work event: %w", err)
		}
		events = append(events, &event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list work events: %w", err)
	}
	return events, nil
}

// appendWorkEvent writes one record inside the transaction that carries the change it describes. The
// same event written twice leaves one row, which is what the primary key is for.
func appendWorkEvent(ctx context.Context, tx pgx.Tx, event *work.Event) error {
	if event == nil {
		return nil
	}
	if event.ID == "" || event.Kind == "" {
		return errors.New("store: a work event needs an id and a kind")
	}
	if _, err := tx.Exec(ctx, `
		insert into work_events (id, kind, work, workspace, project, parent, depth, detail, occurred_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		on conflict (id) do nothing`,
		event.ID, event.Kind, event.Work, event.Workspace, event.Project,
		event.Parent, event.Depth, event.Detail, event.OccurredAt); err != nil {
		return fmt.Errorf("append work event: %w", err)
	}
	return nil
}

// rowScanner is what QueryRow and Rows both offer, so one function reads a piece of work from either.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanWork(row rowScanner) (*work.Work, error) {
	var found work.Work
	var labels []byte
	if err := row.Scan(&found.ID, &found.Workspace, &found.Project, &found.Title, &found.Brief,
		&found.Role, &found.RoleVersion, &found.Mode, &found.ExpectFile, &found.ExpectContains,
		&found.After, &found.Deadline, &found.BudgetTokens, &labels, &found.Parent, &found.Depth,
		&found.Version, &found.Phase, &found.Session, &found.Attempts, &found.Answer, &found.Reason,
		&found.Question, &found.SpentTokens, &found.ObservedVersion,
		&found.LeaseOwner, &found.LeaseUntil,
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
// "work with no parent" is a query rather than a convention.
func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// RunnableWork is the work a controller may start: pending, with no parent, no role and nothing it
// waits for, oldest declared first.
//
// The shape is deliberately narrow. Work that waits for something, work in a role and work under a
// parent are each a later slice, and offering them to a controller that honours none of those would
// run them with their ordering, their boundary and their budget ignored.
func (p *Postgres) RunnableWork(ctx context.Context, limit int) ([]*work.Work, error) {
	return p.workMatching(ctx, `
		where phase = $1 and parent is null and role = '' and cardinality(after_work) = 0
		order by created_at, id`, limit, work.PhasePending)
}

// HeldWork is the work this controller is holding, and only this one: another controller's work is
// not this one's to move. Work with no session yet is left out, because there is no task to read back.
func (p *Postgres) HeldWork(ctx context.Context, owner string, limit int) ([]*work.Work, error) {
	return p.workMatching(ctx, `
		where phase = $1 and session <> '' and lease_owner = $2 and lease_until > now()
		order by created_at, id`, limit, work.PhaseRunning, owner)
}

// ExpiredWork is the work whose holder went away: running, under a lease that has run out or was
// never written.
func (p *Postgres) ExpiredWork(ctx context.Context, limit int) ([]*work.Work, error) {
	return p.workMatching(ctx, `
		where phase = $1 and (lease_until is null or lease_until <= now())
		order by created_at, id`, limit, work.PhaseRunning)
}

// workMatching runs one of the controller's queries, capped.
func (p *Postgres) workMatching(ctx context.Context, where string, limit int, args ...any) ([]*work.Work, error) {
	query := `select ` + workColumns + ` from work ` + where
	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" limit $%d", len(args))
	}
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read work: %w", err)
	}
	defer rows.Close()

	found := make([]*work.Work, 0)
	for rows.Next() {
		one, err := scanWork(rows)
		if err != nil {
			return nil, fmt.Errorf("read work: %w", err)
		}
		found = append(found, one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read work: %w", err)
	}
	return found, nil
}

// StartWork claims one piece of work and records the record of the claim in the same transaction.
//
// The update is conditional on the phase in the same statement, which is the compare and set that
// keeps two controllers from both starting the same work. A row that moved on first is refused, and
// the refusal is not a failure: it means somebody else has it.
func (p *Postgres) StartWork(ctx context.Context, id string, lease work.Lease, events []*work.Event) (*work.Work, error) {
	return p.moveWork(ctx, id, "start work", work.ErrNotPending, events, `
		update work set phase = $2, attempts = attempts + 1, lease_owner = $3, lease_until = $4,
			started_at = now(), updated_at = now()
		where id = $1 and phase = $5`,
		work.PhaseRunning, lease.Owner, lease.Until, work.PhasePending)
}

// TakeOverWork takes the lease on work whose holder went away.
//
// The condition on the lease is in the same statement as the write, so two controllers finding the
// same abandoned row leave one holder. That is the compare and set a log cannot give.
func (p *Postgres) TakeOverWork(ctx context.Context, id string, lease work.Lease, events []*work.Event) (*work.Work, error) {
	return p.moveWork(ctx, id, "take over work", work.ErrHeld, events, `
		update work set lease_owner = $2, lease_until = $3, updated_at = now()
		where id = $1 and phase = $4 and (lease_until is null or lease_until <= now())`,
		lease.Owner, lease.Until, work.PhaseRunning)
}

// ReleaseWork puts work back to pending. Only running work with no session under a lease that has
// run out, which is the one state that says for certain no task was ever sent.
func (p *Postgres) ReleaseWork(ctx context.Context, id string, events []*work.Event) (*work.Work, error) {
	return p.moveWork(ctx, id, "release work", work.ErrHeld, events, `
		update work set phase = $2, lease_owner = '', lease_until = null,
			started_at = null, updated_at = now()
		where id = $1 and phase = $3 and session = '' and (lease_until is null or lease_until <= now())`,
		work.PhasePending, work.PhaseRunning)
}

// RenewLease moves the holder's hold on. Only the holder renews, so a controller that lost a row
// cannot take it back by renewing.
func (p *Postgres) RenewLease(ctx context.Context, id string, lease work.Lease) error {
	tag, err := p.pool.Exec(ctx, `
		update work set lease_until = $3, updated_at = now()
		where id = $1 and lease_owner = $2`, id, lease.Owner, lease.Until)
	if err != nil {
		return fmt.Errorf("renew lease: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := p.GetWork(ctx, id); err != nil {
			return err
		}
		return work.ErrHeld
	}
	return nil
}

// moveWork runs one conditional movement and the records that describe it, in one transaction.
//
// The condition lives in the statement rather than in a read before it, which is what makes two
// controllers racing over one row leave one winner. A statement that changed nothing means the row
// was not in the state this movement is for, and that is refused rather than reported as a failure.
func (p *Postgres) moveWork(ctx context.Context, id, what string, refusal error,
	events []*work.Event, statement string, args ...any) (*work.Work, error) {
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
		if _, err := p.GetWork(ctx, id); err != nil {
			return nil, err
		}
		return nil, refusal
	}
	for _, event := range events {
		if err := appendWorkEvent(ctx, tx, event); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return p.GetWork(ctx, id)
}

// RecordWorkSession writes which conversation a piece of work runs in. Not a movement, so it writes
// no record of its own.
func (p *Postgres) RecordWorkSession(ctx context.Context, id, session string) error {
	tag, err := p.pool.Exec(ctx,
		`update work set session = $2, updated_at = now() where id = $1`, id, session)
	if err != nil {
		return fmt.Errorf("record work session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// LandWork writes what came of the work, with its record, in one transaction. Conditional on the
// work still running, so what it ended as is written once.
func (p *Postgres) LandWork(ctx context.Context, id string, landed work.Landing, event *work.Event) (*work.Work, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("land work: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		update work set phase = $2, answer = $3, reason = $4, spent_tokens = $5,
			observed_version = version, lease_owner = '', lease_until = null,
			finished_at = now(), updated_at = now()
		where id = $1 and phase = $6`,
		id, landed.Phase, landed.Answer, landed.Reason, landed.SpentTokens, work.PhaseRunning)
	if err != nil {
		return nil, fmt.Errorf("land work: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := p.GetWork(ctx, id); err != nil {
			return nil, err
		}
		return nil, work.ErrNotRunning
	}
	if err := appendWorkEvent(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("land work: %w", err)
	}
	return p.GetWork(ctx, id)
}
