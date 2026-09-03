package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// A run of a stage is a row of its own, and both stores keep the same one.
//
// The trap this suite exists for is the column list. Every field of an execution has to reach the
// insert and come back out of the scan, and a store that drops one reads zero where the other passes
// every test. So one case writes every field and reads all of them back.

func runExecutionConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a run round trips with every field it carries", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingToWriteItsTests(t, s, workspace, project)

		until := time.Now().UTC().Add(time.Minute).Truncate(time.Millisecond)
		written := &job.Execution{
			ID: store.NewID(), Job: id, Stage: job.StageBuild, Number: 2,
			Claim: "a piece of work", Phase: job.PhaseRunning, Session: "session-1", Attempts: 2,
			Answer: "Vertical: 2\nRan: 9", Outcome: job.OutcomeProved, Reason: "nothing went wrong",
			Branch: "krewe/tests/" + id, PullRequest: "https://github.com/an/owner/pull/7",
			SpentTokens: 4096, LeaseOwner: "controller-1", LeaseUntil: &until,
			TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", ParentSpanID: "00f067aa0ba902b7",
		}
		if err := s.CreateExecution(ctx, written, ranEvent(id, workspace, project, written)); err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}

		found, err := s.GetExecution(ctx, written.ID)
		if err != nil {
			t.Fatalf("GetExecution: %v", err)
		}
		switch {
		case found.Job != written.Job:
			t.Fatalf("the run reads back under job %q, want %q", found.Job, written.Job)
		case found.Stage != written.Stage:
			t.Fatalf("the run reads back in stage %q, want %q", found.Stage, written.Stage)
		case found.Number != written.Number:
			t.Fatalf("the run reads back as number %d, want %d", found.Number, written.Number)
		case found.Claim != written.Claim:
			t.Fatalf("the run reads back claiming %q, want %q", found.Claim, written.Claim)
		case found.Phase != written.Phase:
			t.Fatalf("the run reads back as %q, want %q", found.Phase, written.Phase)
		case found.Session != written.Session:
			t.Fatalf("the run reads back in session %q, want %q", found.Session, written.Session)
		case found.Attempts != written.Attempts:
			t.Fatalf("the run reads back with %d attempts, want %d", found.Attempts, written.Attempts)
		case found.Answer != written.Answer:
			t.Fatalf("the run reads back with the answer %q", found.Answer)
		case found.Outcome != written.Outcome:
			t.Fatalf("the run reads back with the outcome %q", found.Outcome)
		case found.Reason != written.Reason:
			t.Fatalf("the run reads back with the reason %q", found.Reason)
		case found.Branch != written.Branch:
			t.Fatalf("the run reads back on the branch %q, want %q", found.Branch, written.Branch)
		case found.PullRequest != written.PullRequest:
			t.Fatalf("the run reads back naming %q", found.PullRequest)
		case found.SpentTokens != written.SpentTokens:
			t.Fatalf("the run reads back having spent %d, want %d", found.SpentTokens, written.SpentTokens)
		case found.LeaseOwner != written.LeaseOwner:
			t.Fatalf("the run reads back held by %q, want %q", found.LeaseOwner, written.LeaseOwner)
		case found.LeaseUntil == nil || !found.LeaseUntil.Equal(until):
			t.Fatalf("the run reads back held until %v, want %v", found.LeaseUntil, until)
		case found.TraceID != written.TraceID || found.ParentSpanID != written.ParentSpanID:
			t.Fatalf("the run reads back in trace %q / %q, so it left the job's trace",
				found.TraceID, found.ParentSpanID)
		case found.CreatedAt.IsZero() || found.UpdatedAt.IsZero():
			t.Fatalf("the run reads back with no moment on it: %v / %v", found.CreatedAt, found.UpdatedAt)
		}
	})

	t.Run("a run is dispatched, held and landed the way a job is", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingToWriteItsTests(t, s, workspace, project)
		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		run := job.TestExecutions(kept, job.RequirementsOf(kept))[0]
		if err := s.CreateExecution(ctx, run, ranEvent(id, workspace, project, run)); err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}

		runnable, err := s.RunnableExecutions(ctx, 10)
		if err != nil {
			t.Fatalf("RunnableExecutions: %v", err)
		}
		if len(runnable) != 1 || runnable[0].ID != run.ID {
			t.Fatalf("the run a controller may start reads back as %d rows", len(runnable))
		}

		started, err := s.StartExecution(ctx, run.ID, aLease("controller-1"), nil)
		if err != nil {
			t.Fatalf("StartExecution: %v", err)
		}
		if started.Phase != job.PhaseRunning || started.Attempts != 1 {
			t.Fatalf("a started run reads back %q on attempt %d", started.Phase, started.Attempts)
		}
		// Claimed once. A second controller reading the same pending row is refused rather than
		// dispatching a second session for the same requirement.
		if _, err := s.StartExecution(ctx, run.ID, aLease("controller-2"), nil); err == nil {
			t.Fatal("a run already running was claimed a second time")
		}
		if err := s.RecordExecutionSession(ctx, run.ID, "session-9"); err != nil {
			t.Fatalf("RecordExecutionSession: %v", err)
		}
		if err := s.RecordExecutionBranch(ctx, run.ID, "krewe/tests/"+id); err != nil {
			t.Fatalf("RecordExecutionBranch: %v", err)
		}

		held, err := s.HeldExecutions(ctx, "controller-1", 10)
		if err != nil {
			t.Fatalf("HeldExecutions: %v", err)
		}
		if len(held) != 1 || held[0].Session != "session-9" {
			t.Fatalf("the controller holds %d runs, and the first reads back in session %q",
				len(held), sessionOfFirst(held))
		}
		other, err := s.HeldExecutions(ctx, "controller-2", 10)
		if err != nil {
			t.Fatalf("HeldExecutions: %v", err)
		}
		if len(other) != 0 {
			t.Fatalf("a second controller holds %d runs it never claimed", len(other))
		}

		landed, err := s.LandExecution(ctx, run.ID, job.ExecutionLanding{
			Phase: job.PhaseDone, Answer: "Requirement: 1\nRan: 3\nFailing 1: TestIt",
			Outcome: job.OutcomeProved, SpentTokens: 128,
		}, nil)
		if err != nil {
			t.Fatalf("LandExecution: %v", err)
		}
		switch {
		case landed.Phase != job.PhaseDone:
			t.Fatalf("a landed run reads back as %q", landed.Phase)
		case landed.Branch != "krewe/tests/"+id:
			t.Fatalf("the landing took the branch off the run: %q", landed.Branch)
		case landed.SpentTokens != 128:
			t.Fatalf("a landed run reads back having spent %d", landed.SpentTokens)
		case landed.FinishedAt == nil:
			t.Fatal("a landed run reads back with no moment it finished")
		case landed.LeaseOwner != "":
			t.Fatalf("a landed run is still held by %q", landed.LeaseOwner)
		}
		if _, err := s.LandExecution(ctx, run.ID, job.ExecutionLanding{Phase: job.PhaseFailed},
			nil); err == nil {
			t.Fatal("a run that had already landed was landed a second time")
		}
	})

	t.Run("nobody declares a run, so a run refuses what a job accepts", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingToWriteItsTests(t, s, workspace, project)

		// A run with no job, a run in no stage and a run for no number are each refused. Every one of
		// them is a row a stage could never gather, and a store that took it would hold a run nothing
		// reads.
		for name, run := range map[string]*job.Execution{
			"no job":    {ID: store.NewID(), Stage: job.StageTest, Number: 1},
			"no stage":  {ID: store.NewID(), Job: id, Number: 1},
			"no number": {ID: store.NewID(), Job: id, Stage: job.StageTest},
		} {
			if err := s.CreateExecution(ctx, run, nil); err == nil {
				t.Fatalf("a run with %s was written", name)
			}
		}

		// And a person cannot declare one. There is no road from a declaration to this table: what a
		// caller writes is a job, and CreateJob is the only call that takes one.
		declared := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project, Version: 1,
			Phase: job.PhasePending, Title: "not a run", Brief: "a person wrote this",
		}
		if err := s.CreateJob(ctx, declared, declaredEvent(declared)); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}
		runs, err := s.ListExecutions(ctx, job.ExecutionFilter{Job: declared.ID})
		if err != nil {
			t.Fatalf("ListExecutions: %v", err)
		}
		if len(runs) != 0 {
			t.Fatalf("declaring a job wrote %d runs", len(runs))
		}
	})
}

// sessionOfFirst is the session the first run in a listing reads back in, and a word where the
// listing is empty, so a refusal can say what it found rather than reading past the end.
func sessionOfFirst(runs []*job.Execution) string {
	if len(runs) == 0 {
		return "nothing"
	}
	return runs[0].Session
}

// ranEvent is the record that lands with a run of a stage. It hangs off the job the run belongs to
// and names the run, because a run is not a job.
func ranEvent(id, workspace, project string, run *job.Execution) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventRan, Job: id, Workspace: workspace, Project: project,
		Execution: run.ID, Detail: "a run of the " + run.Stage + " stage",
		OccurredAt: time.Now().UTC(),
	}
}
