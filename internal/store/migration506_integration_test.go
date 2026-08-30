//go:build integration

package store_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The word for the level above every workspace became "system", and four tables and one scope value
// carried the old one.
//
// This is the only part of the change that can lose something. A rename that missed would leave the
// operator's subscription token, their skills, their hooks, their roles and the context every session
// reads sitting in tables nothing queries, which on the screen is a system that came up having
// forgotten all of it. So it is proved against a real database, on rows written before the migration
// ran, rather than on a fresh one where every table is empty and a rename cannot be seen to work.
func TestTheLevelsRowsSurviveTheWordChanging(t *testing.T) {
	ctx := context.Background()
	pool := schemaOfItsOwn(t, "word506")

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Back to the shape a system had before this change, which is what an operator's database is at
	// the moment they upgrade: the four tables under the old word, and a context row that says it.
	for _, statement := range []string{
		`alter table system_secrets rename to crew_secrets`,
		`alter table system_skills rename to crew_skills`,
		`alter table system_hooks rename to crew_hooks`,
		`alter table system_roles rename to crew_roles`,
		`delete from schema_migrations where version = '0045_system_names'`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("put the schema back to what it was: %s: %v", statement, err)
		}
	}
	// The rows the operator would lose. Sealed bytes rather than a plain value, because that is what
	// the column holds and a rename that rewrote them would be worse than one that dropped them.
	if _, err := pool.Exec(ctx,
		`insert into crew_secrets (name, sealed, projection) values ($1, $2, 'env')`,
		"CLAUDE_CODE_OAUTH_TOKEN", []byte("sealed-bytes"),
	); err != nil {
		t.Fatalf("seed the secret: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`insert into contexts (scope, owner, body) values ('crew', '', $1)`,
		"never commit without asking",
	); err != nil {
		t.Fatalf("seed the context: %v", err)
	}

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate over the old shape: %v", err)
	}

	// The secret, read back the way the control plane reads it rather than by looking at a table, so
	// this is the operator's token reaching a session and not just a row that moved.
	held, err := secrets.NewPostgres(pool, make([]byte, 32))
	if err != nil {
		t.Fatalf("open the secrets backend: %v", err)
	}
	refs, err := held.ListSystem(ctx)
	if err != nil {
		t.Fatalf("list what the system holds: %v", err)
	}
	if len(refs) != 1 || refs[0].Name != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Fatalf("the system holds %+v, want the one secret it held before the migration", refs)
	}
	var sealed []byte
	if err := pool.QueryRow(ctx,
		`select sealed from system_secrets where name = $1`, "CLAUDE_CODE_OAUTH_TOKEN",
	).Scan(&sealed); err != nil {
		t.Fatalf("read the sealed value back: %v", err)
	}
	if string(sealed) != "sealed-bytes" {
		t.Fatalf("the sealed value is %q, want it carried across untouched", sealed)
	}

	// And the context, which is the level every session in every workspace reads before it reads a
	// line of anything else.
	opened, err := store.NewPostgres(ctx, databaseURL+"&search_path=word506")
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	t.Cleanup(opened.Close)
	body, err := opened.GetContext(ctx, store.ContextSystem, "")
	if err != nil {
		t.Fatalf("read the system's context: %v", err)
	}
	if body != "never commit without asking" {
		t.Fatalf("the system's context says %q, want what was written under the old word", body)
	}

	// Nothing is left behind under the old word, or a later reader would find two answers.
	for _, table := range []string{"crew_secrets", "crew_skills", "crew_hooks", "crew_roles"} {
		var there bool
		if err := pool.QueryRow(ctx,
			`select exists (select 1 from information_schema.tables
			 where table_schema = 'word506' and table_name = $1)`, table,
		).Scan(&there); err != nil {
			t.Fatalf("look for %s: %v", table, err)
		}
		if there {
			t.Fatalf("%s is still there, so a reader can find the level in two places", table)
		}
	}
}

// schemaOfItsOwn gives a test its own set of tables inside the shared database, so it can migrate
// from nothing without truncating what every other test in this package is using.
func schemaOfItsOwn(t *testing.T, named string) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, fmt.Sprintf(`drop schema if exists %s cascade`, named)); err != nil {
		t.Fatalf("drop schema %s: %v", named, err)
	}
	if _, err := admin.Exec(ctx, fmt.Sprintf(`create schema %s`, named)); err != nil {
		t.Fatalf("create schema %s: %v", named, err)
	}

	pool, err := pgxpool.New(ctx, databaseURL+"&search_path="+named)
	if err != nil {
		t.Fatalf("open postgres on schema %s: %v", named, err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanup, err := pgxpool.New(context.Background(), databaseURL)
		if err != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.Exec(context.Background(), fmt.Sprintf(`drop schema if exists %s cascade`, named))
	})
	return pool
}
