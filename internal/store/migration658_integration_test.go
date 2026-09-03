//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// A run of a stage used to be a job row under the job it ran for, so every system that has run the
// test or the build stage holds some.
//
// This is the part of the split that can lose something. A move that missed would leave the answer a
// run gave sitting in a table nothing reads, and the stage that gathers those answers would then ask
// a person about a requirement whose tests were written days ago. So it is proved against a real
// database, on rows written before the migration ran, rather than on a fresh one where the table is
// empty and a move cannot be seen to work.
func TestTheRunsOfAStageSurviveBecomingExecutions(t *testing.T) {
	ctx := context.Background()
	pool, ownURL := databaseOfItsOwn(t, "executions658")

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Back to the shape a system had before this change, which is what an operator's database is at
	// the moment they upgrade: no executions table, the flag on the jobs table, and no column on a
	// record saying which run it happened in.
	for _, statement := range []string{
		`drop table executions`,
		`alter table job_events drop column execution`,
		`alter table jobs add column building boolean not null default false`,
		`alter table jobs add column branch text not null default ''`,
		`delete from schema_migrations where version = '0058_executions'`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("put the schema back to what it was: %s: %v", statement, err)
		}
	}

	// The rows an operator would lose: a job whose build stage fanned out, and the worker holding its
	// second vertical, which answered and is the only record of that work.
	if _, err := pool.Exec(ctx,
		`insert into workspaces (id, name) values ('w1', 'a workspace')`); err != nil {
		t.Fatalf("seed the workspace: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`insert into projects (id, workspace, name) values ('p1', 'w1', 'a project')`); err != nil {
		t.Fatalf("seed the project: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into jobs (id, workspace, project, title, brief, phase, version)
		values ('job-1', 'w1', 'p1', 'the job somebody declared', 'do the thing', 'pending', 1)`,
	); err != nil {
		t.Fatalf("seed the job: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into jobs (id, workspace, project, title, brief, parent, depth, phase, version,
			claim, building, ungated, session, attempts, answer, outcome, spent_tokens,
			branch, pull_request, trace_id, parent_span_id)
		values ('run-2', 'w1', 'p1', 'build vertical 2: the page', 'build it', 'job-1', 1, 'done', 1,
			'job-1 build 2', true, true, 'session-2', 1,
			'Vertical: 2' || chr(10) || 'Ran: 14' || chr(10) || 'Red: 0', 'proved', 4096,
			'krewe/job-1-requirement-2', 'https://github.com/an/owner/pull/7',
			'4bf92f3577b34da6a3ce929d0e0e4736', '00f067aa0ba902b7')`,
	); err != nil {
		t.Fatalf("seed the worker: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into job_events (id, kind, job, workspace, project, parent, depth, detail, occurred_at)
		values ('event-1', 'job.answered', 'run-2', 'w1', 'p1', 'job-1', 1,
			'the build of vertical 2', now())`,
	); err != nil {
		t.Fatalf("seed the record: %v", err)
	}

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate over the old shape: %v", err)
	}

	// The run reads back as a run, keeping its identifier, so a link somebody wrote down still
	// reaches it. Read through the store rather than off the table, because what has to survive is
	// what the stage gathers.
	opened, err := store.NewPostgres(ctx, ownURL)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	t.Cleanup(opened.Close)
	runs, err := opened.ListExecutions(ctx,
		job.ExecutionFilter{Job: "job-1", Stage: job.StageBuild})
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("the job holds %d runs of its build stage, want the one it had", len(runs))
	}
	moved := runs[0]
	switch {
	case moved.ID != "run-2":
		t.Fatalf("the run reads back as %q, want the identifier it had", moved.ID)
	case moved.Number != 2:
		t.Fatalf("the run reads back as number %d, want the vertical it held", moved.Number)
	case moved.Claim != "job-1 build 2":
		t.Fatalf("the run reads back claiming %q", moved.Claim)
	case moved.Phase != job.PhaseDone || moved.Outcome != "proved":
		t.Fatalf("the run reads back %q on the outcome %q", moved.Phase, moved.Outcome)
	case moved.Answer == "":
		t.Fatal("the run reads back with no answer, which is the only record of its work")
	case moved.Session != "session-2" || moved.SpentTokens != 4096:
		t.Fatalf("the run reads back in session %q having spent %d", moved.Session, moved.SpentTokens)
	case moved.Branch != "krewe/job-1-requirement-2":
		t.Fatalf("the run reads back on the branch %q, so the work it did is nowhere", moved.Branch)
	case moved.PullRequest != "https://github.com/an/owner/pull/7":
		t.Fatalf("the run reads back naming %q", moved.PullRequest)
	case moved.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736":
		t.Fatalf("the run left the job's trace: %q", moved.TraceID)
	}

	// And it is no longer a job. A row nobody declared standing in the jobs listing is the whole
	// fault this change is about.
	listed, err := opened.ListJobs(ctx, job.Filter{Project: "p1"})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "job-1" {
		t.Fatalf("the jobs listing holds %d rows after the move, want the one job somebody declared",
			len(listed))
	}

	// The record it wrote moved onto the job and names the run it happened in, so nothing that was
	// written down is lost with the row it used to hang off.
	var on, execution string
	if err := pool.QueryRow(ctx,
		`select job, execution from job_events where id = 'event-1'`).Scan(&on, &execution); err != nil {
		t.Fatalf("read the record back: %v", err)
	}
	if on != "job-1" || execution != "run-2" {
		t.Fatalf("the record hangs off %q naming run %q, want it on the job naming the run", on, execution)
	}
}
