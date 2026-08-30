//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/role"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A role imported before the rename keeps the verbs it was declared with, and comes back out of the
// store under the new name.
//
// This is the one thing a rename of a stored column can get wrong quietly. The code compiles either
// way, every test that writes and then reads passes on a fresh database, and the rows that already
// exist are the only place the mistake shows. So the row here is written through the old schema, in
// the old column, before the migration that renames it exists.
//
// What it costs to get wrong is the whole boundary: a role whose verbs did not survive the store
// grants nothing, its session is refused at its first call, and nothing in the crew says why. That
// is what quay-crew#459 was, arriving through the column being absent rather than renamed.
func TestARoleImportedBeforeTheRenameKeepsTheVerbsItDeclared(t *testing.T) {
	ctx := context.Background()
	dropEverything(t)

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	// The schema as it stood the day before: the column is called may.
	applyThrough(t, ctx, pool, "0040_job_repository")

	var columnThere bool
	if err := pool.QueryRow(ctx, `
		select exists (select 1 from information_schema.columns
		               where table_name = 'roles' and column_name = 'may')`).Scan(&columnThere); err != nil {
		t.Fatalf("check the old column: %v", err)
	}
	if !columnThere {
		t.Fatal("the old schema has no may column, so this test stands on nothing and proves nothing")
	}

	if _, err := pool.Exec(ctx, `
		insert into roles (name, version, summary, model, receives, "may", brief, fingerprint)
		values ('orchestrator', 1, 'declares the children', 'opus', '{job,context}',
		        '{job.create,job.read}', 'Declare the children.', 'f1'),
		       ('releaser', 1, 'releases what it was given', 'sonnet', '{job}',
		        '{}', 'Open the pull request.', 'f2')`); err != nil {
		t.Fatalf("seed the old shape: %v", err)
	}

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate over the old shape: %v", err)
	}

	// Read back through the store rather than through a query of my own, because what has to hold is
	// that the code path a caller uses finds the verbs.
	kept, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(kept.Close)

	granting, err := kept.GetRole(ctx, "orchestrator", 1)
	if err != nil {
		t.Fatalf("GetRole: %v", err)
	}
	for _, verb := range []string{role.VerbJobCreate, role.VerbJobRead} {
		if !granting.May(verb) {
			t.Errorf("a role imported before the rename may not %s, and it was declared: %v",
				verb, granting.Verbs)
		}
	}
	if granting.May(role.VerbJobStop) {
		t.Errorf("it may %s, which it never declared: %v", role.VerbJobStop, granting.Verbs)
	}

	// A role that declared nothing still declares nothing. Default deny survives the rename too, and
	// a rename that filled the column in would be as wrong as one that emptied it.
	bare, err := kept.GetRole(ctx, "releaser", 1)
	if err != nil {
		t.Fatalf("GetRole: %v", err)
	}
	if len(bare.Verbs) != 0 {
		t.Errorf("a role that declared no verbs may call %v", bare.Verbs)
	}

	// And the old column is gone rather than sitting beside the new one, because two columns holding
	// one answer is how the next reader picks the wrong one.
	var oldThere bool
	if err := pool.QueryRow(ctx, `
		select exists (select 1 from information_schema.columns
		               where table_name = 'roles' and column_name = 'may')`).Scan(&oldThere); err != nil {
		t.Fatalf("check the old column: %v", err)
	}
	if oldThere {
		t.Error("roles still has a may column beside verbs, so two columns hold one answer")
	}
}
