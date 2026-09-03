package job_test

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
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

// The fan out itself: one run for each requirement, each holding its own requirement and told
// nothing about the others.
func TestOneRunPerRequirementAndEachHoldsItsOwn(t *testing.T) {
	one := testingJob()
	wanted := job.RequirementsOf(one)
	runs := job.TestExecutions(one, wanted)
	if len(runs) != len(wanted) {
		t.Fatalf("%d requirements became %d runs", len(wanted), len(runs))
	}

	claims := map[string]bool{}
	for i, run := range runs {
		if run.Job != one.ID || run.Stage != job.StageTest {
			t.Fatalf("run %d belongs to job %q in stage %q", i, run.Job, run.Stage)
		}
		if run.Number != wanted[i].Number {
			t.Fatalf("run %d is for number %d, want %d", i, run.Number, wanted[i].Number)
		}
		if run.Claim == "" {
			t.Fatalf("run %d claims nothing, so a second one could write the same requirement", i)
		}
		if claims[run.Claim] {
			t.Fatalf("two runs claim %q", run.Claim)
		}
		claims[run.Claim] = true
		// Its own requirement, and no other. A session holding the whole list writes a little of each
		// and the fan out buys nothing. What it is asked is built from the job when the task is sent,
		// so that is where this reads it.
		mine, others := wanted[i], 0
		asked := job.WriteFailingTests(one, mine)
		for _, requirement := range wanted {
			if requirement.Number != mine.Number && strings.Contains(asked, requirement.Text) {
				others++
			}
		}
		if others > 0 || !strings.Contains(asked, mine.Text) {
			t.Fatalf("run %d was given %d other requirements as well as its own", i, others)
		}
		if !strings.Contains(asked, "Do not implement") {
			t.Fatalf("run %d was not told to leave the implementation alone: %q", i, asked)
		}
		if run.ID == one.ID || run.ID == "" {
			t.Fatalf("run %d has the identifier %q", i, run.ID)
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
		{"nothing ran", "Ran: 0\nFailing 1: TestNothing", "finds nothing to execute"},
		{"nothing failed", "Ran: 12", "none of them failed"},
		{"nothing about the run at all", "I wrote the tests", "how many tests the run executed"},
		{"no run at all", "Failing 1: TestOne", "how many tests the run executed"},
		{"more failures than tests", "Ran: 1\nFailing 1: A\nFailing 2: B",
			"2 failing tests out of 1"},
	} {
		t.Run(one.name, func(t *testing.T) {
			got, err := job.ReadTestReport(one.reply, 1)
			if err == nil {
				t.Fatalf("%q was read as a report of %+v", one.reply, got)
			}
			if !strings.Contains(err.Error(), one.says) {
				t.Fatalf("the refusal is %q, want it to say %q", err, one.says)
			}
		})
	}

	report, err := job.ReadTestReport(aReport(2), 2)
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
	one := testingJob()
	one.Repository = "atlantic-blue/quay-krewe"
	wanted := job.RequirementsOf(one)
	kept := job.TestsText(one, wanted, map[int]job.TestReport{
		1: {Requirement: 1, Ran: 12, Failing: []string{"TestOne", "TestOneAgain"}},
		2: {Requirement: 2, Ran: 9, Failing: []string{"TestTwo"}},
	})
	for _, line := range []string{
		"Requirement 1: a person pastes a link", "Ran 1: 12", "Fails 1: TestOne",
		"Branch 1: " + job.BranchFor(one, wanted[0]), "Branch 2: " + job.BranchFor(one, wanted[1]),
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

// The requirement a report is filed under comes off the run, so a report that names none is read
// just the same. The stage wrote that number when it made the run, and the worker is asked for the
// run instead: how many tests it executed, and which of them fail.
func TestAReportThatNamesNoRequirementIsReadOffTheRow(t *testing.T) {
	one := testingJob()
	wanted := job.RequirementsOf(one)
	runs := job.TestExecutions(one, wanted)
	// The report the ask now asks for, which names no requirement anywhere in it.
	runs[1].Answer, runs[1].Phase = "I wrote the tests and ran the suite.\n\nRan: 9\n"+
		"Failing 1: TestTheTranscriptPageRendersAtItsAddress", job.PhaseDone

	report, why := job.ReportFrom(one, runs[1:2], wanted[1])
	if why != "" {
		t.Fatalf("a report that names no requirement is refused: %s", why)
	}
	if report.Requirement != 2 {
		t.Fatalf("the report is filed under requirement %d, and the run it came from says 2",
			report.Requirement)
	}
	if report.Ran != 9 || len(report.Failing) != 1 {
		t.Fatalf("the report reads %+v", report)
	}
	if report.NamedAnotherRequirement() {
		t.Fatal("a report that named no requirement reads as naming another one")
	}

	// And the ask is what makes that true of a real worker: nothing in it asks for the number.
	asked := job.WriteFailingTests(one, wanted[1])
	for _, line := range []string{"Requirement: 2", "Requirement: <n>", "the number of the requirement"} {
		if strings.Contains(asked, line) {
			t.Fatalf("the worker is asked for %q, which the row already holds:\n%s", line, asked)
		}
	}
	for _, needed := range []string{"Ran: how many tests the run executed", "Failing 1:"} {
		if !strings.Contains(asked, needed) {
			t.Fatalf("the worker is not asked for %q:\n%s", needed, asked)
		}
	}

	// And a requirement nothing has written for is named rather than passed over.
	if _, why := job.ReportFrom(one, nil, wanted[1]); !strings.Contains(why, "nothing has written its tests") {
		t.Fatalf("a requirement with no run reads %q", why)
	}
}

// A reply that names a requirement the row does not is a fault a person reads in the record. It does
// not move the tests to that other requirement: the row is what the stage wrote, and a report filed
// under a number a worker typed would leave one requirement covered twice and another not at all.
func TestAReplyNamingAnotherRequirementIsAFaultAndNotASourceOfTruth(t *testing.T) {
	one := testingJob()
	wanted := job.RequirementsOf(one)
	runs := job.TestExecutions(one, wanted)
	if runs[0].Number != 1 {
		t.Fatalf("the run of the first requirement carries number %d", runs[0].Number)
	}
	runs[0].Answer, runs[0].Phase = aReport(2), job.PhaseDone

	report, why := job.ReportFrom(one, runs[:1], wanted[0])
	if why != "" {
		t.Fatalf("a run whose reply named another requirement is refused: %s", why)
	}
	if report.Requirement != 1 {
		t.Fatalf("the report is filed under requirement %d, and the run it came from says 1",
			report.Requirement)
	}
	if report.Named != 2 || !report.NamedAnotherRequirement() {
		t.Fatalf("the report reads %+v, want it to hold the requirement the reply named as a fault",
			report)
	}

	// What a person reads: the failing test stays under requirement 1, and the record says out loud
	// that the run's own words disagreed with its row.
	kept := job.TestsText(one, wanted[:1], map[int]job.TestReport{1: report})
	for _, line := range []string{
		"Requirement 1: " + wanted[0].Text,
		"Fails 1: TestRequirement2FailsUntilItIsBuilt",
		"Fault 1: the run holding this requirement named requirement 2 in its report, and the row it " +
			"ran under says 1",
	} {
		if !strings.Contains(kept, line) {
			t.Fatalf("the record is %q, want it to carry %q", kept, line)
		}
	}
	if strings.Contains(kept, "Requirement 2:") || strings.Contains(kept, "Fails 2:") {
		t.Fatalf("the record filed this run's tests under requirement 2: %q", kept)
	}
	// The fault is a line for a person and not a requirement or a failure, so the size of the record
	// does not move.
	if requirements, failing := job.TestsOn(kept); requirements != 1 || failing != 1 {
		t.Fatalf("the record reads as %d requirements and %d failing tests", requirements, failing)
	}
	// And the stage closes on it. The tests are red for requirement 1, which is what this stage is for.
	if err := job.TestsRed(wanted[:1], map[int]job.TestReport{1: report}); err != nil {
		t.Fatalf("a red suite whose worker misnamed its requirement is refused: %v", err)
	}
}

// Every example in a refusal carries the run's own number, and no refusal fabricates one. A worker on
// requirement 4 that is told to write the number two is told something wrong, and it is right to be
// confused: seven of eleven workers were refused for this on 3 September 2026.
func TestNoRefusalNamesANumberThatIsNotTheRunsOwn(t *testing.T) {
	one := testingJob()
	one.Design = "Vertical 1: a person pastes a link and gets the text back\n" +
		"Shown 1: the transcript prints in the terminal for a link the person chooses\n" +
		"Vertical 2: a person opens that transcript in a browser and shares the address\n" +
		"Shown 2: the page renders the transcript at an address the person can send on\n" +
		"Vertical 3: a person searches their own transcripts for a phrase\n" +
		"Shown 3: the matches print with the link each one came from\n" +
		"Vertical 4: a person exports a transcript as a file they can keep\n" +
		"Shown 4: the file lands in the folder the person chose and opens\n"
	wanted := job.RequirementsOf(one)
	if len(wanted) != 4 {
		t.Fatalf("the list reads as %d requirements, want 4", len(wanted))
	}
	// The fourth, because a run for requirement 4 that is told to write the number two is the fault
	// this measures: a literal in the refusal reads as an instruction.
	fourth := wanted[3:]
	if fourth[0].Number != 4 {
		t.Fatalf("the fourth requirement is numbered %d", fourth[0].Number)
	}

	// Every shape of reply this stage refuses, read for a run that holds requirement 4.
	for _, reply := range []string{
		"I wrote the tests", "Ran: 0\nFailing 1: TestOne", "Ran: 12", "Ran: lots\nFailing 1: TestOne",
		"Ran: 1\nFailing 1: A\nFailing 2: B",
	} {
		runs := job.TestExecutions(one, fourth)
		runs[0].Answer, runs[0].Phase = reply, job.PhaseDone
		_, why := job.ReportFrom(one, runs, fourth[0])
		if why == "" {
			t.Fatalf("%q was read as a report", reply)
		}
		// What a person is actually handed, which is the question the stage puts to them.
		asked := job.NoRedSuite(why)
		if !strings.Contains(asked, "requirement 4") {
			t.Fatalf("the question does not say which requirement it is about:\n%s", asked)
		}
		for _, number := range numbersBesideAReportWord(asked) {
			if number != 4 {
				t.Fatalf("the question tells a worker on requirement 4 to write the number %d:\n%s",
					number, asked)
			}
		}
	}
}

// numbersBesideAReportWord is every number this prose writes next to the word for a requirement or
// for the count of a run, which is the shape an example line in a refusal has.
//
// "Failing 1:" is left out on purpose. That number is which failure is being named and it is the
// same for every run, so it is the one number in these refusals that is not a claim about the
// requirement.
func numbersBesideAReportWord(said string) []int {
	var found []int
	for _, one := range regexp.MustCompile(`(?i)(requirement|ran)[ :]+(\d+)`).
		FindAllStringSubmatch(said, -1) {
		number, err := strconv.Atoi(one[2])
		if err != nil {
			continue
		}
		found = append(found, number)
	}
	return found
}

// The whole stage through the controller: the job fans out, waits while its workers write, and the
// suite that comes back closes it.
func TestAnAcceptedListFansOutIntoAWorkerForEachRequirement(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(testingJob())
	ctx := context.Background()

	controller.Tick(ctx)

	// The job itself is given no session. What runs is one session for each requirement.
	got := kept.get(one.ID)
	if got.Phase != job.PhasePending || got.Session != "" {
		t.Fatalf("the job is %q in session %q, want pending with no session", got.Phase, got.Session)
	}
	if !strings.Contains(got.Reason, "of 2 requirements") {
		t.Fatalf("the job says %q, want it to say how many requirements are being written", got.Reason)
	}
	runs := kept.executionsIn(one.ID, job.StageTest)
	if len(runs) != 2 {
		t.Fatalf("the fan out wrote %d runs for 2 requirements", len(runs))
	}
	if got.Tests != "" {
		t.Fatalf("the job holds a record of failing tests before any run happened: %q", got.Tests)
	}

	// A second tick writes nothing more. The claim each run holds is what stops it, and this is
	// the case that would otherwise pay for a second session for every requirement.
	controller.Tick(ctx)
	if again := kept.executionsIn(one.ID, job.StageTest); len(again) != 2 {
		t.Fatalf("a second tick left %d runs for 2 requirements", len(again))
	}

	// The runs happen, each in its own session, and each answers with its own report.
	controller.Tick(ctx)
	for _, run := range kept.executionsIn(one.ID, job.StageTest) {
		if kept.getRun(run.ID).Phase != job.PhaseRunning {
			t.Fatalf("the run of number %d is %q, want running", run.Number, kept.getRun(run.ID).Phase)
		}
	}
	for i, run := range kept.executionsIn(one.ID, job.StageTest) {
		plane.landsIn(job.SessionForExecution(run), landed(aReport(i+1)))
	}
	controller.Tick(ctx)

	// And the stage closes: the record lands, and it names the requirement each failure came from.
	got = kept.get(one.ID)
	if got.Tests == "" {
		t.Fatalf("every run answered and the job holds no record: %s", got.Reason)
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

// A run that died leaves its requirement with nothing holding it, and that stops the job for a
// person rather than closing the stage on the requirements that did finish.
func TestARequirementWhoseRunDiedStopsTheJobForAPerson(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(testingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	controller.Tick(ctx)
	runs := kept.executionsIn(one.ID, job.StageTest)
	plane.landsIn(job.SessionForExecution(runs[0]), landed(aReport(1)))
	plane.failsIn(job.SessionForExecution(runs[1]), "the sandbox went away")
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
	runs := kept.executionsIn(one.ID, job.StageTest)
	if len(runs) != 1 {
		t.Fatalf("one requirement fanned out into %d runs", len(runs))
	}
	plane.landsIn(job.SessionForExecution(runs[0]), landed(aReport(1)))
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
	if len(kept.executionsOf(one.ID)) != 0 {
		t.Fatal("a job with no requirements ran a stage")
	}
	if !strings.Contains(got.Question, "no requirement") {
		t.Fatalf("the question is %q", got.Question)
	}
}

// The claim refusing a second run for one requirement, at the store rather than in the arithmetic
// above it. This is the case two controllers ticking the same row produce.
func TestASecondRunForOneRequirementIsRefusedByTheClaim(t *testing.T) {
	controller, kept, _ := aController(t)
	one := kept.add(testingJob())
	ctx := context.Background()

	wanted := job.RequirementsOf(one)
	first := job.TestExecutions(one, wanted[:1])[0]
	if err := kept.CreateExecution(ctx, first, ranEvent(one, first)); err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	second := job.TestExecutions(one, wanted[:1])[0]
	err := kept.CreateExecution(ctx, second, ranEvent(one, second))
	var held *job.Held
	if !errors.As(err, &held) {
		t.Fatalf("a second run for requirement 1 was written, and the store said %v", err)
	}
	if held.Holder != first.ID {
		t.Fatalf("the refusal names %q, and %q holds it", held.Holder, first.ID)
	}

	// So the fan out writes the one requirement that has no run, and leaves the one that has.
	controller.Tick(ctx)
	runs := kept.executionsOf(one.ID)
	if len(runs) != 2 {
		t.Fatalf("the fan out left %d runs for 2 requirements", len(runs))
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
	// And it names no requirement on a report line, because the ask asks for none: the number comes
	// off the run. A double that stated one would prove the reading of a line the stage no longer
	// asks any worker to write.
	one := testingJob()
	for _, requirement := range job.RequirementsOf(one) {
		asked := job.WriteFailingTests(one, requirement)
		answered := model.FakeTestReport(asked)
		if strings.Contains(answered, "\nRequirement:") {
			t.Fatalf("the double names its requirement on a report line, which nothing asks for:\n%s",
				answered)
		}
		report, err := job.ReadTestReport(answered, requirement.Number)
		if err != nil {
			t.Fatalf("the double answers a report the system refuses: %v", err)
		}
		if report.Requirement != requirement.Number || report.Named != 0 {
			t.Fatalf("the report is filed under requirement %d and names %d",
				report.Requirement, report.Named)
		}
	}
}

// A run never fans out again, and it cannot: a stage fans out from a job, and a run is not one. It
// leaves no row in the jobs table at all, so nothing reads it as owing a stage of its own.
func TestARunOfTheFanOutIsNoJobAtAll(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(testingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	runs := kept.executionsOf(one.ID)
	if len(runs) != 2 {
		t.Fatalf("the fan out wrote %d runs for 2 requirements", len(runs))
	}
	// Nothing a person declared, and nothing under the job either. The jobs table holds the one row
	// somebody wrote and no other.
	if declared := kept.all(); len(declared) != 1 || declared[0].ID != one.ID {
		t.Fatalf("the jobs table holds %d rows, and one job was declared", len(declared))
	}
	if caused := kept.caused(one.ID); len(caused) != 0 {
		t.Fatalf("the fan out wrote %d jobs for the job it ran for, and a run is not a job", len(caused))
	}

	// And the reason a step of a flow run never fans out, held on its own: the graph a person imported
	// is its plan, so it is outside these stages however far through them its row looks.
	step := testingJob()
	step.Run = "a-run"
	if job.WaitingForItsTests(step) {
		t.Fatal("a step of a flow run owes failing tests of its own")
	}

	controller.Tick(ctx)
	for _, run := range runs {
		got := kept.getRun(run.ID)
		if got.Session == "" {
			t.Fatalf("run %d was given no session, so nothing writes its tests", run.Number)
		}
	}
	if plane.sent() != len(runs) {
		t.Fatalf("the system was asked to run %d tasks for %d runs", plane.sent(), len(runs))
	}
}

// A reply that names a requirement is read in either shape: the number after the word, or the number
// the requirement itself is written under, which is how the ask states it. What that number is for is
// the comparison with the row, so both shapes have to reach it.
func TestAReplyNamingARequirementIsReadInEitherShape(t *testing.T) {
	for _, one := range []struct {
		name, reply string
	}{
		{"the number after the word", "Requirement: 3\nRan: 12\nFailing 1: TestOne"},
		{"the number it is written under",
			"Requirement 3: a person pastes a link\nRan: 12\nFailing 1: TestOne"},
	} {
		t.Run(one.name, func(t *testing.T) {
			report, err := job.ReadTestReport(one.reply, 3)
			if err != nil {
				t.Fatalf("ReadTestReport: %v", err)
			}
			if report.Named != 3 {
				t.Fatalf("the reply reads as naming requirement %d, want 3", report.Named)
			}
			if report.NamedAnotherRequirement() {
				t.Fatal("a reply naming the requirement its row holds reads as a fault")
			}

			// And the same two shapes on a run holding another requirement, which is the fault.
			elsewhere, err := job.ReadTestReport(one.reply, 4)
			if err != nil {
				t.Fatalf("ReadTestReport: %v", err)
			}
			if elsewhere.Requirement != 4 || !elsewhere.NamedAnotherRequirement() {
				t.Fatalf("the report reads %+v, want it filed under 4 and marked as a fault", elsewhere)
			}
		})
	}
}

// A run that answered without naming its pull request is asked again, once, before anything is
// landed.
//
// This is the correction that used to cost one task. Its tests are in a sandbox that will go, and
// the address is the only thing that says where they went, so a run that forgot it holds nothing the
// build stage can read. Without the ask the stage refuses that requirement and puts it to a person,
// which turns a job that corrected itself into a job waiting on somebody.
func TestARunThatNamedNoPullRequestIsAskedAgainBeforeAnythingIsLanded(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(inARepositoryFor(testingJob()))
	ctx := context.Background()

	controller.Tick(ctx)
	controller.Tick(ctx)
	runs := kept.executionsIn(one.ID, job.StageTest)
	if len(runs) != 2 {
		t.Fatalf("the fan out wrote %d runs for 2 requirements", len(runs))
	}
	// A report the system can read, with no address anywhere in it.
	plane.landsIn(job.SessionForExecution(runs[0]), landed(aReport(1)))
	controller.Tick(ctx)

	// Nothing landed. The run is still running, and the session was asked one more thing.
	if got := kept.getRun(runs[0].ID); job.Terminal(got.Phase) {
		t.Fatalf("the run landed as %q without ever being asked where the work went", got.Phase)
	}
	asked := plane.asked(job.SessionForExecution(runs[0]))
	if len(asked) != 2 {
		t.Fatalf("the session was sent %d tasks, want the work and then the ask for the address", len(asked))
	}
	for _, phrase := range []string{"named no pull request", "state its outcome"} {
		if !strings.Contains(asked[1], phrase) {
			t.Fatalf("the second ask does not say %q:\n%s", phrase, asked[1])
		}
	}

	// The session answers with the address, and the run lands with it.
	plane.landsIn(job.SessionForExecution(runs[0]),
		landed(aReport(1)+"\n\nhttps://github.com/atlantic-blue/quay-krewe/pull/700"))
	controller.Tick(ctx)
	got := kept.getRun(runs[0].ID)
	if got.Phase != job.PhaseDone {
		t.Fatalf("the run is %q after answering with its address: %s", got.Phase, got.Reason)
	}
	if got.PullRequest != "https://github.com/atlantic-blue/quay-krewe/pull/700" {
		t.Fatalf("the run reads back naming %q", got.PullRequest)
	}

	// And it is asked once. A run that answers a second time without an address stops with where its
	// work actually is, rather than being asked for ever.
	plane.landsIn(job.SessionForExecution(runs[1]), landed(aReport(2)))
	controller.Tick(ctx)
	plane.landsIn(job.SessionForExecution(runs[1]), landed(aReport(2)))
	controller.Tick(ctx)
	second := kept.getRun(runs[1].ID)
	if second.Phase != job.PhaseStopped {
		t.Fatalf("the run is %q after two answers with no address", second.Phase)
	}
	if !strings.Contains(second.Reason, "no answer named a pull request") {
		t.Fatalf("the run stopped saying %q", second.Reason)
	}
}

// inARepositoryFor is a job that works somewhere its runs can push to, so the address is asked for.
func inARepositoryFor(one *job.Job) *job.Job {
	one.Repository = "atlantic-blue/quay-krewe"
	return one
}
