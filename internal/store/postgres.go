package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/deploy"
	"github.com/atlantic-blue/quay-krewe/internal/model"
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
	// failed task, not a task that hangs.
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

// Probe writes one row and writes over it next time, so a caller can prove the store still takes a
// write. It takes a connection from the pool the way every other write does, which is the half of
// the path a read cannot speak for: a pool with nothing left to hand out answers no write at all.
func (p *Postgres) Probe(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx,
		`insert into health_probe (id, written_at) values (1, now())
		 on conflict (id) do update set written_at = now()`); err != nil {
		return fmt.Errorf("store: probe write: %w", err)
	}
	return nil
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
		workspace, name           string
		account, region, identity string
		repository, visibility    string
		createdAt                 time.Time
	)
	// The join is what stops a project outliving the workspace it belongs to.
	err := p.pool.QueryRow(ctx, `
		select p.workspace, p.name, p.created_at, p.deploy_account, p.deploy_region, p.deploy_identity,
		       p.repository, p.visibility
		from projects p join workspaces w on w.id = p.workspace
		where p.id = $1 and p.deleted_at is null and w.deleted_at is null`, id,
	).Scan(&workspace, &name, &createdAt, &account, &region, &identity, &repository, &visibility)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	return &quaycrewv1.Project{
		Id: id, Workspace: workspace, Name: name, CreatedAt: timestamppb.New(createdAt),
		DeployTarget: deployTarget(account, region, identity),
		Repository:   repository, Visibility: visibility,
	}, nil
}

// SetProjectRepository records where a project's work lands, and what kind of repository it is.
//
// The row is read first, through GetProject, so a project whose workspace has been deleted is not
// found rather than updated: a project outliving its workspace is the case that join exists for.
func (p *Postgres) SetProjectRepository(ctx context.Context, project, repository, visibility string) (*quaycrewv1.Project, error) {
	if _, err := p.GetProject(ctx, project); err != nil {
		return nil, err
	}
	if _, err := p.pool.Exec(ctx, `
		update projects set repository = $2, visibility = $3, updated_at = now()
		where id = $1 and deleted_at is null`, project, repository, visibility); err != nil {
		return nil, fmt.Errorf("set project repository: %w", err)
	}
	return p.GetProject(ctx, project)
}

// ListProjects returns live projects, filtered to one workspace when set, newest first.
func (p *Postgres) ListProjects(ctx context.Context, workspace string) ([]*quaycrewv1.Project, error) {
	rows, err := p.pool.Query(ctx, `
		select p.id, p.workspace, p.name, p.created_at, p.deploy_account, p.deploy_region, p.deploy_identity,
		       p.repository, p.visibility
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
			id, owner, name           string
			account, region, identity string
			repository, visibility    string
			createdAt                 time.Time
		)
		if err := rows.Scan(&id, &owner, &name, &createdAt, &account, &region, &identity,
			&repository, &visibility); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		out = append(out, &quaycrewv1.Project{
			Id: id, Workspace: owner, Name: name, CreatedAt: timestamppb.New(createdAt),
			DeployTarget: deployTarget(account, region, identity),
			Repository:   repository, Visibility: visibility,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return out, nil
}

// SetDeployTarget records where a project ships, and a zero target clears it.
func (p *Postgres) SetDeployTarget(ctx context.Context, project string, target deploy.Target) error {
	if _, err := p.GetProject(ctx, project); err != nil {
		return err
	}
	if _, err := p.pool.Exec(ctx, `
		update projects
		set deploy_account = $2, deploy_region = $3, deploy_identity = $4, updated_at = now()
		where id = $1 and deleted_at is null`,
		project, target.Account, target.Region, target.Identity); err != nil {
		return fmt.Errorf("set deploy target: %w", err)
	}
	return nil
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

// FindOrCreateSession returns the project's session for a session, creating it on first use.
//
// The insert races with any other caller dispatching to the same session, so it defers to the unique
// constraint on (workspace, handle) and reads the winner back rather than trusting a prior select.
func (p *Postgres) FindOrCreateSession(ctx context.Context, project, session string, born Birth) (*quaycrewv1.Session, bool, error) {
	owner, err := p.GetProject(ctx, project)
	if err != nil {
		return nil, false, err
	}
	// The mode is written here rather than left to the column's default, because the default is one
	// value for every system and this is the system's own choice.
	//
	// Whether a row was written is read from the insert rather than by looking first: two callers
	// racing for one handle would both find nothing and both call it a creation, and the session
	// would be announced twice.
	tag, err := p.pool.Exec(ctx, `
		insert into sessions (id, workspace, project, handle, status, permission_mode, role, title)
		values ($1, $2, $3, $4, 'idle', $5, $6, $7)
		on conflict (project, handle) do nothing`,
		NewID(), owner.GetWorkspace(), project, session, model.PermissionModeBornIn(born.Mode),
		born.Role, born.Title)
	if err != nil {
		return nil, false, fmt.Errorf("create session: %w", err)
	}
	found, err := p.sessionBy(ctx, `project = $1 and handle = $2`, project, session)
	if err != nil {
		return nil, false, err
	}
	return found, tag.RowsAffected() == 1, nil
}

// RecordTask stores the model conversation handle and status after a task. An empty handle leaves
// the stored one alone, so a failed task cannot erase the pointer to a live conversation.
func (p *Postgres) RecordTask(ctx context.Context, id, modelSessionID, status string) error {
	tag, err := p.pool.Exec(ctx, `
		update sessions
		set model_session_id = case when $2 = '' then model_session_id else $2 end,
		    status = $3,
		    -- A task is running or has landed, so the session holds a container again and the stamp
		    -- that said the system took the last one back is no longer true. Left behind, the archive
		    -- rule would go on measuring against a reclaim that a dispatch already undid.
		    reclaimed_at = null,
		    updated_at = now()
		where id = $1`,
		id, modelSessionID, status)
	if err != nil {
		return fmt.Errorf("record task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetSession returns a session by id.
func (p *Postgres) GetSession(ctx context.Context, id string) (*quaycrewv1.Session, error) {
	return p.sessionBy(ctx, `id = $1`, id)
}

// sessionColumns is every field of a session, in the order scanSession reads them. One list rather
// than four copies of it: a column added to the row and forgotten in one of the four reads is a
// session that scans in three places and fails in the fourth.
const sessionColumns = `id, workspace, project, handle, status, model_session_id, created_at, ` +
	`updated_at, archived_at, reclaimed_at, permission_mode, driver, label, description, ` +
	`described_at_task, role, title`

// ListSessions returns sessions, filtered to one project when set, else to one workspace when set,
// last moved first: see sortByLastMoved for the order and why it is that one.
func (p *Postgres) ListSessions(ctx context.Context, filter SessionFilter) ([]*quaycrewv1.Session, error) {
	rows, err := p.pool.Query(ctx, `
		select `+sessionColumns+`
		from sessions
		where ($2 = '' or project = $2)
		  and ($2 <> '' or $1 = '' or workspace = $1)
		  and ((archived_at is not null) = $3)
		-- The same stamp the age column shows, so the column reads in order. An archived session is
		-- measured from when it was put away, a live one from when it was last touched, and the
		-- identifier breaks a tie. sortByLastMoved is this rule in Go, and storetest holds the two to it.
		order by coalesce(archived_at, updated_at) desc, id`, filter.Workspace, filter.Project, filter.Archived)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	out := make([]*quaycrewv1.Session, 0)
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
		`update sessions set status = 'stopped', skills_fingerprint = '', reclaimed_at = null,
		 updated_at = now() where id = $1`, id)
	if err != nil {
		return fmt.Errorf("stop session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ReclaimSession records that the system took the session's container back.
//
// The stamp is written beside the status rather than read off updated_at, because the archive time is
// measured against how long the session has been reclaimed, and updated_at moves on every write.
//
// The skills fingerprint goes with the container, the same way stopping clears it: the next sandbox
// is born with the workspace's current set, so a reclaimed session is never stale.
func (p *Postgres) ReclaimSession(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx,
		`update sessions set status = 'reclaimed', skills_fingerprint = '', reclaimed_at = now(),
		 updated_at = now() where id = $1`, id)
	if err != nil {
		return fmt.Errorf("reclaim session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// IdleSandboxes is the sessions that still hold a container and nothing is holding open, oldest
// touched first.
//
// Live, not running, not already reclaimed, and named by no job in a non terminal phase. A session
// with a task under way is not settled, and neither is one whose job is still open even though its
// own task has landed: the job is what says the session is wanted, and the controller is about to
// send it another task.
//
// Sessions an operator stopped are left out. A stop is somebody's decision, and filing away what
// somebody halted would overwrite it with bookkeeping.
//
// Reclaimed rows are left out too, and that split is the fault this closes. They stay settled for
// ever where no archive time is set, and their updated_at is the moment they were reclaimed, so they
// sort ahead of a sandbox that has been idle for an hour. One batch of twenty then holds twenty rows
// nothing can move, and the reclaim never reaches a container again. See issue 575.
func (p *Postgres) IdleSandboxes(ctx context.Context, limit int) ([]*quaycrewv1.Session, error) {
	return p.settled(ctx, `
		  and s.status = any($1)
		  and not exists (
		      select 1 from jobs w where w.session = s.id and not (w.phase = any($2))
		  )
		  and not exists (
		      select 1 from executions x where x.session = s.id and not (x.phase = any($2))
		  )
		order by s.updated_at, s.id`, limit, holdingStatuses(), terminalPhases())
}

// ReclaimedSessions is the sessions whose container has already gone, longest reclaimed first.
//
// Ordered by reclaimed_at rather than updated_at, because that is what the archive time is measured
// against. A reclaim writes both stamps and only one of them says how long the session has been in
// this state.
func (p *Postgres) ReclaimedSessions(ctx context.Context, limit int) ([]*quaycrewv1.Session, error) {
	return p.settled(ctx, `
		  and s.status = $1
		  and not exists (
		      select 1 from jobs w where w.session = s.id and not (w.phase = any($2))
		  )
		  and not exists (
		      select 1 from executions x where x.session = s.id and not (x.phase = any($2))
		  )
		order by s.reclaimed_at nulls first, s.id`, limit, StatusReclaimed, terminalPhases())
}

// settled runs one of the two queries above. Both read the same rows through the same index and
// differ only in which statuses they take and what they order by.
func (p *Postgres) settled(ctx context.Context, where string, limit int, args ...any) (
	[]*quaycrewv1.Session, error) {
	query := `select ` + sessionColumns + ` from sessions s where s.archived_at is null` + where
	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" limit $%d", len(args))
	}
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("settled sessions: %w", err)
	}
	defer rows.Close()

	out := make([]*quaycrewv1.Session, 0)
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settled sessions: %w", err)
	}
	return out, nil
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
		`update sessions set status = 'idle', reclaimed_at = null, updated_at = now() where id = $1`, id)
	if err != nil {
		return fmt.Errorf("restart session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPermissionMode records what a session's tasks may do without asking.
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

// SetLabel records what the operator calls a session. Empty clears it.
func (p *Postgres) SetLabel(ctx context.Context, id, label string) error {
	tag, err := p.pool.Exec(ctx,
		`update sessions set label = $2, updated_at = now() where id = $1`, id, label)
	if err != nil {
		return fmt.Errorf("set label: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetDescription records what the system observed a session to be, with the task count it was written
// at, in one statement so the two can never disagree about how current it is.
func (p *Postgres) SetDescription(ctx context.Context, id, description string, atTask int) error {
	tag, err := p.pool.Exec(ctx,
		`update sessions set description = $2, described_at_task = $3, updated_at = now() where id = $1`,
		id, description, atTask)
	if err != nil {
		return fmt.Errorf("set description: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CountTasks is how many tasks a session has had.
func (p *Postgres) CountTasks(ctx context.Context, session string) (int, error) {
	var count int
	if err := p.pool.QueryRow(ctx,
		`select count(*) from tasks where session = $1`, session).Scan(&count); err != nil {
		return 0, fmt.Errorf("count tasks: %w", err)
	}
	return count, nil
}

// ArchiveSession stamps a session as put away. Nothing is deleted, which is the whole point: the row,
// the conversation handle and the files on the host are all untouched, so restoring is one update.
func (p *Postgres) ArchiveSession(ctx context.Context, id string) error {
	return p.stampArchived(ctx, id, `archived_at = now(), skills_fingerprint = ''`)
}

// RestoreSession clears the stamp, bringing the session back into the default listing.
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
// opening a second connection to the same database for the same system.
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
func (p *Postgres) sessionBy(ctx context.Context, where string, args ...any) (*quaycrewv1.Session, error) {
	rows, err := p.pool.Query(ctx, `
		select `+sessionColumns+`
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

func scanSession(rows pgx.Rows) (*quaycrewv1.Session, error) {
	var (
		id, workspace, project, handle, status, modelSessionID string
		createdAt, updatedAt                                   time.Time
		archivedAt, reclaimedAt                                *time.Time
		permissionMode                                         string
		driver                                                 bool
		label, description, roleName, title                    string
		describedAtTask                                        int32
	)
	if err := rows.Scan(&id, &workspace, &project, &handle, &status, &modelSessionID,
		&createdAt, &updatedAt, &archivedAt, &reclaimedAt, &permissionMode, &driver, &label,
		&description, &describedAtTask, &roleName, &title); err != nil {
		return nil, fmt.Errorf("scan session: %w", err)
	}
	session := &quaycrewv1.Session{
		Id:              id,
		Workspace:       workspace,
		Project:         project,
		Handle:          handle,
		Status:          status,
		ModelSessionId:  modelSessionID,
		CreatedAt:       timestamppb.New(createdAt),
		UpdatedAt:       timestamppb.New(updatedAt),
		PermissionMode:  permissionMode,
		Driver:          driver,
		Label:           label,
		Description:     description,
		DescribedAtTask: describedAtTask,
		Role:            roleName,
		Title:           title,
	}
	if archivedAt != nil {
		session.ArchivedAt = timestamppb.New(*archivedAt)
	}
	if reclaimedAt != nil {
		session.ReclaimedAt = timestamppb.New(*reclaimedAt)
	}
	return session, nil
}

// AppendTask records a task. Writing the same one twice is harmless, which is what makes a consumer
// with at least once delivery safe to replay.
func (p *Postgres) AppendTask(ctx context.Context, task *quaycrewv1.Task, workspace, project, session string) error {
	if task.GetId() == "" {
		return errors.New("store: a task needs an id, so writing the same one twice leaves one task")
	}
	_, err := p.pool.Exec(ctx, `
		insert into tasks (id, session, workspace, project, handle, prompt, reply, status, failure,
			trace_id, occurred_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		on conflict (id) do nothing`,
		task.GetId(), task.GetSession(), workspace, project, session,
		task.GetPrompt(), task.GetReply(), task.GetStatus(), task.GetFailure(), task.GetTraceId(),
		task.GetOccurredAt().AsTime())
	if err != nil {
		return fmt.Errorf("append task: %w", err)
	}
	return nil
}

// FinishTask closes the record a task opened when it started. A row that is not there is left
// alone: the task happened whatever the store holds.
func (p *Postgres) FinishTask(ctx context.Context, id, status, reply, failure string) error {
	if id == "" {
		return errors.New("store: a task needs an id to be finished")
	}
	_, err := p.pool.Exec(ctx, `
		update tasks set status = $2, reply = $3, failure = $4
		where id = $1`, id, status, reply, failure)
	if err != nil {
		return fmt.Errorf("finish task: %w", err)
	}
	return nil
}

// ListTasks returns a session's tasks oldest first, capped at limit.
//
// The cap takes the most recent, because the end of a conversation is the part somebody is looking
// for, and then the result is turned back the right way round so it reads as it happened.
func (p *Postgres) ListTasks(ctx context.Context, session string, limit int) ([]*quaycrewv1.Task, error) {
	rows, err := p.pool.Query(ctx, `
		select id, session, prompt, reply, status, failure, trace_id, occurred_at
		from tasks where session = $1
		order by occurred_at desc, id desc
		limit $2`, session, TaskLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]*quaycrewv1.Task, 0)
	for rows.Next() {
		var task quaycrewv1.Task
		var occurredAt time.Time
		if err := rows.Scan(&task.Id, &task.Session, &task.Prompt, &task.Reply,
			&task.Status, &task.Failure, &task.TraceId, &occurredAt); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		task.OccurredAt = timestamppb.New(occurredAt)
		tasks = append(tasks, &task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	// Read back newest first so the limit keeps the end of the conversation; hand it over oldest
	// first so it reads in the order it happened.
	for i, j := 0, len(tasks)-1; i < j; i, j = i+1, j-1 {
		tasks[i], tasks[j] = tasks[j], tasks[i]
	}
	return tasks, nil
}

// AppendSessionEvent records one thing that happened to a session. The same event written twice
// leaves one row, which is what the primary key is for.
func (p *Postgres) AppendSessionEvent(ctx context.Context, event *quaycrewv1.SessionEvent) error {
	if event.GetId() == "" {
		return errors.New("store: a session event needs an id, so writing the same one twice leaves one event")
	}
	if event.GetKind() == "" {
		return errors.New("store: a session event needs a kind, which is the field a consumer switches on")
	}
	_, err := p.pool.Exec(ctx, `
		insert into session_events (id, kind, session, workspace, project, handle, detail, occurred_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
		on conflict (id) do nothing`,
		event.GetId(), event.GetKind(), event.GetSession(), event.GetWorkspace(),
		event.GetProject(), event.GetHandle(), event.GetDetail(), event.GetOccurredAt().AsTime())
	if err != nil {
		return fmt.Errorf("append session event: %w", err)
	}
	return nil
}

// ListSessionEvents returns a session's lifecycle oldest first, or the whole system's when no session
// is named.
//
// Read newest first so the cap keeps the recent end, then turned back the right way round, the same
// as a history.
func (p *Postgres) ListSessionEvents(ctx context.Context, session string, limit int) ([]*quaycrewv1.SessionEvent, error) {
	rows, err := p.pool.Query(ctx, `
		select id, kind, session, workspace, project, handle, detail, occurred_at
		from session_events
		where $1 = '' or session = $1
		order by occurred_at desc, id desc
		limit $2`, session, TaskLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list session events: %w", err)
	}
	defer rows.Close()

	events := make([]*quaycrewv1.SessionEvent, 0)
	for rows.Next() {
		var event quaycrewv1.SessionEvent
		var occurredAt time.Time
		if err := rows.Scan(&event.Id, &event.Kind, &event.Session, &event.Workspace,
			&event.Project, &event.Handle, &event.Detail, &occurredAt); err != nil {
			return nil, fmt.Errorf("scan session event: %w", err)
		}
		event.OccurredAt = timestamppb.New(occurredAt)
		events = append(events, &event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list session events: %w", err)
	}
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return events, nil
}

// FindOrCreateDriver returns the project's driver, the session that drives the system, creating it the
// first time somebody opens it.
//
// One per project, held by a unique index rather than by reading first and writing after: two opened
// at once would otherwise each make one, and the second would be reached by nobody.
func (p *Postgres) FindOrCreateDriver(ctx context.Context, project string) (*quaycrewv1.Session, error) {
	owner, err := p.GetProject(ctx, project)
	if err != nil {
		return nil, err
	}
	// Created dangerous. The driver acts for the operator rather than doing job of its own, and a
	// driver that stops to ask before every step describes the task instead of doing it. What bounds
	// it is the sandbox, which is the same boundary it would have either way.
	if _, err := p.pool.Exec(ctx, `
		insert into sessions (id, workspace, project, handle, status, driver, permission_mode)
		values ($1, $2, $3, $4, 'idle', true, $5)
		on conflict do nothing`,
		NewID(), owner.GetWorkspace(), project, NewID(), model.PermissionBypass); err != nil {
		return nil, fmt.Errorf("open the driver: %w", err)
	}
	rows, err := p.pool.Query(ctx, `
		select `+sessionColumns+`
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

// deployTarget is the three columns as a target, and nothing at all when a project has not said.
//
// A project that has not said carries no target rather than a target of three empty strings, so a
// reader asks one question instead of three.
func deployTarget(account, region, identity string) *quaycrewv1.DeployTarget {
	if account == "" && region == "" && identity == "" {
		return nil
	}
	return &quaycrewv1.DeployTarget{Account: account, Region: region, Identity: identity}
}
