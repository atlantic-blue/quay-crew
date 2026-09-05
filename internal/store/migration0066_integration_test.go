//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// A path belonged to a project and now belongs to a feature, and migration 0066 carries the rows
// that already exist across.
//
// This is the part of the change that can lose something. The schema half of it is proved by every
// other test simply running, because nothing reads project_steps any more. The row half is not: a
// migration that renamed the table and left the steps pointing at nothing would pass a fresh
// database and empty the path of every project that had one. So it is proved on rows written before
// the migration ran.
func TestThePathsWrittenBeforeAFeatureExistedSurviveTheKeyMoving(t *testing.T) {
	ctx := context.Background()
	pool, ownURL := databaseOfItsOwn(t, "path0066")

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Back to the shape a system had before this change: the step table keyed by the project, under
	// the name it had. This is what an operator's database looks like at the moment they upgrade.
	for _, statement := range []string{
		`alter table feature_steps drop constraint feature_steps_pkey`,
		`alter table feature_steps add column project text references projects (id) on delete cascade`,
		`alter table feature_steps drop column feature`,
		`alter table feature_steps alter column project set not null`,
		`alter table feature_steps add primary key (project, number)`,
		`alter index feature_steps_session_idx rename to project_steps_session_idx`,
		`alter table feature_steps rename to project_steps`,
		`delete from schema_migrations where version = '0066_a_path_belongs_to_a_feature'`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("put the schema back to what it was: %s: %v", statement, err)
		}
	}

	// Two projects: one holding a path, and one holding none. The second is here because a project
	// that never had a step must not come out of this owning a feature nobody asked for.
	if _, err := pool.Exec(ctx,
		`insert into workspaces (id, name) values ('w1', 'acme')`); err != nil {
		t.Fatalf("seed the workspace: %v", err)
	}
	for _, project := range []struct{ id, name string }{
		{"p1", "house-bills"},
		{"p2", "the-garden"},
	} {
		if _, err := pool.Exec(ctx,
			`insert into projects (id, workspace, name) values ($1, 'w1', $2)`,
			project.id, project.name); err != nil {
			t.Fatalf("seed project %s: %v", project.id, err)
		}
	}
	// Every column a caller may set, and the two the system writes when somebody takes a step. A
	// migration that kept the numbers and dropped the words would read as a path that survived.
	if _, err := pool.Exec(ctx, `
		insert into project_steps
			(project, number, title, intention, touches, proof, proof_scenario, after, state, session)
		values
			('p1', 1, 'the store holds a brief', 'The design has nowhere to live.',
			 'internal/store/store.go', 'It reads back.', 'a project carries a brief', 0,
			 'taken', 'session-one'),
			('p1', 5, 'the command line reads it back', '', '', '', '', 1, 'ready', '')`,
	); err != nil {
		t.Fatalf("seed the path: %v", err)
	}

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate over the old shape: %v", err)
	}

	// The project that held steps holds one feature now, numbered 1 and titled with the project name.
	var featureID, title string
	var number int32
	if err := pool.QueryRow(ctx,
		`select id, number, title from features where project = 'p1'`,
	).Scan(&featureID, &number, &title); err != nil {
		t.Fatalf("read the feature the migration made: %v", err)
	}
	if number != 1 || title != "house-bills" {
		t.Fatalf("the feature reads number %d titled %q, want 1 titled house-bills", number, title)
	}

	// The project that held none holds none. A feature over an empty path is a row that says nothing.
	var over int
	if err := pool.QueryRow(ctx,
		`select count(*) from features where project = 'p2'`).Scan(&over); err != nil {
		t.Fatalf("count the features of the project with no path: %v", err)
	}
	if over != 0 {
		t.Fatalf("the project that held no step came out of the migration with %d features", over)
	}

	// Both steps, under that one feature, with every column they were written with.
	opened, err := store.NewPostgres(ctx, ownURL)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	t.Cleanup(opened.Close)
	steps, err := opened.ListSteps(ctx, featureID)
	if err != nil {
		t.Fatalf("read the path back under its feature: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("the path reads back as %d steps, want the 2 that were written", len(steps))
	}
	first := steps[0]
	if first.GetNumber() != 1 || first.GetTitle() != "the store holds a brief" {
		t.Fatalf("step 1 reads number %d titled %q", first.GetNumber(), first.GetTitle())
	}
	if first.GetIntention() != "The design has nowhere to live." ||
		first.GetTouches() != "internal/store/store.go" {
		t.Errorf("step 1 reads intention %q and touches %q",
			first.GetIntention(), first.GetTouches())
	}
	if first.GetProof() != "It reads back." || first.GetProofScenario() != "a project carries a brief" {
		t.Errorf("step 1 reads proof %q and scenario %q",
			first.GetProof(), first.GetProofScenario())
	}
	if first.GetState() != store.StepTaken || first.GetSession() != "session-one" {
		t.Errorf("step 1 reads as %q held by %q, want the state and session it was written with",
			first.GetState(), first.GetSession())
	}
	if first.GetFeature() != featureID {
		t.Errorf("step 1 names feature %q, want %q", first.GetFeature(), featureID)
	}
	// The number and what it waits for, because a path renumbered by the migration is a different
	// path.
	if steps[1].GetNumber() != 5 || steps[1].GetAfter() != 1 {
		t.Errorf("step 5 reads number %d waiting for %d, want 5 waiting for 1",
			steps[1].GetNumber(), steps[1].GetAfter())
	}
}
