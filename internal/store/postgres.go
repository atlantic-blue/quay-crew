package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Postgres is the durable Store. Everything the control plane knows lives here, so the process holds
// nothing that a restart would lose.
type Postgres struct {
	pool *pgxpool.Pool
}

var _ Store = (*Postgres)(nil)

// NewPostgres connects, applies the migrations, and returns the store. The caller closes it.
func NewPostgres(ctx context.Context, databaseURL string) (*Postgres, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	// Every query the control plane makes is short. A connection that cannot be had quickly is a
	// failed turn, not a turn that hangs.
	config.ConnConfig.ConnectTimeout = 10 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

// Close releases the connection pool.
func (p *Postgres) Close() { p.pool.Close() }

// CreateWorkspace inserts a workspace.
func (p *Postgres) CreateWorkspace(ctx context.Context, name string) (*quaycrewv1.Workspace, error) {
	var (
		id        = NewID()
		createdAt time.Time
	)
	err := p.pool.QueryRow(ctx,
		`insert into workspaces (id, name) values ($1, $2) returning created_at`, id, name,
	).Scan(&createdAt)
	if err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}
	return &quaycrewv1.Workspace{Id: id, Name: name, CreatedAt: timestamppb.New(createdAt)}, nil
}

// GetWorkspace returns a workspace that has not been deleted.
func (p *Postgres) GetWorkspace(ctx context.Context, id string) (*quaycrewv1.Workspace, error) {
	var (
		name      string
		createdAt time.Time
	)
	err := p.pool.QueryRow(ctx,
		`select name, created_at from workspaces where id = $1 and deleted_at is null`, id,
	).Scan(&name, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get workspace: %w", err)
	}
	return &quaycrewv1.Workspace{Id: id, Name: name, CreatedAt: timestamppb.New(createdAt)}, nil
}

// ListWorkspaces returns every workspace that has not been deleted, newest first.
func (p *Postgres) ListWorkspaces(ctx context.Context) ([]*quaycrewv1.Workspace, error) {
	rows, err := p.pool.Query(ctx,
		`select id, name, created_at from workspaces where deleted_at is null order by created_at desc, id`)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()

	out := make([]*quaycrewv1.Workspace, 0)
	for rows.Next() {
		var (
			id, name  string
			createdAt time.Time
		)
		if err := rows.Scan(&id, &name, &createdAt); err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		out = append(out, &quaycrewv1.Workspace{Id: id, Name: name, CreatedAt: timestamppb.New(createdAt)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	return out, nil
}

// DeleteWorkspace soft deletes a workspace, leaving its sessions intact.
func (p *Postgres) DeleteWorkspace(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx,
		`update workspaces set deleted_at = now(), updated_at = now() where id = $1 and deleted_at is null`, id)
	if err != nil {
		return fmt.Errorf("delete workspace: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AttachChannel records a channel against a live workspace.
func (p *Postgres) AttachChannel(ctx context.Context, workspace, id, kind string) (*quaycrewv1.Channel, error) {
	if _, err := p.GetWorkspace(ctx, workspace); err != nil {
		return nil, err
	}
	_, err := p.pool.Exec(ctx, `
		insert into channels (id, workspace, kind) values ($1, $2, $3)
		on conflict (workspace, id) do update set kind = excluded.kind, updated_at = now()`,
		id, workspace, kind)
	if err != nil {
		return nil, fmt.Errorf("attach channel: %w", err)
	}
	return &quaycrewv1.Channel{Workspace: workspace, Id: id, Kind: kind}, nil
}

// CreateProject adds a body of work to a live workspace.
func (p *Postgres) CreateProject(ctx context.Context, workspace, name string) (*quaycrewv1.Project, error) {
	if _, err := p.GetWorkspace(ctx, workspace); err != nil {
		return nil, err
	}
	var (
		id        = NewID()
		createdAt time.Time
	)
	err := p.pool.QueryRow(ctx,
		`insert into projects (id, workspace, name) values ($1, $2, $3) returning created_at`,
		id, workspace, name,
	).Scan(&createdAt)
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return &quaycrewv1.Project{Id: id, Workspace: workspace, Name: name, CreatedAt: timestamppb.New(createdAt)}, nil
}

// GetProject returns a live project whose workspace is also live.
func (p *Postgres) GetProject(ctx context.Context, id string) (*quaycrewv1.Project, error) {
	var (
		workspace, name string
		createdAt       time.Time
	)
	// The join is what stops a project outliving the workspace it belongs to.
	err := p.pool.QueryRow(ctx, `
		select p.workspace, p.name, p.created_at
		from projects p join workspaces w on w.id = p.workspace
		where p.id = $1 and p.deleted_at is null and w.deleted_at is null`, id,
	).Scan(&workspace, &name, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	return &quaycrewv1.Project{
		Id: id, Workspace: workspace, Name: name, CreatedAt: timestamppb.New(createdAt),
	}, nil
}

// ListProjects returns live projects, filtered to one workspace when set, newest first.
func (p *Postgres) ListProjects(ctx context.Context, workspace string) ([]*quaycrewv1.Project, error) {
	rows, err := p.pool.Query(ctx, `
		select p.id, p.workspace, p.name, p.created_at
		from projects p join workspaces w on w.id = p.workspace
		where p.deleted_at is null and w.deleted_at is null and ($1 = '' or p.workspace = $1)
		order by p.created_at desc, p.id`, workspace)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	out := make([]*quaycrewv1.Project, 0)
	for rows.Next() {
		var (
			id, owner, name string
			createdAt       time.Time
		)
		if err := rows.Scan(&id, &owner, &name, &createdAt); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		out = append(out, &quaycrewv1.Project{
			Id: id, Workspace: owner, Name: name, CreatedAt: timestamppb.New(createdAt),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return out, nil
}

// DeleteProject soft deletes a project, leaving its sessions intact.
func (p *Postgres) DeleteProject(ctx context.Context, id string) error {
	if _, err := p.GetProject(ctx, id); err != nil {
		return err
	}
	if _, err := p.pool.Exec(ctx,
		`update projects set deleted_at = now(), updated_at = now() where id = $1 and deleted_at is null`, id); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

// FindOrCreateSession returns the project's session for a thread, creating it on first use.
//
// The insert races with any other caller dispatching to the same thread, so it defers to the unique
// constraint on (workspace, thread_id) and reads the winner back rather than trusting a prior select.
func (p *Postgres) FindOrCreateSession(ctx context.Context, project, thread, bornIn string) (*quaycrewv1.Thread, error) {
	owner, err := p.GetProject(ctx, project)
	if err != nil {
		return nil, err
	}
	// The mode is written here rather than left to the column's default, because the default is one
	// value for every crew and this is the crew's own choice.
	if _, err := p.pool.Exec(ctx, `
		insert into sessions (id, workspace, project, thread_id, status, permission_mode)
		values ($1, $2, $3, $4, 'idle', $5)
		on conflict (project, thread_id) do nothing`,
		NewID(), owner.GetWorkspace(), project, thread, model.PermissionModeBornIn(bornIn)); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return p.sessionBy(ctx, `project = $1 and thread_id = $2`, project, thread)
}

// RecordTurn stores the model conversation handle and status after a turn. An empty handle leaves
// the stored one alone, so a failed turn cannot erase the pointer to a live conversation.
func (p *Postgres) RecordTurn(ctx context.Context, id, modelSessionID, status string) error {
	tag, err := p.pool.Exec(ctx, `
		update sessions
		set model_session_id = case when $2 = '' then model_session_id else $2 end,
		    status = $3,
		    updated_at = now()
		where id = $1`,
		id, modelSessionID, status)
	if err != nil {
		return fmt.Errorf("record turn: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetSession returns a session by id.
func (p *Postgres) GetSession(ctx context.Context, id string) (*quaycrewv1.Thread, error) {
	return p.sessionBy(ctx, `id = $1`, id)
}

// ListSessions returns sessions, filtered to one project when set, else to one workspace when set.
func (p *Postgres) ListSessions(ctx context.Context, filter SessionFilter) ([]*quaycrewv1.Thread, error) {
	rows, err := p.pool.Query(ctx, `
		select id, workspace, project, thread_id, status, model_session_id, created_at, updated_at, archived_at, permission_mode, driver
		from sessions
		where ($2 = '' or project = $2)
		  and ($2 <> '' or $1 = '' or workspace = $1)
		  and ((archived_at is not null) = $3)
		order by created_at desc, id`, filter.Workspace, filter.Project, filter.Archived)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	out := make([]*quaycrewv1.Thread, 0)
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return out, nil
}

// StopSession marks a session stopped.
func (p *Postgres) StopSession(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx,
		`update sessions set status = 'stopped', skills_fingerprint = '', updated_at = now() where id = $1`, id)
	if err != nil {
		return fmt.Errorf("stop session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetSessionSkills records the skill set a session's live sandbox was born with; empty clears it.
func (p *Postgres) SetSessionSkills(ctx context.Context, id, fingerprint string) error {
	tag, err := p.pool.Exec(ctx,
		`update sessions set skills_fingerprint = $2, updated_at = now() where id = $1`, id, fingerprint)
	if err != nil {
		return fmt.Errorf("set session skills: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SessionSkills reads what skill set the session's live sandbox was born with, empty when no live
// sandbox is known.
func (p *Postgres) SessionSkills(ctx context.Context, id string) (string, error) {
	var fingerprint string
	err := p.pool.QueryRow(ctx,
		`select skills_fingerprint from sessions where id = $1`, id).Scan(&fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("session skills: %w", err)
	}
	return fingerprint, nil
}

// RestartSession marks a session idle again. The conversation handle is left exactly as it was: it
// is the only pointer to a conversation the model keeps on its own disk.
func (p *Postgres) RestartSession(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx,
		`update sessions set status = 'idle', updated_at = now() where id = $1`, id)
	if err != nil {
		return fmt.Errorf("restart session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPermissionMode records what a thread's turns may do without asking.
func (p *Postgres) SetPermissionMode(ctx context.Context, id, mode string) error {
	tag, err := p.pool.Exec(ctx,
		`update sessions set permission_mode = $2, updated_at = now() where id = $1`, id, mode)
	if err != nil {
		return fmt.Errorf("set permission mode: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ArchiveSession stamps a session as put away. Nothing is deleted, which is the whole point: the row,
// the conversation handle and the files on the host are all untouched, so restoring is one update.
func (p *Postgres) ArchiveSession(ctx context.Context, id string) error {
	return p.stampArchived(ctx, id, `archived_at = now(), skills_fingerprint = ''`)
}

// RestoreSession clears the stamp, bringing the thread back into the default listing.
func (p *Postgres) RestoreSession(ctx context.Context, id string) error {
	return p.stampArchived(ctx, id, `archived_at = null`)
}

// stampArchived is the one update both of those are. The clause is a constant from the two callers
// above and never carries a value, so nothing here is built from input.
func (p *Postgres) stampArchived(ctx context.Context, id, clause string) error {
	tag, err := p.pool.Exec(ctx,
		`update sessions set `+clause+`, updated_at = now() where id = $1`, id)
	if err != nil {
		return fmt.Errorf("archive session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Pool is the connection this store holds, so another durable thing can sit beside it rather than
// opening a second connection to the same database for the same crew.
func (p *Postgres) Pool() *pgxpool.Pool { return p.pool }

// GetContext returns what the model should be told at a scope. Nothing written is the normal state
// and comes back empty rather than as an error.
func (p *Postgres) GetContext(ctx context.Context, scope ContextScope, owner string) (string, error) {
	var body string
	err := p.pool.QueryRow(ctx,
		`select body from contexts where scope = $1 and owner = $2`, string(scope), owner).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get context: %w", err)
	}
	return body, nil
}

// SetContext records what the model should be told at a scope.
func (p *Postgres) SetContext(ctx context.Context, scope ContextScope, owner, body string) error {
	_, err := p.pool.Exec(ctx, `
		insert into contexts (scope, owner, body) values ($1, $2, $3)
		on conflict (scope, owner) do update set body = excluded.body, updated_at = now()`,
		string(scope), owner, body)
	if err != nil {
		return fmt.Errorf("set context: %w", err)
	}
	return nil
}

// sessionBy reads the single session matching a where clause.
func (p *Postgres) sessionBy(ctx context.Context, where string, args ...any) (*quaycrewv1.Thread, error) {
	rows, err := p.pool.Query(ctx, `
		select id, workspace, project, thread_id, status, model_session_id, created_at, updated_at, archived_at, permission_mode, driver
		from sessions where `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("get session: %w", err)
		}
		return nil, ErrNotFound
	}
	return scanSession(rows)
}

func scanSession(rows pgx.Rows) (*quaycrewv1.Thread, error) {
	var (
		id, workspace, project, thread, status, modelSessionID string
		createdAt, updatedAt                                   time.Time
		archivedAt                                             *time.Time
		permissionMode                                         string
		driver                                                 bool
	)
	if err := rows.Scan(&id, &workspace, &project, &thread, &status, &modelSessionID,
		&createdAt, &updatedAt, &archivedAt, &permissionMode, &driver); err != nil {
		return nil, fmt.Errorf("scan session: %w", err)
	}
	session := &quaycrewv1.Thread{
		Id:             id,
		Workspace:      workspace,
		Project:        project,
		Handle:         thread,
		Status:         status,
		ModelSessionId: modelSessionID,
		CreatedAt:      timestamppb.New(createdAt),
		UpdatedAt:      timestamppb.New(updatedAt),
		PermissionMode: permissionMode,
		Driver:         driver,
	}
	if archivedAt != nil {
		session.ArchivedAt = timestamppb.New(*archivedAt)
	}
	return session, nil
}

// AppendTurn records a turn. Writing the same one twice is harmless, which is what makes a consumer
// with at least once delivery safe to replay.
func (p *Postgres) AppendTurn(ctx context.Context, turn *quaycrewv1.Turn, workspace, project, thread string) error {
	if turn.GetId() == "" {
		return errors.New("store: a turn needs an id, so writing the same one twice leaves one turn")
	}
	_, err := p.pool.Exec(ctx, `
		insert into turns (id, session, workspace, project, thread_id, prompt, reply, status, failure, occurred_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		on conflict (id) do nothing`,
		turn.GetId(), turn.GetThread(), workspace, project, thread,
		turn.GetPrompt(), turn.GetReply(), turn.GetStatus(), turn.GetFailure(), turn.GetOccurredAt().AsTime())
	if err != nil {
		return fmt.Errorf("append turn: %w", err)
	}
	return nil
}

// ListTurns returns a session's turns oldest first, capped at limit.
//
// The cap takes the most recent, because the end of a conversation is the part somebody is looking
// for, and then the result is turned back the right way round so it reads as it happened.
func (p *Postgres) ListTurns(ctx context.Context, session string, limit int) ([]*quaycrewv1.Turn, error) {
	rows, err := p.pool.Query(ctx, `
		select id, session, prompt, reply, status, failure, occurred_at
		from turns where session = $1
		order by occurred_at desc, id desc
		limit $2`, session, TurnLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list turns: %w", err)
	}
	defer rows.Close()

	turns := make([]*quaycrewv1.Turn, 0)
	for rows.Next() {
		var turn quaycrewv1.Turn
		var occurredAt time.Time
		if err := rows.Scan(&turn.Id, &turn.Thread, &turn.Prompt, &turn.Reply,
			&turn.Status, &turn.Failure, &occurredAt); err != nil {
			return nil, fmt.Errorf("scan turn: %w", err)
		}
		turn.OccurredAt = timestamppb.New(occurredAt)
		turns = append(turns, &turn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list turns: %w", err)
	}
	// Read back newest first so the limit keeps the end of the conversation; hand it over oldest
	// first so it reads in the order it happened.
	for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
		turns[i], turns[j] = turns[j], turns[i]
	}
	return turns, nil
}

// FindOrCreateDriver returns the project's driver, the session that drives the crew, creating it the
// first time somebody opens it.
//
// One per project, held by a unique index rather than by reading first and writing after: two opened
// at once would otherwise each make one, and the second would be reached by nobody.
func (p *Postgres) FindOrCreateDriver(ctx context.Context, project string) (*quaycrewv1.Thread, error) {
	owner, err := p.GetProject(ctx, project)
	if err != nil {
		return nil, err
	}
	// Created dangerous. The driver acts for the operator rather than doing work of its own, and a
	// driver that stops to ask before every step describes the task instead of doing it. What bounds
	// it is the sandbox, which is the same boundary it would have either way.
	if _, err := p.pool.Exec(ctx, `
		insert into sessions (id, workspace, project, thread_id, status, driver, permission_mode)
		values ($1, $2, $3, $4, 'idle', true, $5)
		on conflict do nothing`,
		NewID(), owner.GetWorkspace(), project, NewID(), model.PermissionBypass); err != nil {
		return nil, fmt.Errorf("open the driver: %w", err)
	}
	rows, err := p.pool.Query(ctx, `
		select id, workspace, project, thread_id, status, model_session_id, created_at, updated_at, archived_at, permission_mode, driver
		from sessions where project = $1 and driver`, project)
	if err != nil {
		return nil, fmt.Errorf("open the driver: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrNotFound
	}
	return scanSession(rows)
}
