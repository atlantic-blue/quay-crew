package job_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
)

// The requirements a person accepted become failing tests, written by workers that never see an
// implementation, several at once.

// aRedSuite is the record of that stage as the system keeps it, used here and by the stages either
// side of it: a job past this point carries one, and the plan gate reads it.
const aRedSuite = "Requirement 1: a person pastes a link on the command line and gets the text back\n" +
	"Ran 1: 12\n" +
	"Fails 1: TestPastingALinkPrintsTheTranscript\n" +
	"Requirement 2: a person opens the same transcript in a browser and shares the address\n" +
	"Ran 2: 9\n" +
	"Fails 2: TestTheTranscriptPageRendersAtItsAddress"

// aReport is what one worker answers with: the requirement it held, the run it made, and the test
// that fails now.
func aReport(requirement int) string {
	return fmt.Sprintf("I wrote the tests and ran the suite.\n\nRequirement: %d\nRan: 12\n"+
		"Failing 1: TestRequirement%dFailsUntilItIsBuilt", requirement, requirement)
}

// testingJob is a job whose list a person accepted, which is what makes it owe failing tests.
func testingJob() *job.Job {
	one := listingJob()
	one.Design = job.DesignText(job.DesignIn(aListReply))
	one.DesignAccepted = true
	return one
}

// The requirement list is the list a person accepted, and there is no second record of it.
func TestTheAcceptedListIsTheRequirementList(t *testing.T) {
	one := testingJob()
	wanted := job.RequirementsOf(one)
	if len(wanted) != 2 {
		t.Fatalf("the job has %d requirements, want 2", len(wanted))
	}
	if wanted[0].Number != 1 || !strings.Contains(wanted[0].Text, "pastes a link") ||
		!strings.Contains(wanted[0].Shown, "prints in the terminal") {
		t.Fatalf("requirement 1 reads %+v", wanted[0])
	}

	// A list nobody accepted is a proposal. Writing tests against it would spend a session on every
	// line a person is about to change.
	one.DesignAccepted = false
	if got := job.RequirementsOf(one); got != nil {
		t.Fatalf("an unaccepted list carries %d requirements, want none", len(got))
	}
}

// The fan out itself: one worker for each requirement, each holding its own requirement and told
// nothing about the others.
func TestOneWorkerPerRequirementAndEachHoldsItsOwn(t *testing.T) {
	one := testingJob()
	wanted := job.RequirementsOf(one)
	workers := job.TestWorkers(one, wanted)
	if len(workers) != len(wanted) {
		t.Fatalf("%d requirements became %d workers", len(wanted), len(workers))
	}

	claims := map[string]bool{}
	for i, worker := range workers {
		if worker.Parent != one.ID || worker.Depth != one.Depth+1 {
			t.Fatalf("worker %d hangs under %q at depth %d", i, worker.Parent, worker.Depth)
		}
		if worker.Product != one.Product {
			t.Fatalf("worker %d serves %q, and the job serves %q", i, worker.Product, one.Product)
		}
		if worker.Claim == "" {
			t.Fatalf("worker %d claims nothing, so a second one could write the same requirement", i)
		}
		if claims[worker.Claim] {
			t.Fatalf("two workers claim %q", worker.Claim)
		}
		claims[worker.Claim] = true
		// Its own requirement, and no other. A worker holding the whole list writes a little of each and
		// the fan out buys nothing.
		mine, others := wanted[i], 0
		for _, requirement := range wanted {
			if requirement.Number != mine.Number && strings.Contains(worker.Brief, requirement.Text) {
				others++
			}
		}
		if others > 0 || !strings.Contains(worker.Brief, mine.Text) {
			t.Fatalf("worker %d was given %d other requirements as well as its own", i, others)
		}
		if !strings.Contains(worker.Brief, "Do not implement") {
			t.Fatalf("worker %d was not told to leave the implementation alone: %q", i, worker.Brief)
		}
		if worker.ID == one.ID || worker.ID == "" {
			t.Fatalf("worker %d has the identifier %q", i, worker.ID)
		}
	}
}

// The claim is the system's to write, and it is the same string on every reading of the same row.
// A claim worked out twice that came out twice differently would refuse nothing.
func TestTheClaimOnARequirementIsDerivedAndStable(t *testing.T) {
	one := testingJob()
	wanted := job.RequirementsOf(one)
	first := job.ClaimOnRequirement(one.ID, wanted[0])
	if first != job.ClaimOnRequirement(one.ID, wanted[0]) {
		t.Fatal("the claim on one requirement reads differently on a second reading")
	}
	if first == job.ClaimOnRequirement(one.ID, wanted[1]) {
		t.Fatal("two requirements of one job claim the same piece of work")
	}
	if first == job.ClaimOnRequirement("job-2", wanted[0]) {
		t.Fatal("two jobs claim the same piece of work for their own first requirement")
	}
	if len(first) > job.ClaimLimit {
		t.Fatalf("the claim is %d bytes and the ceiling is %d", len(first), job.ClaimLimit)
	}
}

// A run that executed nothing, and a run where everything passed, both read as success everywhere
// else in this system. Here they are failures, because a test that passes before anything is built
// asserts nothing and a suite that found no tests never ran one.
func TestASuiteThatRanNothingAndOneThatPassedAreBothRefused(t *testing.T) {
	for _, one := range []struct {
		name, reply, says string
	}{
		{"nothing ran", "Requirement: 1\nRan: 0\nFailing 1: TestNothing", "finds nothing to execute"},
		{"nothing failed", "Requirement: 1\nRan: 12", "none of them failed"},
		{"no requirement", "Ran: 12\nFailing 1: TestOne", "does not say which requirement"},
		{"no run at all", "Requirement: 1\nFailing 1: TestOne", "how many tests the run executed"},
		{"more failures than tests", "Requirement: 1\nRan: 1\nFailing 1: A\nFailing 2: B",
			"2 failing tests out of 1"},
	} {
		t.Run(one.name, func(t *testing.T) {
			got, err := job.ReadTestReport(one.reply)
			if err == nil {
				t.Fatalf("%q was read as a report of %+v", one.reply, got)
			}
			if !strings.Contains(err.Error(), one.says) {
				t.Fatalf("the refusal is %q, want it to say %q", err, one.says)
			}
		})
	}

	report, err := job.ReadTestReport(aReport(2))
	if err != nil {
		t.Fatalf("ReadTestReport: %v", err)
	}
	if report.Requirement != 2 || report.Ran != 12 || len(report.Failing) != 1 {
		t.Fatalf("the report reads %+v", report)
	}
}

// The check that closes the stage. Every requirement has a failing test, or the stage is not done,
// and a requirement whose worker died is named rather than passed over.
func TestTheStageIsNotDoneUntilEveryRequirementHasAFailingTest(t *testing.T) {
	wanted := job.RequirementsOf(testingJob())
	full := map[int]job.TestReport{
		1: {Requirement: 1, Ran: 12, Failing: []string{"TestOne"}},
		2: {Requirement: 2, Ran: 9, Failing: []string{"TestTwo"}},
	}
	if err := job.TestsRed(wanted, full); err != nil {
		t.Fatalf("a red suite covering every requirement was refused: %v", err)
	}

	short := map[int]job.TestReport{1: full[1]}
	err := job.TestsRed(wanted, short)
	if err == nil {
		t.Fatal("a suite covering one of two requirements closed the stage")
	}
	if !strings.Contains(err.Error(), "requirement 2") {
		t.Fatalf("the refusal is %q, want it to name requirement 2", err)
	}

	// An empty list is the other end of it. Nothing to write tests for is not a stage that passed.
	if err := job.TestsRed(nil, nil); err == nil {
		t.Fatal("a job with no requirements closed the test stage")
	}

	// And a report that is green, however it got onto the row.
	green := map[int]job.TestReport{1: full[1], 2: {Requirement: 2, Ran: 9}}
	if err := job.TestsRed(wanted, green); err == nil {
		t.Fatal("a requirement whose tests all passed closed the stage")
	}
}

// The record ties every failure to the requirement it came from, so a reader of the row can say
// which requirement any one of these tests holds.
func TestTheRecordNamesTheRequirementEachFailureCameFrom(t *testing.T) {
	wanted := job.RequirementsOf(testingJob())
	one := testingJob()
	one.Repository = "atlantic-blue/quay-krewe"
	wanted = job.RequirementsOf(one)
	kept := job.TestsText(one, wanted, map[int]job.TestReport{
		1: {Requirement: 1, Ran: 12, Failing: []string{"TestOne", "TestOneAgain"}},
		2: {Requirement: 2, Ran: 9, Failing: []string{"TestTwo"}},
	})
	for _, line := range []string{
		"Requirement 1: a person pastes a link", "Ran 1: 12", "Fails 1: TestOne",
		"Fails 1: TestOneAgain", "Requirement 2: a person opens the same transcript", "Fails 2: TestTwo",
	} {
		if !strings.Contains(kept, line) {
			t.Fatalf("the record is %q, want it to carry %q", kept, line)
		}
	}
	requirements, failing := job.TestsOn(kept)
	if requirements != 2 || failing != 3 {
		t.Fatalf("the record reads as %d requirements and %d failing tests", requirements, failing)
	}
}

// A worker that wrote for a requirement it does not hold is refused. A report filed under the wrong
// number would leave one requirement covered twice and another not at all.
func TestAWorkerThatReportedOnSomebodyElsesRequirementIsRefused(t *testing.T) {
	one := testingJob()
	wanted := job.RequirementsOf(one)
	workers := job.TestWorkers(one, wanted)
	workers[0].Answer, workers[0].Phase = aReport(2), job.PhaseDone

	report, why := job.ReportFrom(workers[:1], wanted[0])
	if why == "" {
		t.Fatalf("a worker reporting on another requirement was read as %+v", report)
	}
	if !strings.Contains(why, "reported on requirement 2 instead") {
		t.Fatalf("the refusal is %q", why)
	}

	// And a requirement nothing has written for is named rather than passed over.
	if _, why := job.ReportFrom(nil, wanted[1]); !strings.Contains(why, "nothing has written its tests") {
		t.Fatalf("a requirement with no worker reads %q", why)
	}
}

// The whole stage through the controller: the job fans out, waits while its workers write, and the
// suite that comes back closes it.
func TestAnAcceptedListFansOutIntoAWorkerForEachRequirement(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(testingJob())
	ctx := context.Background()

	controller.Tick(ctx)

	// The job itself is given no session. What runs is one worker for each requirement.
	got := kept.get(one.ID)
	if got.Phase != job.PhasePending || got.Session != "" {
		t.Fatalf("the job is %q in session %q, want pending with no session", got.Phase, got.Session)
	}
	if !strings.Contains(got.Reason, "of 2 requirements") {
		t.Fatalf("the job says %q, want it to say how many requirements are being written", got.Reason)
	}
	workers := kept.children(one.ID)
	if len(workers) != 2 {
		t.Fatalf("the fan out declared %d workers for 2 requirements", len(workers))
	}
	if got.Tests != "" {
		t.Fatalf("the job holds a record of failing tests before any worker ran: %q", got.Tests)
	}

	// A second tick declares nothing more. The claim each worker holds is what stops it, and this is
	// the case that would otherwise pay for a second session for every requirement.
	controller.Tick(ctx)
	if again := kept.children(one.ID); len(again) != 2 {
		t.Fatalf("a second tick left %d workers for 2 requirements", len(again))
	}

	// The workers run, each in its own session, and each answers with its own report.
	controller.Tick(ctx)
	for _, worker := range kept.children(one.ID) {
		if kept.get(worker.ID).Phase != job.PhaseRunning {
			t.Fatalf("worker %q is %q, want running", worker.Title, kept.get(worker.ID).Phase)
		}
	}
	for i, worker := range kept.children(one.ID) {
		plane.landsIn(job.SessionFor(worker.ID), landed(aReport(i+1)))
	}
	controller.Tick(ctx)

	// And the stage closes: the record lands, and it names the requirement each failure came from.
	got = kept.get(one.ID)
	if got.Tests == "" {
		t.Fatalf("the workers all answered and the job holds no record: %s", got.Reason)
	}
	for _, line := range []string{"Requirement 1:", "Fails 1: TestRequirement1", "Requirement 2:",
		"Fails 2: TestRequirement2"} {
		if !strings.Contains(got.Tests, line) {
			t.Fatalf("the record is %q, want it to carry %q", got.Tests, line)
		}
	}
	if got.Phase != job.PhasePending {
		t.Fatalf("the job is %q after its suite went red, want pending so it plans next", got.Phase)
	}
}

// The plan is what turns a red suite green, so it is asked for after the tests and never before.
func TestNoPlanIsAskedForUntilTheSuiteIsRed(t *testing.T) {
	one := testingJob()
	if !job.WaitingForItsTests(one) || job.WaitingForItsPlan(one) {
		t.Fatal("a job whose list was accepted owes a plan before it owes failing tests")
	}
	one.Tests = aRedSuite
	if job.WaitingForItsTests(one) || !job.WaitingForItsPlan(one) {
		t.Fatal("a job whose suite is red still owes tests, or owes no plan")
	}

	// A row written before this stage existed carries a plan and no record, and it is past this gate
	// rather than being sent back to the beginning of the work.
	older := testingJob()
	older.Plan, older.PlanApproved = "Step 1: read the issue", true
	if job.WaitingForItsTests(older) {
		t.Fatal("a job holding an approved plan was sent back to write its tests")
	}
}

// A worker that died leaves its requirement with nothing holding it, and that stops the job for a
// person rather than closing the stage on the requirements that did finish.
func TestARequirementWhoseWorkerDiedStopsTheJobForAPerson(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(testingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	controller.Tick(ctx)
	workers := kept.children(one.ID)
	plane.landsIn(job.SessionFor(workers[0].ID), landed(aReport(1)))
	plane.failsIn(job.SessionFor(workers[1].ID), "the sandbox went away")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseAsking {
		t.Fatalf("the job is %q, want asking: %s", got.Phase, got.Reason)
	}
	if got.Tests != "" {
		t.Fatalf("one requirement has no tests and the stage closed anyway: %q", got.Tests)
	}
	if !strings.Contains(got.Question, "requirement 2") {
		t.Fatalf("the question is %q, want it to name the requirement nothing holds", got.Question)
	}
}

// A list of one requirement is a fan out of one. It is the ordinary outcome of a list of one
// vertical, not a case to refuse.
func TestAListOfOneRequirementIsAFanOutOfOne(t *testing.T) {
	controller, kept, plane := aController(t)
	one := testingJob()
	one.Design = "Vertical 1: a person pastes a link on the command line and gets the text back\n" +
		"Shown 1: the transcript prints in the terminal for a link the person chooses"
	kept.add(one)
	ctx := context.Background()

	controller.Tick(ctx)
	controller.Tick(ctx)
	workers := kept.children(one.ID)
	if len(workers) != 1 {
		t.Fatalf("one requirement fanned out into %d workers", len(workers))
	}
	plane.landsIn(job.SessionFor(workers[0].ID), landed(aReport(1)))
	controller.Tick(ctx)

	if got := kept.get(one.ID); !strings.Contains(got.Tests, "Fails 1: TestRequirement1") {
		t.Fatalf("the record is %q", got.Tests)
	}
}

// A job whose accepted list carries nothing the system can read has nothing to write tests for, and
// that is a person's to decide rather than a stage to pass.
func TestAJobWithNoRequirementsStopsForAPerson(t *testing.T) {
	controller, kept, _ := aController(t)
	one := testingJob()
	one.Design = "I will build the thing we discussed"
	kept.add(one)
	ctx := context.Background()

	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseAsking {
		t.Fatalf("the job is %q, want asking", got.Phase)
	}
	if len(kept.children(one.ID)) != 0 {
		t.Fatal("a job with no requirements declared workers")
	}
	if !strings.Contains(got.Question, "no requirement") {
		t.Fatalf("the question is %q", got.Question)
	}
}

// The claim refusing a second worker for one requirement, at the store rather than in the arithmetic
// above it. This is the case two controllers ticking the same row produce.
func TestASecondWorkerForOneRequirementIsRefusedByTheClaim(t *testing.T) {
	controller, kept, _ := aController(t)
	one := kept.add(testingJob())
	ctx := context.Background()

	wanted := job.RequirementsOf(one)
	first := job.TestWorkers(one, wanted[:1])[0]
	if err := kept.CreateJob(ctx, first, declaredEvent(first)); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	second := job.TestWorkers(one, wanted[:1])[0]
	err := kept.CreateJob(ctx, second, declaredEvent(second))
	var held *job.Held
	if !errors.As(err, &held) {
		t.Fatalf("a second worker for requirement 1 was declared, and the store said %v", err)
	}
	if held.Holder != first.ID {
		t.Fatalf("the refusal names %q, and %q holds it", held.Holder, first.ID)
	}

	// So the fan out declares the one requirement that has no worker, and leaves the one that has.
	controller.Tick(ctx)
	workers := kept.children(one.ID)
	if len(workers) != 2 {
		t.Fatalf("the fan out left %d workers for 2 requirements", len(workers))
	}
}

// The double and the reader have to agree, because a double looser than the engine manufactures a
// green suite: every test about a job past its accepted list runs through this reply.
func TestTheDoubleAnswersAReportTheSystemCanRead(t *testing.T) {
	if model.TestAsk != job.TheTestAsk {
		t.Fatalf("the double watches for %q and the system asks with %q", model.TestAsk, job.TheTestAsk)
	}
	if model.TestMarker != job.TestMarker {
		t.Fatalf("the double marks a report with %q and the system reads %q",
			model.TestMarker, job.TestMarker)
	}
	// And it reports on the requirement it was handed rather than on one of its own, which is what the
	// stage refuses: a double that always said the same number would be refused for every worker but
	// the first.
	one := testingJob()
	for _, requirement := range job.RequirementsOf(one) {
		asked := job.WriteFailingTests(one, requirement)
		report, err := job.ReadTestReport(model.FakeTestReport(asked))
		if err != nil {
			t.Fatalf("the double answers a report the system refuses: %v", err)
		}
		if report.Requirement != requirement.Number {
			t.Fatalf("the double was handed requirement %d and reported on %d",
				requirement.Number, report.Requirement)
		}
	}
}

// A worker never fans out again. It is one part of a plan the job above it is still writing, so it
// runs its brief like any other declared job: a worker that entered this stage would declare workers
// of its own, and the whole tree would be nothing but test writers.
func TestAWorkerOfTheFanOutNeverFansOutItself(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(testingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	workers := kept.children(one.ID)
	for _, worker := range workers {
		if job.WaitingForItsTests(worker) {
			t.Fatalf("worker %q owes failing tests of its own", worker.Title)
		}
		if len(kept.children(worker.ID)) != 0 {
			t.Fatalf("worker %q declared workers of its own", worker.Title)
		}
	}

	// And the reason it does not, held on its own: a job under another is one part of a plan somebody
	// already approved, so it is outside these stages however far through them its row looks.
	under := testingJob()
	under.Parent, under.Depth = one.ID, 1
	if job.WaitingForItsTests(under) {
		t.Fatal("a job declared under another owes failing tests of its own")
	}

	controller.Tick(ctx)
	for _, worker := range workers {
		got := kept.get(worker.ID)
		if got.Session == "" {
			t.Fatalf("worker %q was given no session, so nothing writes its tests", worker.Title)
		}
		if len(kept.children(worker.ID)) != 0 {
			t.Fatalf("worker %q declared workers of its own once it started", worker.Title)
		}
	}
	if plane.sent() != len(workers) {
		t.Fatalf("the system was asked to run %d tasks for %d workers", plane.sent(), len(workers))
	}
}

// A worker says which requirement it holds in either shape: the number after the word, which is what
// the ask asks for, or the number the requirement itself is written under, which is how the ask
// states it. Both are the same claim, and refusing one of them would cost a task to find out.
func TestAReportNamesItsRequirementInEitherShape(t *testing.T) {
	for _, one := range []struct {
		name, reply string
	}{
		{"the number after the word", "Requirement: 3\nRan: 12\nFailing 1: TestOne"},
		{"the number it is written under",
			"Requirement 3: a person pastes a link\nRan: 12\nFailing 1: TestOne"},
	} {
		t.Run(one.name, func(t *testing.T) {
			report, err := job.ReadTestReport(one.reply)
			if err != nil {
				t.Fatalf("ReadTestReport: %v", err)
			}
			if report.Requirement != 3 {
				t.Fatalf("the report reads as requirement %d, want 3", report.Requirement)
			}
		})
	}
}
