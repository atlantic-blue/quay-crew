//go:build integration

package store_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/atlantic-blue/quay-crew/internal/store/storetest"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
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
	project, err := first.CreateProject(ctx, "acme")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	first.Close()

	// A second open runs Migrate again against the same database.
	second, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatalf("second open ran the migrations again and failed: %v", err)
	}
	t.Cleanup(second.Close)

	if _, err := second.GetProject(ctx, project.GetId()); err != nil {
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
	if _, err := pool.Exec(ctx, `truncate sessions, channels, projects restart identity cascade`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}
