//go:build integration

package store_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"errors"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/atlantic-blue/quay-crew/internal/store/storetest"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"path/filepath"
	"strings"
)

// databaseURL addresses the Postgres container shared by every test in this file.
var databaseURL string

// TestMain starts one real Postgres for the package. Each subtest truncates first, so they cannot
// observe each other's rows.
func TestMain(m *testing.M) {
	code, err := runWithPostgres(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func runWithPostgres(m *testing.M) (int, error) {
	ctx := context.Background()

	// An existing database wins, so this suite can run against a local Postgres on a machine with no
	// Docker daemon. Continuous integration leaves it unset and gets the container below.
	if existing := os.Getenv("QC_TEST_DATABASE_URL"); existing != "" {
		databaseURL = existing
		return m.Run(), nil
	}

	container, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("quaycrew"),
		postgres.WithUsername("quaycrew"),
		postgres.WithPassword("quaycrew"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return 0, fmt.Errorf("start: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = container.Terminate(ctx)
	}()

	databaseURL, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return 0, fmt.Errorf("connection string: %w", err)
	}
	return m.Run(), nil
}

// TestPostgresConformance runs the whole store contract against a real database. Each subtest gets a
// clean set of tables, and "reopening" opens a genuinely new connection pool, which is what proves a
// session outlives the process that created it.
func TestPostgresConformance(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) storetest.Opener {
		truncate(t)
		return func(t *testing.T) store.Store {
			s, err := store.NewPostgres(context.Background(), databaseURL)
			if err != nil {
				t.Fatalf("open postgres: %v", err)
			}
			t.Cleanup(s.Close)
			return s
		}
	})
}

// TestMigrationsAreIdempotent proves running the migrations again on a database that already has
// them is a no op, because the control plane applies them on every start.
func TestMigrationsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	truncate(t)

	first, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	workspace, err := first.CreateWorkspace(ctx, "acme")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	first.Close()

	// A second open runs Migrate again against the same database.
	second, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatalf("second open ran the migrations again and failed: %v", err)
	}
	t.Cleanup(second.Close)

	if _, err := second.GetWorkspace(ctx, workspace.GetId()); err != nil {
		t.Fatalf("rerunning the migrations lost the data: %v", err)
	}
}

// truncate empties the tables so one subtest cannot see another's rows. It tolerates the tables not
// existing yet, which is the case before the very first migration runs.
func truncate(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to truncate: %v", err)
	}
	defer pool.Close()

	var exists bool
	if err := pool.QueryRow(ctx,
		`select exists (select 1 from information_schema.tables where table_name = 'sessions')`,
	).Scan(&exists); err != nil {
		t.Fatalf("check for tables: %v", err)
	}
	if !exists {
		return
	}
	// Skills, hooks and contexts are named here rather than left to the cascade. Nothing references
	// them from workspaces, so they survived a truncate that claimed to leave nothing behind: a skill
	// is keyed by its own name, so one subtest's github was still there for the next one to attach, at
	// a version it never imported. The cascade then takes skill_secrets, skill_files and
	// workspace_skills, and hook_events, hook_secrets, hook_files, workspace_hooks and crew_hooks.
	//
	// Hooks landed here the same way and cost the same hour: the memory store gives every subtest a
	// fresh map and passed, while against real Postgres the first attach in a subtest came back at
	// version 2 from rows the previous subtest had left behind.
	if _, err := pool.Exec(ctx,
		// Turns are named here for the same reason as skills. A turn is keyed by its own id and
		// survived a truncate that claimed to leave nothing behind, so one subtest's turn-0 was still
		// there when the next one wrote its own, and AppendTurn's "on conflict do nothing" dropped it
		// silently. What that looked like was a case reading zero turns it had just written.
		`truncate sessions, turns, channels, workspaces, skills, hooks, contexts, flow_graphs restart identity cascade`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// TestRenameMigrationKeepsExistingRows applies the original schema by hand, seeds it the way a
// database in use would look, and then lets the store migrate. Everything must still be there under
// the new names, above all a session's model_session_id, which is the only pointer to a conversation
// the model holds on its own disk. Losing it in a rename would orphan every live conversation.
func TestRenameMigrationKeepsExistingRows(t *testing.T) {
	ctx := context.Background()
	dropEverything(t)

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	original, err := os.ReadFile("migrations/0001_init.up.sql")
	if err != nil {
		pool.Close()
		t.Fatalf("read the original migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(original)); err != nil {
		pool.Close()
		t.Fatalf("apply the original schema: %v", err)
	}
	// Record it as applied, so the store migrates from here rather than starting over.
	if _, err := pool.Exec(ctx, `
		create table if not exists schema_migrations (
			version text primary key, applied_at timestamptz not null default now())`); err != nil {
		pool.Close()
		t.Fatalf("create schema_migrations: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`insert into schema_migrations (version) values ('0001_init') on conflict do nothing`); err != nil {
		pool.Close()
		t.Fatalf("record the original migration: %v", err)
	}

	// A workspace and a session as they looked under the old names.
	if _, err := pool.Exec(ctx,
		`insert into projects (id, name) values ('ws-1', 'me')`); err != nil {
		pool.Close()
		t.Fatalf("seed the project: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into sessions (id, project, thread_id, status, model_session_id)
		values ('sess-1', 'ws-1', 'thread-1', 'idle', 'conversation-1')`); err != nil {
		pool.Close()
		t.Fatalf("seed the session: %v", err)
	}
	pool.Close()

	// Opening the store runs the rename.
	migrated, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(migrated.Close)

	workspace, err := migrated.GetWorkspace(ctx, "ws-1")
	if err != nil {
		t.Fatalf("the workspace did not survive the rename: %v", err)
	}
	if workspace.GetName() != "me" {
		t.Fatalf("the workspace is named %q, want me", workspace.GetName())
	}

	session, err := migrated.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("the session did not survive the rename: %v", err)
	}
	if session.GetWorkspace() != "ws-1" {
		t.Fatalf("the session belongs to %q, want ws-1", session.GetWorkspace())
	}
	if session.GetModelSessionId() != "conversation-1" {
		t.Fatalf("the conversation handle did not survive: %q", session.GetModelSessionId())
	}

	// Sessions predate projects, so the migration gives each workspace one to adopt what it already
	// had. Without that a session would belong to no project and be unreachable.
	adopted, err := migrated.ListProjects(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListProjects after migrating: %v", err)
	}
	if len(adopted) != 1 {
		t.Fatalf("the workspace has %d projects after migrating, want the one that adopts its sessions", len(adopted))
	}
	if session.GetProject() != adopted[0].GetId() {
		t.Fatalf("the session belongs to project %q, want the adopting project %q", session.GetProject(), adopted[0].GetId())
	}

	// The thread must still resolve to the same session, or the next turn starts a new conversation.
	same, err := migrated.FindOrCreateSession(ctx, adopted[0].GetId(), "thread-1", "")
	if err != nil {
		t.Fatalf("FindOrCreateSession after the rename: %v", err)
	}
	if same.GetId() != "sess-1" {
		t.Fatalf("the thread made a new session after the rename: %q", same.GetId())
	}
	if same.GetModelSessionId() != "conversation-1" {
		t.Fatalf("the adopted session lost its conversation handle: %q", same.GetModelSessionId())
	}
}

// dropEverything returns the database to empty, so a migration test starts from nothing.
func dropEverything(t *testing.T) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect to drop: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(context.Background(),
		`drop table if exists sessions, channels, projects, workspaces, schema_migrations cascade`); err != nil {
		t.Fatalf("drop: %v", err)
	}
}

// TestTheSubscriptionTokenSurvivesARestart is the whole point of keeping secrets in the database.
//
// Every restart of the stack lost the token, so the next turn failed with nothing useful to say and
// the operator had to mint and set one again before anything worked. This closes a second store over
// the same database, which is what a restarted control plane is from the outside.
func TestTheSubscriptionTokenSurvivesARestart(t *testing.T) {
	ctx := context.Background()
	truncate(t)

	key, err := secrets.KeyAt(filepath.Join(t.TempDir(), "secrets.key"))
	if err != nil {
		t.Fatalf("KeyAt: %v", err)
	}

	before, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	kept, err := secrets.NewPostgres(before.Pool(), key)
	if err != nil {
		t.Fatalf("open secrets: %v", err)
	}
	const token = "sk-ant-oat-not-a-real-one"
	if err := kept.Set(ctx, "acme", "CLAUDE_CODE_OAUTH_TOKEN", token); err != nil {
		t.Fatalf("Set: %v", err)
	}
	before.Close()

	after, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatalf("reopen postgres: %v", err)
	}
	t.Cleanup(after.Close)
	reopened, err := secrets.NewPostgres(after.Pool(), key)
	if err != nil {
		t.Fatalf("reopen secrets: %v", err)
	}

	got, err := reopened.Get(ctx, "acme", "CLAUDE_CODE_OAUTH_TOKEN")
	if err != nil {
		t.Fatalf("the token did not survive: %v", err)
	}
	if got != token {
		t.Fatalf("the token came back as %q", got)
	}
	if _, err := reopened.Get(ctx, "acme", "never-set"); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("a secret that was never set returned %v, want ErrNotFound", err)
	}

	// And it is not sitting in the clear in a table anybody might dump.
	var sealed []byte
	if err := after.Pool().QueryRow(ctx,
		`select sealed from secrets where workspace = 'acme'`).Scan(&sealed); err != nil {
		t.Fatalf("reading the row: %v", err)
	}
	if strings.Contains(string(sealed), token) {
		t.Fatal("the token is in the database in the clear")
	}
}

// A database that lived through the repository machinery still migrates clean. The remote column
// (0010) and the workspace_repositories table (0011) both carried operator data at some point, and
// 0013 removes the machinery: what has to hold is that the journey runs end to end over that data,
// and that nothing of it is left in the schema afterwards, because a table nothing writes is a
// question every later migration has to answer.
func TestTheRepositoryMachineryIsGoneFromTheSchema(t *testing.T) {
	ctx := context.Background()
	dropEverything(t)

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	// Bring the schema up to the old shape, so the migrations that remove it really have data under
	// them rather than empty tables.
	applyThrough(t, ctx, pool, "0010_project_remote")

	if _, err := pool.Exec(ctx, `
		insert into workspaces (id, name) values ('w1', 'acme');
		insert into projects (id, workspace, name, remote)
		values ('p1', 'w1', 'house bills', 'https://github.com/atlantic-blue/quay-crew.git'),
		       ('p2', 'w1', 'gardening', 'https://github.com/atlantic-blue/quay-crew.git'),
		       ('p3', 'w1', 'nothing here', '')`); err != nil {
		t.Fatalf("seed the old shape: %v", err)
	}

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate over the old shape: %v", err)
	}

	var tableThere bool
	if err := pool.QueryRow(ctx, `
		select exists (select 1 from information_schema.tables
		               where table_name = 'workspace_repositories')`).Scan(&tableThere); err != nil {
		t.Fatalf("check the old table: %v", err)
	}
	if tableThere {
		t.Error("workspace_repositories still exists, so the machinery was removed from the code and left in the schema")
	}

	var columnThere bool
	if err := pool.QueryRow(ctx, `
		select exists (select 1 from information_schema.columns
		               where table_name = 'projects' and column_name = 'remote')`).Scan(&columnThere); err != nil {
		t.Fatalf("check the old column: %v", err)
	}
	if columnThere {
		t.Error("projects still has a remote column")
	}

	// The projects themselves survive the journey: only the machinery goes.
	var projects int
	if err := pool.QueryRow(ctx, `select count(*) from projects`).Scan(&projects); err != nil {
		t.Fatalf("count the projects: %v", err)
	}
	if projects != 3 {
		t.Errorf("%d projects survived the migrations, want 3", projects)
	}
}

// applyThrough runs the migrations in order up to and including one of them, and records them as
// applied, so a test can stand on the schema as it was at a particular commit.
func applyThrough(t *testing.T, ctx context.Context, pool *pgxpool.Pool, last string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		create table if not exists schema_migrations (
			version text primary key, applied_at timestamptz not null default now())`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	entries, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read the migrations: %v", err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		version := strings.TrimSuffix(name, ".up.sql")
		body, err := os.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx,
			`insert into schema_migrations (version) values ($1) on conflict do nothing`, version); err != nil {
			t.Fatalf("record %s: %v", version, err)
		}
		if version == last {
			return
		}
	}
	t.Fatalf("no migration called %s", last)
}
