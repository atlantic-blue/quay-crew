package job_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
)

// The failing tests become an implementation, built by workers that may read every test and change
// none of them, several at once.

// buildingJob is a job whose suite is red and whose plan a person approved, which is what makes it
// owe a build.
func buildingJob() *job.Job {
	one := testingJob()
	one.Tests = aRedSuite
	one.Plan = "Step 1: build the two verticals"
	one.PlanApproved = true
	return one
}

// aBuildReport is what one worker answers with: the vertical it held, the run it made, the test that
// passes now, and the file it wrote to make it pass.
func aBuildReport(vertical int, passing string) string {
	return fmt.Sprintf("I built vertical %d and ran the suite.\n\nVertical: %d\nRan: 14\nRed: 0\n"+
		"Passing 1: %s\nChanged 1: internal/transcript/vertical%d.go", vertical, vertical, passing, vertical)
}

// theFailures are the tests the test stage recorded as failing, by the requirement each was written
// for. A build report has to name them, which is the link between the two stages.
var theFailures = map[int]string{
	1: "TestPastingALinkPrintsTheTranscript",
	2: "TestTheTranscriptPageRendersAtItsAddress",
}

// The gate itself: the build stage stands behind the plan, and every stage before it.
func TestAJobOwesABuildOnlyOnceItsPlanIsApprovedAndItsSuiteIsRed(t *testing.T) {
	one := buildingJob()
	if !job.WaitingForItsBuild(one) {
		t.Fatal("a job with an approved plan and a red suite owes no build")
	}

	// Behind the plan. A job whose plan nobody approved is asked for one first, because the plan is
	// the steps that turn these tests green.
	unapproved := buildingJob()
	unapproved.PlanApproved = false
	if job.WaitingForItsBuild(unapproved) {
		t.Fatal("a job whose plan nobody approved is being built")
	}

	// Behind the tests. A row written before the test stage existed carries a plan and no suite, and
	// fanning it out against tests nobody wrote would build against nothing.
	older := buildingJob()
	older.Tests = ""
	if job.WaitingForItsBuild(older) {
		t.Fatal("a job with no failing tests is being built against them")
	}

	// And a job that is built owes nothing more. Without this the same verticals fan out again on
	// every tick, because a finished worker has let its claim go.
	built := buildingJob()
	built.Build = "Vertical 1: a person pastes a link\nRan 1: 14\nPasses 1: TestItPasses"
	if job.WaitingForItsBuild(built) {
		t.Fatal("a job that is already built is being built again")
	}
}

// One worker for each vertical, each holding its own claim, each told the tests it owns and the
// boundary it works under.
func TestOneWorkerPerVerticalAndEachHoldsItsOwn(t *testing.T) {
	one := buildingJob()
	wanted := job.RequirementsOf(one)
	workers := job.BuildWorkers(one, wanted, job.FailuresOn(one.Tests))
	if len(workers) != 2 {
		t.Fatalf("a list of 2 verticals made %d workers", len(workers))
	}

	claims := map[string]bool{}
	for at, worker := range workers {
		vertical := wanted[at]
		if worker.Claim != job.ClaimOnBuild(one.ID, vertical) {
			t.Fatalf("worker %d holds %q", vertical.Number, worker.Claim)
		}
		if claims[worker.Claim] {
			t.Fatalf("two workers hold %q, so both would build one vertical", worker.Claim)
		}
		claims[worker.Claim] = true
		if worker.Parent != one.ID || worker.Depth != one.Depth+1 {
			t.Fatalf("worker %d is under %q at depth %d", vertical.Number, worker.Parent, worker.Depth)
		}
		// Under the boundary, and that is what puts the session under the gate: the system reads this
		// field when it sends the task. A worker without it would be under advice.
		if !worker.Building {
			t.Fatalf("worker %d builds outside the boundary", vertical.Number)
		}
		if !strings.Contains(worker.Title, fmt.Sprintf("vertical %d", vertical.Number)) {
			t.Fatalf("the worker is called %q, and a refused claim names it", worker.Title)
		}
		// Its own vertical and nothing else, the tests it owns by name, and the boundary said in the
		// brief as well as held by the gate.
		for _, phrase := range []string{
			job.TheBuildAsk, vertical.Text, theFailures[vertical.Number],
			"You may not change one", "Build this vertical only",
		} {
			if !strings.Contains(worker.Brief, phrase) {
				t.Fatalf("the brief of worker %d does not say %q: %s", vertical.Number, phrase, worker.Brief)
			}
		}
		// And not the other vertical's tests, or the fan out buys nothing.
		other := 3 - vertical.Number
		if strings.Contains(worker.Brief, theFailures[other]) {
			t.Fatalf("worker %d was given the tests of vertical %d", vertical.Number, other)
		}
	}
}

// The claim is written by the system and it is the same string on the second reading of the same
// row, or a second controller would declare a second worker for work the first is doing.
func TestTheClaimOnAVerticalIsDerivedAndStable(t *testing.T) {
	one := buildingJob()
	wanted := job.RequirementsOf(one)
	first := job.ClaimOnBuild(one.ID, wanted[0])
	if first != job.ClaimOnBuild(one.ID, wanted[0]) {
		t.Fatal("the claim on one vertical is not the same string twice")
	}
	if first == job.ClaimOnBuild(one.ID, wanted[1]) {
		t.Fatal("two verticals of one job hold the same claim")
	}
	if first == job.ClaimOnBuild("another-job", wanted[0]) {
		t.Fatal("two jobs accepting a list of the same length collide on the first vertical")
	}
	// And it is not the claim the test stage's worker held on the same vertical. One claim for both
	// would refuse the build worker for work the test worker already did.
	if first == job.ClaimOnRequirement(one.ID, wanted[0]) {
		t.Fatal("the worker that wrote the tests and the worker that builds hold one claim")
	}
}

// The three shapes of false green this stage refuses. Each of them reads as success everywhere else
// in this system, and none of them says a vertical was built.
func TestARunThatBuiltNothingIsNotAPass(t *testing.T) {
	sad := []struct {
		name  string
		reply string
		says  string
	}{
		{
			name:  "a run that executed nothing",
			reply: "Vertical: 1\nRan: 0\nRed: 0\nPassing 1: TestIt\nChanged 1: main.go",
			says:  "executed 0 tests",
		},
		{
			name:  "a run that is still red",
			reply: "Vertical: 1\nRan: 14\nRed: 3\nPassing 1: TestIt\nChanged 1: main.go",
			says:  "still has 3 failing tests",
		},
		{
			name:  "a green run that changed nothing",
			reply: "Vertical: 1\nRan: 14\nRed: 0\nPassing 1: TestIt",
			says:  "was already passing",
		},
		{
			name:  "a build that changed a test",
			reply: "Vertical: 1\nRan: 14\nRed: 0\nPassing 1: TestIt\nChanged 1: internal/job/build_test.go",
			says:  "a build may not change one",
		},
		{
			name:  "a reply that says nothing about a run",
			reply: "I built it and it works",
			says:  "does not say which vertical",
		},
		{
			name:  "a reply that names no passing test",
			reply: "Vertical: 1\nRan: 14\nRed: 0\nChanged 1: main.go",
			says:  "names no test that passes now",
		},
	}
	for _, one := range sad {
		t.Run(one.name, func(t *testing.T) {
			_, err := job.ReadBuildReport(one.reply)
			if err == nil {
				t.Fatalf("%q was read as a build", one.reply)
			}
			if !strings.Contains(err.Error(), one.says) {
				t.Fatalf("the refusal is %q, want it to say %q", err, one.says)
			}
		})
	}
}

// A green run is only green for the tests the stage before it wrote. Without this a worker turns one
// easy test green, names that one, and the stage closes on a vertical whose own tests still fail.
func TestAReportMustNameTheTestsThatWereFailing(t *testing.T) {
	one := buildingJob()
	wanted := job.RequirementsOf(one)
	failing := job.FailuresOn(one.Tests)
	if len(failing[1]) != 1 || failing[1][0] != theFailures[1] {
		t.Fatalf("the failing tests of requirement 1 read as %v", failing[1])
	}

	reports := map[int]job.BuildReport{}
	for _, vertical := range wanted {
		report, err := job.ReadBuildReport(aBuildReport(vertical.Number, theFailures[vertical.Number]))
		if err != nil {
			t.Fatalf("ReadBuildReport: %v", err)
		}
		reports[vertical.Number] = report
	}
	if err := job.BuiltGreen(wanted, failing, reports); err != nil {
		t.Fatalf("a build that named every failing test is refused: %v", err)
	}

	// The same run, naming a test nobody was waiting on. It is refused by the name of the one that is
	// missing, so a person reads which test is unaccounted for.
	elsewhere, err := job.ReadBuildReport(aBuildReport(2, "TestSomethingElseEntirely"))
	if err != nil {
		t.Fatalf("ReadBuildReport: %v", err)
	}
	reports[2] = elsewhere
	err = job.BuiltGreen(wanted, failing, reports)
	if err == nil || !strings.Contains(err.Error(), theFailures[2]) {
		t.Fatalf("a build that never turned its own test green is accepted, saying %v", err)
	}
}

// A vertical whose worker died leaves nothing holding that vertical, and the other workers finishing
// must not read as the stage being done.
func TestTheStageIsNotDoneUntilEveryVerticalIsGreen(t *testing.T) {
	one := buildingJob()
	wanted := job.RequirementsOf(one)
	failing := job.FailuresOn(one.Tests)
	only, err := job.ReadBuildReport(aBuildReport(1, theFailures[1]))
	if err != nil {
		t.Fatalf("ReadBuildReport: %v", err)
	}

	err = job.BuiltGreen(wanted, failing, map[int]job.BuildReport{1: only})
	if err == nil {
		t.Fatal("a job with one of two verticals built reads as built")
	}
	if !strings.Contains(err.Error(), "vertical 2") {
		t.Fatalf("the refusal is %q, want it to name the vertical nothing holds", err)
	}

	// And a list the system cannot read is refused rather than read as nothing to do.
	if err := job.BuiltGreen(nil, nil, nil); err == nil {
		t.Fatal("a job with no verticals reads as built")
	}
}

// A worker is read as holding the vertical its claim says it holds. The two disagreeing would leave
// one vertical covered twice and another not at all.
func TestAWorkerThatReportedOnSomebodyElsesVerticalIsRefused(t *testing.T) {
	one := buildingJob()
	wanted := job.RequirementsOf(one)
	worker := job.BuildWorkers(one, wanted[1:], job.FailuresOn(one.Tests))[0]
	worker.Phase, worker.Answer = job.PhaseDone, aBuildReport(1, theFailures[1])

	_, why := job.BuiltBy([]*job.Job{worker}, wanted[1], job.FailuresOn(one.Tests)[2])
	if why == "" {
		t.Fatal("a worker that reported on vertical 1 was read as building vertical 2")
	}
	if !strings.Contains(why, "reported on vertical 1 instead") {
		t.Fatalf("the refusal is %q", why)
	}
}

// The record names the vertical every line came from, so a reader can say which vertical any one file
// belongs to.
func TestTheRecordNamesTheVerticalEachFileCameFrom(t *testing.T) {
	one := buildingJob()
	wanted := job.RequirementsOf(one)
	reports := map[int]job.BuildReport{}
	for _, vertical := range wanted {
		report, err := job.ReadBuildReport(aBuildReport(vertical.Number, theFailures[vertical.Number]))
		if err != nil {
			t.Fatalf("ReadBuildReport: %v", err)
		}
		reports[vertical.Number] = report
	}

	kept := job.BuiltText(wanted, reports)
	for _, line := range []string{
		"Vertical 1:", "Ran 1: 14", "Passes 1: " + theFailures[1],
		"Changed 1: internal/transcript/vertical1.go", "Vertical 2:", "Passes 2: " + theFailures[2],
	} {
		if !strings.Contains(kept, line) {
			t.Fatalf("the record is %q, want it to carry %q", kept, line)
		}
	}
	if verticals, passing := job.BuiltOn(kept); verticals != 2 || passing != 2 {
		t.Fatalf("the record reads as %d verticals and %d passing tests", verticals, passing)
	}
}

// The double answers a report the system can read, or every test about a fan out becomes a test about
// the double ignoring its task.
func TestTheDoubleAnswersABuildReportTheSystemCanRead(t *testing.T) {
	if model.BuildAsk != job.TheBuildAsk || model.BuildMarker != job.BuildMarker {
		t.Fatalf("the double answers %q to %q, and the stage asks %q and reads %q",
			model.BuildMarker, model.BuildAsk, job.TheBuildAsk, job.BuildMarker)
	}
	one := buildingJob()
	wanted := job.RequirementsOf(one)
	worker := job.BuildWorkers(one, wanted[1:], job.FailuresOn(one.Tests))[0]

	report, err := job.ReadBuildReport(model.FakeBuildReport(worker.Brief))
	if err != nil {
		t.Fatalf("the double's answer is not a report: %v", err)
	}
	if report.Vertical != 2 {
		t.Fatalf("the double reported on vertical %d, and the worker holds 2", report.Vertical)
	}
	// It names the test it was told fails, because the stage refuses a report that does not.
	if _, why := job.BuiltBy([]*job.Job{{Phase: job.PhaseDone,
		Answer: model.FakeBuildReport(worker.Brief)}}, wanted[1],
		job.FailuresOn(one.Tests)[2]); why != "" {
		t.Fatalf("the double's report is refused: %s", why)
	}
}

// An approved plan and a red suite fan out into one worker for each vertical, and the job itself gets
// no session at all.
func TestAnApprovedPlanFansOutIntoAWorkerForEachVertical(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(buildingJob())
	ctx := context.Background()

	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhasePending || got.Session != "" {
		t.Fatalf("the job is %q in session %q, want pending with no session", got.Phase, got.Session)
	}
	if !strings.Contains(got.Reason, "of 2 verticals are built") {
		t.Fatalf("the job says %q, want it to say how many verticals are built", got.Reason)
	}
	if !strings.Contains(got.Reason, "able to change a test") {
		t.Fatalf("the job says %q, want it to say the workers cannot change a test", got.Reason)
	}
	if len(kept.children(one.ID)) != 2 {
		t.Fatalf("the fan out declared %d workers for 2 verticals", len(kept.children(one.ID)))
	}

	// A second tick declares nothing more. The claim each worker holds is what stops it, and this is
	// the case that would otherwise pay for a second session for every vertical.
	controller.Tick(ctx)
	if again := kept.children(one.ID); len(again) != 2 {
		t.Fatalf("a second tick left %d workers for 2 verticals", len(again))
	}

	// The workers run, each in its own session, and each answers with its own report.
	controller.Tick(ctx)
	for _, worker := range kept.children(one.ID) {
		if kept.get(worker.ID).Phase != job.PhaseRunning {
			t.Fatalf("worker %q is %q, want running", worker.Title, kept.get(worker.ID).Phase)
		}
	}
	for i, worker := range kept.children(one.ID) {
		plane.landsIn(job.SessionFor(worker.ID), landed(aBuildReport(i+1, theFailures[i+1])))
	}
	controller.Tick(ctx)

	// And the stage closes by holding the job for a person rather than by calling it done.
	got = kept.get(one.ID)
	if got.Build == "" {
		t.Fatalf("every worker answered and the job holds no record: %s", got.Reason)
	}
	if got.Phase != job.PhaseAsking {
		t.Fatalf("the job is %q once every vertical is green, want asking so a person accepts it",
			got.Phase)
	}
	for _, phrase := range []string{"say whether the value arrived", "Nothing else"} {
		if !strings.Contains(got.Question, phrase) {
			t.Fatalf("the question is %q, want it to say %q", got.Question, phrase)
		}
	}
	// The question names each vertical and what a person is shown, because that is what they are being
	// asked to look at.
	for _, vertical := range job.RequirementsOf(got) {
		if !strings.Contains(got.Question, vertical.Shown) {
			t.Fatalf("the question does not say what vertical %d shows: %s",
				vertical.Number, got.Question)
		}
	}
}

// What the person does next. A hold that nothing can leave is a stopped job, so the answer has to put
// the job back on the road it was on: its own session, with the plan it is accountable to.
func TestOnceAPersonAcceptsItTheJobCarriesOnWithItsPlan(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(buildingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	controller.Tick(ctx)
	for i, worker := range kept.children(one.ID) {
		plane.landsIn(job.SessionFor(worker.ID), landed(aBuildReport(i+1, theFailures[i+1])))
	}
	controller.Tick(ctx)
	if kept.get(one.ID).Phase != job.PhaseAsking {
		t.Fatalf("the job is %q before anybody accepted it", kept.get(one.ID).Phase)
	}

	kept.sendTheListBack(one.ID, "I looked at both and the value arrived")

	controller.Tick(ctx)

	// It is dispatched into its own session now, carrying what the person said, rather than fanned out
	// a second time. That is the whole of what the hold has to leave: a job nothing can move on from
	// is a stopped job wearing another word.
	got := kept.get(one.ID)
	if len(kept.children(one.ID)) != 2 {
		t.Fatalf("accepting the build declared %d workers, want the same 2", len(kept.children(one.ID)))
	}
	if got.Phase != job.PhaseRunning {
		t.Fatalf("an accepted job is %q, want running: %s", got.Phase, got.Reason)
	}
	if sent := plane.lastText(); !strings.Contains(sent, "the value arrived") {
		t.Fatalf("the task after acceptance is %q, want it to carry what the person said", sent)
	}
	// And the record of what was built stays on the row, because that is what the person accepted.
	if got.Build == "" {
		t.Fatal("accepting the build lost the record of it")
	}
}

// A vertical nobody could build stops the job for a person rather than closing on the ones that
// landed. It asks rather than failing, because the usual cause is a test that says the wrong thing.
func TestAVerticalThatCannotBeBuiltStopsTheJobForAPerson(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(buildingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	controller.Tick(ctx)
	workers := kept.children(one.ID)
	plane.landsIn(job.SessionFor(workers[0].ID), landed(aBuildReport(1, theFailures[1])))
	// The second worker ran the suite and it is still red. This is the shape a session reaches for
	// when the test is the thing that is wrong.
	plane.landsIn(job.SessionFor(workers[1].ID),
		landed("Vertical: 2\nRan: 14\nRed: 2\nPassing 1: TestOne\nChanged 1: internal/page.go"))
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseAsking {
		t.Fatalf("the job is %q, want asking: %s", got.Phase, got.Reason)
	}
	if got.Build != "" {
		t.Fatalf("a job with a vertical nobody built carries the record %q", got.Build)
	}
	for _, phrase := range []string{"vertical 2", "still has 2 failing tests", "which test is wrong"} {
		if !strings.Contains(got.Question, phrase) {
			t.Fatalf("the question is %q, want it to say %q", got.Question, phrase)
		}
	}
}

// A test that was already green before anything was built holds nothing, so a worker reporting one is
// put to a person rather than counted.
func TestATestThatWasAlreadyGreenIsNotABuild(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(buildingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	controller.Tick(ctx)
	workers := kept.children(one.ID)
	plane.landsIn(job.SessionFor(workers[0].ID), landed(aBuildReport(1, theFailures[1])))
	plane.landsIn(job.SessionFor(workers[1].ID), landed(fmt.Sprintf(
		"Nothing needed doing.\n\nVertical: 2\nRan: 14\nRed: 0\nPassing 1: %s", theFailures[2])))
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseAsking || got.Build != "" {
		t.Fatalf("a vertical nobody wrote a file for is %q carrying %q", got.Phase, got.Build)
	}
	if !strings.Contains(got.Question, "was already passing") {
		t.Fatalf("the question is %q, want it to say the test was already passing", got.Question)
	}
}

// A worker that died leaves its vertical with nothing holding it, and the job stops for a person
// rather than reading the others as the whole.
func TestAVerticalWhoseWorkerDiedStopsTheJobForAPerson(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(buildingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	controller.Tick(ctx)
	workers := kept.children(one.ID)
	plane.landsIn(job.SessionFor(workers[0].ID), landed(aBuildReport(1, theFailures[1])))
	plane.failsIn(job.SessionFor(workers[1].ID), "the sandbox went away")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseAsking {
		t.Fatalf("the job is %q, want asking: %s", got.Phase, got.Reason)
	}
	if !strings.Contains(got.Question, "vertical 2") {
		t.Fatalf("the question is %q, want it to name the vertical nothing holds", got.Question)
	}
}

// A worker of the fan out never fans out itself. It carries the sentence its parent states, and a
// worker that read that as owing its own reading, list, tests and build would go round for ever.
func TestAWorkerOfTheBuildNeverFansOutItself(t *testing.T) {
	one := buildingJob()
	worker := job.BuildWorkers(one, job.RequirementsOf(one)[:1], job.FailuresOn(one.Tests))[0]
	if job.WaitingForItsBuild(worker) {
		t.Fatal("a build worker owes a build of its own")
	}
	if job.WaitingForItsIdeation(worker) || job.WaitingForItsDesign(worker) ||
		job.WaitingForItsTests(worker) || job.WaitingForItsPlan(worker) {
		t.Fatal("a build worker owes a stage of its own")
	}
	if stage := job.StageOf(worker); stage.Name != "" || stage.Outside == "" {
		t.Fatalf("a build worker is in stage %q of its own", stage.Name)
	}
}
