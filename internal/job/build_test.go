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
		"Passing 1: %s\nChanged 1: internal/transcript/vertical%d.go\nPicture: vertical%d.png\n"+
		"Taken: the page at http://localhost:3000, drawn with krewe render while the server was up",
		vertical, vertical, passing, vertical, vertical)
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

// One run for each vertical, each holding its own claim, each in the stage that puts it under the
// boundary, and each told the tests it owns.
func TestOneRunPerVerticalAndEachHoldsItsOwn(t *testing.T) {
	one := buildingJob()
	wanted := job.RequirementsOf(one)
	runs := job.BuildExecutions(one, wanted, nil)
	if len(runs) != 2 {
		t.Fatalf("a list of 2 verticals made %d runs", len(runs))
	}

	claims := map[string]bool{}
	for at, run := range runs {
		vertical := wanted[at]
		if run.Claim != job.ClaimOnBuild(one.ID, vertical) {
			t.Fatalf("run %d holds %q", vertical.Number, run.Claim)
		}
		if claims[run.Claim] {
			t.Fatalf("two runs hold %q, so both would build one vertical", run.Claim)
		}
		claims[run.Claim] = true
		if run.Job != one.ID || run.Number != vertical.Number {
			t.Fatalf("run %d belongs to job %q as number %d", vertical.Number, run.Job, run.Number)
		}
		// In the build stage, and that is what puts the session under the gate: the system reads the
		// stage of the run when it sends the task. A run in any other stage would be under advice.
		if run.Stage != job.StageBuild {
			t.Fatalf("run %d is in stage %q, so it builds outside the boundary",
				vertical.Number, run.Stage)
		}
		// Nothing a person wrote is on the row. What the session is asked and what the listing calls
		// it are built from the job when the task is sent.
		title := job.BuildingVertical(vertical)
		if !strings.Contains(title, fmt.Sprintf("vertical %d", vertical.Number)) {
			t.Fatalf("the run is called %q, and a refused claim names it", title)
		}
		// Its own vertical and nothing else, the tests it owns by name, and the boundary said in what
		// it is asked as well as held by the gate.
		asked := job.BuildTheVertical(one, vertical, job.FailuresOn(one.Tests)[vertical.Number], job.Opened{})
		for _, phrase := range []string{
			job.TheBuildAsk, vertical.Text, theFailures[vertical.Number],
			"You may not change one", "Build this vertical only",
		} {
			if !strings.Contains(asked, phrase) {
				t.Fatalf("what run %d is asked does not say %q: %s", vertical.Number, phrase, asked)
			}
		}
		// And not the other vertical's tests, or the fan out buys nothing.
		other := 3 - vertical.Number
		if strings.Contains(asked, theFailures[other]) {
			t.Fatalf("run %d was given the tests of vertical %d", vertical.Number, other)
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

// A run is read as holding the vertical its claim says it holds. The two disagreeing would leave
// one vertical covered twice and another not at all.
func TestARunThatReportedOnSomebodyElsesVerticalIsRefused(t *testing.T) {
	one := buildingJob()
	wanted := job.RequirementsOf(one)
	run := job.BuildExecutions(one, wanted[1:], nil)[0]
	run.Phase, run.Answer = job.PhaseDone, aBuildReport(1, theFailures[1])

	_, why := job.BuiltBy([]*job.Execution{run}, wanted[1], job.FailuresOn(one.Tests)[2])
	if why == "" {
		t.Fatal("a run that reported on vertical 1 was read as building vertical 2")
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
	asked := job.BuildTheVertical(one, wanted[1], job.FailuresOn(one.Tests)[2], job.Opened{})

	report, err := job.ReadBuildReport(model.FakeBuildReport(asked))
	if err != nil {
		t.Fatalf("the double's answer is not a report: %v", err)
	}
	if report.Vertical != 2 {
		t.Fatalf("the double reported on vertical %d, and the run holds 2", report.Vertical)
	}
	// It names the test it was told fails, because the stage refuses a report that does not.
	if _, why := job.BuiltBy([]*job.Execution{{Phase: job.PhaseDone,
		Answer: model.FakeBuildReport(asked)}}, wanted[1],
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
		t.Fatalf("the job says %q, want it to say the runs cannot change a test", got.Reason)
	}
	if len(kept.executionsIn(one.ID, job.StageBuild)) != 2 {
		t.Fatalf("the fan out wrote %d runs for 2 verticals", len(kept.executionsIn(one.ID, job.StageBuild)))
	}

	// A second tick writes nothing more. The claim each run holds is what stops it, and this is
	// the case that would otherwise pay for a second session for every vertical.
	controller.Tick(ctx)
	if again := kept.executionsIn(one.ID, job.StageBuild); len(again) != 2 {
		t.Fatalf("a second tick left %d runs for 2 verticals", len(again))
	}

	// The runs happen, each in its own session, and each answers with its own report.
	controller.Tick(ctx)
	for _, run := range kept.executionsIn(one.ID, job.StageBuild) {
		if kept.getRun(run.ID).Phase != job.PhaseRunning {
			t.Fatalf("the run of number %d is %q, want running", run.Number, kept.getRun(run.ID).Phase)
		}
	}
	for i, run := range kept.executionsIn(one.ID, job.StageBuild) {
		plane.landsIn(job.SessionForExecution(run), landed(aBuildReport(i+1, theFailures[i+1])))
	}
	controller.Tick(ctx)

	// And the stage closes by holding the job for a person rather than by calling it done.
	got = kept.get(one.ID)
	if got.Build == "" {
		t.Fatalf("every run answered and the job holds no record: %s", got.Reason)
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

// What the person's word does. It lands the job, and it is the only thing that does: the stage
// before this one ended by holding, and a hold nothing can leave is a stopped job wearing another
// word.
func TestOnlyThePersonsWordLandsAJobWhoseVerticalsAreBuilt(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(buildingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	controller.Tick(ctx)
	for i, run := range kept.executionsIn(one.ID, job.StageBuild) {
		plane.landsIn(job.SessionForExecution(run), landed(aBuildReport(i+1, theFailures[i+1])))
	}
	controller.Tick(ctx)
	if kept.get(one.ID).Phase != job.PhaseAsking {
		t.Fatalf("the job is %q before anybody accepted it", kept.get(one.ID).Phase)
	}

	// Every tick between the question and the answer leaves the job exactly where it is. An
	// acceptance that never comes is the sad path that decides whether this is a gate at all: a job
	// that moved on by itself here would be a job accepted by the passage of time.
	tasks := plane.sent()
	for i := 0; i < 3; i++ {
		controller.Tick(ctx)
	}
	waiting := kept.get(one.ID)
	if waiting.Phase != job.PhaseAsking || waiting.Accepted {
		t.Fatalf("three ticks with nobody answering left the job %q, accepted %v",
			waiting.Phase, waiting.Accepted)
	}
	if plane.sent() != tasks {
		t.Fatalf("waiting for an acceptance spent %d tasks", plane.sent()-tasks)
	}

	kept.sendTheListBack(one.ID, "yes")
	controller.Tick(ctx)

	// Their word is on the row, and no second fan out came of it. It is permission rather than an
	// ending: what is left is the ending every job has, and the session is told to build nothing
	// further because what they accepted is what is built.
	got := kept.get(one.ID)
	if !got.Accepted {
		t.Fatalf("the job a person accepted does not say so: %s", got.Reason)
	}
	if len(kept.executionsIn(one.ID, job.StageBuild)) != 2 {
		t.Fatalf("accepting the build declared %d workers, want the same 2", len(kept.executionsIn(one.ID, job.StageBuild)))
	}
	// And the record of what was built stays on the row, because that is what the person accepted.
	if got.Build == "" {
		t.Fatal("accepting the build lost the record of it")
	}
	if !kept.wrote(one.ID, job.EventAccepted) {
		t.Fatal("nothing on the record says a person accepted this job")
	}

	// Then the ordinary road, which was refused before their word and is open now. The session that
	// finishes the job is told what it is finishing.
	controller.Tick(ctx)
	if plane.sent() <= tasks {
		t.Fatal("nothing carried the accepted job to its ending")
	}
	if sent := plane.lastText(); !strings.Contains(sent, "said the value arrived") ||
		!strings.Contains(sent, "Build nothing further") {
		t.Fatalf("the task after acceptance is %q", sent)
	}
	kept.recordStep(one.ID, "1: build the two verticals")
	plane.lands("the work is in a pull request")
	controller.Tick(ctx)
	if landed := kept.get(one.ID); landed.Phase != job.PhaseDone {
		t.Fatalf("an accepted job that finished is %q: %s", landed.Phase, landed.Reason)
	}
}

// The other answer. A person who looked and did not see the value says what is missing, and the
// verticals go back to the stage that built them rather than the job stopping.
func TestAnAnswerThatIsNotTheAcceptanceSendsTheVerticalsBackToBuild(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(buildingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	controller.Tick(ctx)
	for i, run := range kept.executionsIn(one.ID, job.StageBuild) {
		plane.landsIn(job.SessionForExecution(run), landed(aBuildReport(i+1, theFailures[i+1])))
	}
	controller.Tick(ctx)

	kept.sendTheListBack(one.ID, "the second picture shows an empty page, the link is not read")
	controller.Tick(ctx)

	// Back to pending with the record cleared, which is what puts it in front of the build stage
	// again. What the person said stays on the row, because that is what the next fan out builds
	// against.
	sent := kept.get(one.ID)
	if sent.Accepted {
		t.Fatal("a job nobody accepted says it was accepted")
	}
	if sent.Phase != job.PhasePending {
		t.Fatalf("a job that was not accepted is %q, want pending so the build stage takes it back",
			sent.Phase)
	}
	if sent.Build != "" {
		t.Fatal("a job sent back still carries the record of a build nobody accepted")
	}
	if !strings.Contains(sent.Told, "empty page") {
		t.Fatalf("what the person said is %q, want it kept whole", sent.Told)
	}
	if !kept.wrote(one.ID, job.EventSentBack) {
		t.Fatal("nothing on the record says the job was sent back")
	}

	// And the build stage picks it up again, with a second run for each vertical.
	controller.Tick(ctx)
	if len(kept.executionsIn(one.ID, job.StageBuild)) != 4 {
		t.Fatalf("the build stage wrote %d runs in total, want a second for each of the 2",
			len(kept.executionsIn(one.ID, job.StageBuild)))
	}
}

// The last sad path, and the one that names this stage. A job whose verticals are built cannot
// settle on its own answer, however green everything it ran was.
func TestAJobCannotCallItselfDoneWithNobodyHavingLookedAtAPicture(t *testing.T) {
	controller, kept, plane := aController(t)
	// A job whose verticals are built and whose record shows nothing running. The row is past every
	// stage, so it is dispatched into its own session the way any other job is, and the session
	// answers as though it were finished: every check the machine can make is green and the answer
	// states an outcome, which is exactly the shape that used to settle a job.
	built := buildingJob()
	built.Build = "Vertical 1: a person pastes a link\nRan 1: 14\nPasses 1: TestItPasses\n" +
		"Changed 1: internal/transcript/paste.go"
	one := kept.add(built)
	ctx := context.Background()

	controller.Tick(ctx)
	if kept.get(one.ID).Phase != job.PhaseRunning {
		t.Fatalf("the job is %q, want a session of its own to answer from", kept.get(one.ID).Phase)
	}
	// Its plan is accounted for, so the gate in front of this one passes and what stops the job is
	// this one alone. A test that trips two gates proves nothing about either.
	kept.recordStep(one.ID, "1: build the two verticals")
	plane.lands("everything passes and the work is finished\n\nOutcome: proved")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase == job.PhaseDone {
		t.Fatal("a job called itself done with nobody having looked at a picture")
	}
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q, want stopped", got.Phase)
	}
	for _, phrase := range []string{"nothing shows any of them running", "not done until a person"} {
		if !strings.Contains(got.Reason, phrase) {
			t.Fatalf("the reason is %q, want it to say %q", got.Reason, phrase)
		}
	}
	// The work is not lost. It is unaccepted, which is a different thing, and the answer stays where a
	// person reads it next to the reason.
	if !strings.Contains(got.Answer, "the work is finished") {
		t.Fatalf("the answer is %q, want the work still on the row", got.Answer)
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
	runs := kept.executionsIn(one.ID, job.StageBuild)
	plane.landsIn(job.SessionForExecution(runs[0]), landed(aBuildReport(1, theFailures[1])))
	// The second run ran the suite and it is still red. This is the shape a session reaches for
	// when the test is the thing that is wrong.
	plane.landsIn(job.SessionForExecution(runs[1]),
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

// A test that was already green before anything was built holds nothing, so a run reporting one is
// put to a person rather than counted.
func TestATestThatWasAlreadyGreenIsNotABuild(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(buildingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	controller.Tick(ctx)
	runs := kept.executionsIn(one.ID, job.StageBuild)
	plane.landsIn(job.SessionForExecution(runs[0]), landed(aBuildReport(1, theFailures[1])))
	plane.landsIn(job.SessionForExecution(runs[1]), landed(fmt.Sprintf(
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

// A run that died leaves its vertical with nothing holding it, and the job stops for a person
// rather than reading the others as the whole.
func TestAVerticalWhoseRunDiedStopsTheJobForAPerson(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(buildingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	controller.Tick(ctx)
	runs := kept.executionsIn(one.ID, job.StageBuild)
	plane.landsIn(job.SessionForExecution(runs[0]), landed(aBuildReport(1, theFailures[1])))
	plane.failsIn(job.SessionForExecution(runs[1]), "the sandbox went away")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseAsking {
		t.Fatalf("the job is %q, want asking: %s", got.Phase, got.Reason)
	}
	if !strings.Contains(got.Question, "vertical 2") {
		t.Fatalf("the question is %q, want it to name the vertical nothing holds", got.Question)
	}
}
