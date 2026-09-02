package storetest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// theBuild is the record of a job's verticals being built against their failing tests, in the shape
// the system keeps one.
const theBuild = "Vertical 1: a person pastes a link on the command line and gets the text back\n" +
	"Ran 1: 14\n" +
	"Passes 1: TestPastingALinkPrintsTheTranscript\n" +
	"Changed 1: internal/transcript/paste.go\n" +
	"Vertical 2: a person opens the same transcript in a browser and shares the address\n" +
	"Ran 2: 9\n" +
	"Passes 2: TestTheTranscriptPageRendersAtItsAddress\n" +
	"Changed 2: internal/web/transcript.go"

// theAcceptanceQuestion is what a person is asked when every vertical is green.
const theAcceptanceQuestion = "Every vertical is built and its tests pass. Look at it and say " +
	"whether the value arrived."

// theBuildQuestion is what a person is asked when they are not.
const theBuildQuestion = "The verticals of this job are not built. Say what to do."

// runJobBuildConformance holds both stores to what the build stage writes.
//
// Two movements and one query, and neither store may be the lenient one. A store that let the record
// land twice would hold one job for two people. A store that wrote the record without stopping the
// job would let a built job carry on as though somebody had already looked at it, which is the whole
// of what this stage ends on. A store that took the question from a running row would put a question
// in front of a person about a job whose session is still working.
//
// Every case reads back through GetJob rather than trusting what the write answered with. A column
// written and never selected, or selected and never scanned, reads back empty in the real engine
// while the memory store passes, and the job then fans the same verticals out again on the next tick.
func runJobBuildConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("the record of the build lands and the job holds for a person", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingToBuild(t, s, workspace, project)

		held, err := s.HoldJobForAcceptance(ctx, id, theBuild, theAcceptanceQuestion,
			builtEvent(id, workspace, project),
			askedEvent(id, workspace, project, theAcceptanceQuestion))
		if err != nil {
			t.Fatalf("HoldJobForAcceptance: %v", err)
		}
		// Asking, not pending and not done. The machine's three checks are in the record it just read,
		// and the fourth is a person looking at the thing.
		if held.Phase != job.PhaseAsking || held.Question != theAcceptanceQuestion {
			t.Fatalf("the job is %q asking %q", held.Phase, held.Question)
		}

		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if kept.Build != theBuild {
			t.Fatalf("the record reads back as %q, which is the column this suite exists to catch",
				kept.Build)
		}
		// Nobody holds it, for the reason nobody holds any asking job: nothing moves until a person
		// answers.
		if kept.LeaseOwner != "" || kept.LeaseUntil != nil {
			t.Fatalf("the job is held by %q while it waits for a person", kept.LeaseOwner)
		}
		if verticals, passing := job.BuiltOn(kept.Build); verticals != 2 || passing != 2 {
			t.Fatalf("the record reads as %d verticals and %d passing tests", verticals, passing)
		}
		// And it owes no build now, or the same verticals fan out again on the next tick.
		if job.WaitingForItsBuild(kept) {
			t.Fatalf("a job that is built and waiting for a person still owes a build")
		}
	})

	t.Run("a second hold is refused and the first record stands", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingToBuild(t, s, workspace, project)
		if _, err := s.HoldJobForAcceptance(ctx, id, theBuild, theAcceptanceQuestion,
			builtEvent(id, workspace, project)); err != nil {
			t.Fatalf("HoldJobForAcceptance: %v", err)
		}

		if _, err := s.HoldJobForAcceptance(ctx, id, "Vertical 1: something else\nRan 1: 1",
			theAcceptanceQuestion, builtEvent(id, workspace, project)); err == nil {
			t.Fatal("a second record landed, so two fan outs would each write their own")
		}
		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if kept.Build != theBuild {
			t.Fatalf("the record reads back as %q, want the first one", kept.Build)
		}
	})

	t.Run("the question about verticals that are not built comes from the pending row",
		func(t *testing.T) {
			s := newDataset(t)(t)
			ctx := context.Background()
			workspace, project := aProject(t, s)
			id := waitingToBuild(t, s, workspace, project)

			asking, err := s.AskAboutJobBuild(ctx, id, theBuildQuestion,
				askedEvent(id, workspace, project, theBuildQuestion))
			if err != nil {
				t.Fatalf("AskAboutJobBuild: %v", err)
			}
			if asking.Phase != job.PhaseAsking || asking.Question != theBuildQuestion {
				t.Fatalf("the job is %q asking %q", asking.Phase, asking.Question)
			}
			kept, err := s.GetJob(ctx, id)
			if err != nil {
				t.Fatalf("GetJob: %v", err)
			}
			if kept.Build != "" {
				t.Fatalf("a job whose verticals are not built carries the record %q", kept.Build)
			}
			// And the question cannot be asked twice, because the second read finds it asking rather than
			// pending.
			if _, err := s.AskAboutJobBuild(ctx, id, theBuildQuestion,
				askedEvent(id, workspace, project, theBuildQuestion)); err == nil {
				t.Fatal("a job already asking was asked again about its build")
			}
		})

	t.Run("a job that is built is never asked about its build", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingToBuild(t, s, workspace, project)
		if _, err := s.HoldJobForAcceptance(ctx, id, theBuild, theAcceptanceQuestion,
			builtEvent(id, workspace, project)); err != nil {
			t.Fatalf("HoldJobForAcceptance: %v", err)
		}

		if _, err := s.AskAboutJobBuild(ctx, id, theBuildQuestion,
			askedEvent(id, workspace, project, theBuildQuestion)); err == nil {
			t.Fatal("a job whose verticals are built was stopped to ask about them")
		}
	})

	t.Run("the workers of a build fan out are found by the claim each one holds", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingToBuild(t, s, workspace, project)
		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		wanted := job.RequirementsOf(kept)
		failing := job.FailuresOn(kept.Tests)
		workers := job.BuildWorkers(kept, wanted, failing)
		if len(workers) != 2 {
			t.Fatalf("the fan out made %d workers for 2 verticals", len(workers))
		}
		for _, worker := range workers {
			if err := s.CreateJob(ctx, worker, declaredEvent(worker)); err != nil {
				t.Fatalf("CreateJob: %v", err)
			}
		}

		// A second worker for a vertical one already holds is refused, by the claim rather than by
		// anything the fan out remembers.
		second := job.BuildWorkers(kept, wanted[:1], failing)[0]
		err = s.CreateJob(ctx, second, declaredEvent(second))
		var taken *job.Held
		if !errors.As(err, &taken) {
			t.Fatalf("a second worker for vertical 1 was declared, and the store said %v", err)
		}
		if taken.Holder != workers[0].ID {
			t.Fatalf("the refusal names job %q, and %q holds the claim", taken.Holder, workers[0].ID)
		}

		// The boundary reads back off the row, because that is what the dispatch reads when it sends
		// the task. A worker whose flag was written and never selected would build under no gate.
		stored, err := s.GetJob(ctx, workers[0].ID)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if !stored.Building {
			t.Fatalf("the worker reads back outside the boundary, which is the column this suite " +
				"exists to catch")
		}

		// And the fan out reads its workers back whole. The answer is what it came for, and a listing
		// leaves the answer out.
		answered := "Vertical: 1\nRan: 14\nRed: 0\nPassing 1: TestItPasses\nChanged 1: paste.go"
		if _, err := s.StartJob(ctx, workers[0].ID, aLease("controller-1"),
			[]*job.Event{startedEvent(workers[0].ID, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}
		if _, err := s.LandJob(ctx, workers[0].ID, job.Landing{
			Phase: job.PhaseDone, Answer: answered, Outcome: job.OutcomeProved,
		}, answeredEvent(workers[0].ID, workspace, project)); err != nil {
			t.Fatalf("LandJob: %v", err)
		}

		var claims []string
		for _, vertical := range wanted {
			claims = append(claims, job.ClaimOnBuild(id, vertical))
		}
		found, err := s.JobsClaiming(ctx, workspace, claims)
		if err != nil {
			t.Fatalf("JobsClaiming: %v", err)
		}
		if len(found) != 2 {
			t.Fatalf("the fan out reads back %d workers for 2 verticals", len(found))
		}
		for _, worker := range found {
			if worker.Claim != job.ClaimOnBuild(id, wanted[0]) {
				continue
			}
			// The one that finished, read back with what it said. A settled worker has let its claim go, so
			// a query that asked who still holds it would find nothing and the stage would never close.
			if !strings.Contains(worker.Answer, "Passing 1: TestItPasses") {
				t.Fatalf("the worker that answered reads back with %q", worker.Answer)
			}
			if worker.Phase != job.PhaseDone {
				t.Fatalf("the worker that answered reads back as %q", worker.Phase)
			}
		}

		// The test stage's claim on the same vertical is a different claim, so a build worker is never
		// refused for work the worker that wrote the tests already did.
		writer := job.TestWorkers(kept, wanted[:1])[0]
		if err := s.CreateJob(ctx, writer, declaredEvent(writer)); err != nil {
			t.Fatalf("a worker writing the tests for vertical 1 was refused: %v", err)
		}
	})
}

// waitingToBuild is a job whose suite is red and whose plan a person approved, which is what makes it
// owe a build.
func waitingToBuild(t *testing.T, s store.Store, workspace, project string) string {
	t.Helper()
	ctx := context.Background()
	id := waitingToWriteItsTests(t, s, workspace, project)
	if _, err := s.RecordJobTests(ctx, id, theRedSuite, testedEvent(id, workspace, project)); err != nil {
		t.Fatalf("RecordJobTests: %v", err)
	}
	// The plan a person approved, which stands between the red suite and the build: it is the steps
	// that turn these tests green, and nothing is built until somebody approves them.
	const question = "the plan, do you approve it"
	if _, err := s.StartJob(ctx, id, aLease("controller-1"),
		[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	if _, err := s.ProposeJobPlan(ctx, id, "Step 1: build the two verticals", question,
		askedEvent(id, workspace, project, question)); err != nil {
		t.Fatalf("ProposeJobPlan: %v", err)
	}
	if _, err := s.ApproveJobPlan(ctx, id, toldEvent(id, workspace, project)); err != nil {
		t.Fatalf("ApproveJobPlan: %v", err)
	}
	return id
}

// builtEvent is the record that lands with a job's verticals being built.
func builtEvent(id, workspace, project string) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventBuilt, Job: id, Workspace: workspace, Project: project,
		Detail: "2 verticals were built against their failing tests",
	}
}
