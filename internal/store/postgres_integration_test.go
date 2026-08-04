//go:build integration

package store_test

import (
	"context"
	"fmt"
	"os"
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
	if _, err := pool.Exec(ctx, `truncate sessions, channels, workspaces restart identity cascade`); err != nil {
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
	same, err := migrated.FindOrCreateSession(ctx, adopted[0].GetId(), "thread-1")
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
