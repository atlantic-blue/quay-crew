package storetest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// theRedSuite is the record of a job's requirements becoming failing tests, in the shape the system
// keeps one, used by every case below and by the stage suite next door.
const theRedSuite = "Requirement 1: a person pastes a link on the command line and gets the text back\n" +
	"Ran 1: 12\n" +
	"Fails 1: TestPastingALinkPrintsTheTranscript\n" +
	"Requirement 2: a person opens the same transcript in a browser and shares the address\n" +
	"Ran 2: 9\n" +
	"Fails 2: TestTheTranscriptPageRendersAtItsAddress"

// theSuiteQuestion is what a person is asked when that suite is not red for the reasons the stage
// needs.
const theSuiteQuestion = "The tests for this job's requirements are not red. Say what to do."

// runJobTestConformance holds both stores to what the test stage writes.
//
// Three movements and one query, and neither store may be the lenient one. A store that let the
// record land twice would plan against one list and count the tests of another. A store that took
// the question from a running row would put a question in front of a person about a job whose
// session is still working. A store that answered the claim query with a listing would hand the fan
// out rows with no answer on them, and every requirement would read as having written nothing.
//
// Every case reads back through GetJob rather than trusting what the write answered with. A column
// written and never selected, or selected and never scanned, reads back empty in the real engine
// while the memory store passes, and the job then plans with no record of any test at all.
func runJobTestConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("the record of the failing tests lands on a pending job", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingToWriteItsTests(t, s, workspace, project)

		pending, err := s.RecordJobTests(ctx, id, theRedSuite, testedEvent(id, workspace, project))
		if err != nil {
			t.Fatalf("RecordJobTests: %v", err)
		}
		// Pending after it, not asking and not running. What ran was one job for each requirement, and
		// this row is next started to write the plan that turns the suite green.
		if pending.Phase != job.PhasePending {
			t.Fatalf("the job is %q after its suite went red, want pending", pending.Phase)
		}

		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if kept.Tests != theRedSuite {
			t.Fatalf("the record reads back as %q, which is the column this suite exists to catch",
				kept.Tests)
		}
		// And the stage moves with it, read off the row rather than written on it.
		if stage := job.StageOf(kept); stage.Name != job.StageBuild {
			t.Fatalf("a job whose suite is red reads as stage %q", stage.Name)
		}
		if requirements, failing := job.TestsOn(kept.Tests); requirements != 2 || failing != 2 {
			t.Fatalf("the record reads as %d requirements and %d failing tests", requirements, failing)
		}
	})

	t.Run("a second record is refused and the first one stands", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingToWriteItsTests(t, s, workspace, project)
		if _, err := s.RecordJobTests(ctx, id, theRedSuite,
			testedEvent(id, workspace, project)); err != nil {
			t.Fatalf("RecordJobTests: %v", err)
		}

		if _, err := s.RecordJobTests(ctx, id, "Requirement 1: something else\nRan 1: 1\nFails 1: X",
			testedEvent(id, workspace, project)); err == nil {
			t.Fatal("a second record landed, so two fan outs would each write their own")
		}
		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if kept.Tests != theRedSuite {
			t.Fatalf("the record reads back as %q, want the first one", kept.Tests)
		}
	})

	t.Run("the question about a suite that is not red comes from the pending row", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingToWriteItsTests(t, s, workspace, project)

		asking, err := s.AskAboutJobTests(ctx, id, theSuiteQuestion,
			askedEvent(id, workspace, project, theSuiteQuestion))
		if err != nil {
			t.Fatalf("AskAboutJobTests: %v", err)
		}
		if asking.Phase != job.PhaseAsking || asking.Question != theSuiteQuestion {
			t.Fatalf("the job is %q asking %q", asking.Phase, asking.Question)
		}
		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if kept.Tests != "" {
			t.Fatalf("a job that could not go red carries the record %q", kept.Tests)
		}
		// Nobody holds it, for the reason nobody holds any asking job: nothing moves until a person
		// answers.
		if kept.LeaseOwner != "" || kept.LeaseUntil != nil {
			t.Fatalf("the job is held by %q while it waits for a person", kept.LeaseOwner)
		}
		// And the question cannot be asked twice, because the second read finds it asking rather than
		// pending.
		if _, err := s.AskAboutJobTests(ctx, id, theSuiteQuestion,
			askedEvent(id, workspace, project, theSuiteQuestion)); err == nil {
			t.Fatal("a job already asking was asked again about its suite")
		}
	})

	t.Run("a job that already went red is never asked about its suite", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingToWriteItsTests(t, s, workspace, project)
		if _, err := s.RecordJobTests(ctx, id, theRedSuite,
			testedEvent(id, workspace, project)); err != nil {
			t.Fatalf("RecordJobTests: %v", err)
		}

		if _, err := s.AskAboutJobTests(ctx, id, theSuiteQuestion,
			askedEvent(id, workspace, project, theSuiteQuestion)); err == nil {
			t.Fatal("a job whose suite is red was stopped to ask about it")
		}
	})

	t.Run("the workers of a fan out are found by the claim each one holds", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingToWriteItsTests(t, s, workspace, project)
		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		wanted := job.RequirementsOf(kept)
		if len(wanted) != 2 {
			t.Fatalf("the accepted list carries %d requirements, want 2", len(wanted))
		}
		workers := job.TestWorkers(kept, wanted)
		for _, worker := range workers {
			if err := s.CreateJob(ctx, worker, declaredEvent(worker)); err != nil {
				t.Fatalf("CreateJob: %v", err)
			}
		}

		// A second worker for a requirement one already holds is refused, by the claim rather than by
		// anything the fan out remembers.
		second := job.TestWorkers(kept, wanted[:1])[0]
		err = s.CreateJob(ctx, second, declaredEvent(second))
		var taken *job.Held
		if !errors.As(err, &taken) {
			t.Fatalf("a second worker for requirement 1 was declared, and the store said %v", err)
		}
		if taken.Holder != workers[0].ID {
			t.Fatalf("the refusal names job %q, and %q holds the claim", taken.Holder, workers[0].ID)
		}

		// And the fan out reads its workers back whole. The answer is what it came for, and a listing
		// leaves the answer out.
		answered := "Requirement: 1\nRan: 12\nFailing 1: TestItFails"
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
		for _, requirement := range wanted {
			claims = append(claims, job.ClaimOnRequirement(id, requirement))
		}
		found, err := s.JobsClaiming(ctx, workspace, claims)
		if err != nil {
			t.Fatalf("JobsClaiming: %v", err)
		}
		if len(found) != 2 {
			t.Fatalf("the fan out reads back %d workers for 2 requirements", len(found))
		}
		for _, worker := range found {
			if worker.Claim == job.ClaimOnRequirement(id, wanted[0]) {
				// The one that finished, read back with what it said. A settled worker has let its claim go,
				// so a query that asked who still holds it would find nothing and the stage would never
				// close.
				if !strings.Contains(worker.Answer, "Failing 1: TestItFails") {
					t.Fatalf("the worker that answered reads back with %q", worker.Answer)
				}
				if worker.Phase != job.PhaseDone {
					t.Fatalf("the worker that answered reads back as %q", worker.Phase)
				}
			}
			if worker.Parent != id {
				t.Fatalf("a worker reads back under %q, want %q", worker.Parent, id)
			}
		}

		// A claim nothing holds answers with nothing rather than with everything.
		none, err := s.JobsClaiming(ctx, workspace, []string{"nobody claims this"})
		if err != nil {
			t.Fatalf("JobsClaiming: %v", err)
		}
		if len(none) != 0 {
			t.Fatalf("a claim nothing holds answered with %d jobs", len(none))
		}
	})
}

// waitingToWriteItsTests is a job whose list a person accepted: past the reading, past the list, and
// owing failing tests for every requirement on it.
func waitingToWriteItsTests(t *testing.T, s store.Store, workspace, project string) string {
	t.Helper()
	ctx := context.Background()
	id := waitingToAcceptTheList(t, s, workspace, project)
	if _, err := s.AcceptJobDesign(ctx, id, toldEvent(id, workspace, project)); err != nil {
		t.Fatalf("AcceptJobDesign: %v", err)
	}
	return id
}

// testedEvent is the record that lands with a job's requirements becoming failing tests.
func testedEvent(id, workspace, project string) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventTested, Job: id, Workspace: workspace, Project: project,
		Detail: "2 requirements became 2 failing tests",
	}
}
