//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// Rows already carrying a parent are of two kinds, and they go to different places. A row a session
// declared keeps its identity as a job in its project and gains the cause; a row a flow run declared
// becomes a step of that run.
//
// This is the part of the change that can lose something. A move that missed would leave a job a
// person declared out of the listing of its project, or leave a run's step belonging to nothing, so
// it is proved against a real database on rows written before the migration ran.
func TestBothKindsOfChildSurviveTheirParentGoingAway(t *testing.T) {
	ctx := context.Background()
	pool, ownURL := databaseOfItsOwn(t, "nesting666")

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Back to the shape a system had before this change, which is what an operator's database is at
	// the moment they upgrade: a parent and a depth on the jobs table, a job at the top of every
	// steer, and a ceiling that counted depth.
	for _, statement := range []string{
		`alter table jobs add column parent text references jobs (id)`,
		`alter table jobs add column depth int not null default 0`,
		`alter table jobs drop column cause`,
		`alter table jobs drop column run`,
		`alter table job_events add column parent text not null default ''`,
		`alter table job_events add column depth int not null default 0`,
		`alter table job_steers add column root text not null default ''`,
		`alter table workspace_limits rename column max_declared to max_depth`,
		`delete from schema_migrations where version = '0059_a_job_is_not_under_a_job'`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("put the schema back to what it was: %s: %v", statement, err)
		}
	}

	for _, seed := range []string{
		`insert into workspaces (id, name) values ('w1', 'a workspace')`,
		`insert into projects (id, workspace, name) values ('p1', 'w1', 'a project')`,
		`insert into workspace_limits (workspace, max_depth) values ('w1', 2)`,
		`insert into jobs (id, workspace, project, title, brief, phase, version)
			values ('job-1', 'w1', 'p1', 'the job somebody declared', 'do the thing', 'pending', 1)`,
		// A job the session running job-1 declared. It is a job, and it has to stay one.
		`insert into jobs (id, workspace, project, title, brief, parent, depth, phase, version, answer)
			values ('job-2', 'w1', 'p1', 'fetch the captions', 'fetch them', 'job-1', 1, 'done', 1,
				'the captions are in the bucket')`,
		// The job carrying a flow run, which a session started, and one step of that run.
		`insert into jobs (id, workspace, project, title, brief, parent, depth, phase, version, labels)
			values ('carrier-1', 'w1', 'p1', 'flow fix-red version 1', 'carries the run',
				'job-1', 1, 'waiting', 1, '{"flow.run": "run-1", "flow.graph": "fix-red"}')`,
		`insert into jobs (id, workspace, project, title, brief, parent, depth, phase, version, labels, answer)
			values ('step-1', 'w1', 'p1', 'fix-red step fix', 'fix the build', 'carrier-1', 2,
				'done', 1, '{"flow.run": "run-1", "flow.graph": "fix-red", "flow.node": "fix"}',
				'the build is green')`,
		`insert into job_steers (id, job, root, workspace, project, text)
			values ('steer-1', 'job-2', 'job-1', 'w1', 'p1', 'it chose a store that bills while idle')`,
	} {
		if _, err := pool.Exec(ctx, seed); err != nil {
			t.Fatalf("seed the rows an operator would have: %s: %v", seed, err)
		}
	}

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate over the old shape: %v", err)
	}

	opened, err := store.NewPostgres(ctx, ownURL)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	t.Cleanup(opened.Close)

	// Every row is still a row of the project, and the answers they carry are still there.
	listed, err := opened.ListJobs(ctx, job.Filter{Project: "p1"})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	held := map[string]*job.Job{}
	for _, one := range listed {
		held[one.ID] = one
	}
	if len(listed) != 4 {
		t.Fatalf("the project lists %d jobs, want the four rows it had", len(listed))
	}

	// A row a session declared: a job in its project, carrying the cause and belonging to no run.
	caused := held["job-2"]
	switch {
	case caused == nil:
		t.Fatal("the job a session declared is not in the listing of its project")
	case caused.Cause != "job-1":
		t.Fatalf("the job a session declared says %q caused it, want the job it hung under", caused.Cause)
	case caused.Run != "":
		t.Fatalf("the job a session declared belongs to run %q, and no run declared it", caused.Run)
	}
	// The answer whole, off the reader rather than the listing: a listing carries no answers.
	if read, err := opened.GetJob(ctx, "job-2"); err != nil ||
		read.Answer != "the captions are in the bucket" {
		t.Fatalf("the job a session declared reads back answering %q (%v)", answerOf(read), err)
	}

	// A row a flow run declared: a step of that run, belonging to no job.
	step := held["step-1"]
	switch {
	case step == nil:
		t.Fatal("the step of the run is not in the listing of its project")
	case step.Run != "run-1":
		t.Fatalf("the step belongs to run %q, want the run whose node it ran", step.Run)
	case step.Cause != "":
		t.Fatalf("the step says %q caused it, and a step belongs to its run", step.Cause)
	}
	if read, err := opened.GetJob(ctx, "step-1"); err != nil || read.Answer != "the build is green" {
		t.Fatalf("the step reads back answering %q (%v)", answerOf(read), err)
	}

	// The job carrying the run is one of the first kind: a session started the run, so that session's
	// job is what caused it, and it is not one of the run's own steps.
	carrier := held["carrier-1"]
	switch {
	case carrier == nil:
		t.Fatal("the job carrying the run is not in the listing of its project")
	case carrier.Cause != "job-1":
		t.Fatalf("the job carrying the run says %q caused it, want the job whose session started it",
			carrier.Cause)
	case carrier.Run != "":
		t.Fatalf("the job carrying the run reads back as a step of run %q", carrier.Run)
	}

	// And the run reads its own steps, while the job carrying it lists none.
	steps, err := opened.ListJobs(ctx, job.Filter{Run: "run-1"})
	if err != nil {
		t.Fatalf("ListJobs by run: %v", err)
	}
	if len(steps) != 1 || steps[0].ID != "step-1" {
		t.Fatalf("the run reads back %d steps, want the one it declared", len(steps))
	}

	// The steers already recorded keep the job they landed on.
	marks, err := opened.ListSteers(ctx, "job-2")
	if err != nil {
		t.Fatalf("ListSteers: %v", err)
	}
	if len(marks) != 1 || marks[0].Job != "job-2" {
		t.Fatalf("the job reads back %d steers, want the one made against it", len(marks))
	}

	// And the ceiling an operator set survives, meaning what it means now.
	limits, err := opened.WorkspaceLimits(ctx, "w1")
	if err != nil {
		t.Fatalf("WorkspaceLimits: %v", err)
	}
	if limits.MaxDeclared != 2 {
		t.Fatalf("the workspace lets one session declare %d jobs, want the 2 it was set to",
			limits.MaxDeclared)
	}
}

// answerOf is what a row answered, and nothing where the read failed, so a failure prints what it
// found rather than stopping on a nil row.
func answerOf(one *job.Job) string {
	if one == nil {
		return ""
	}
	return one.Answer
}
