package job_test

import (
	"context"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// The gate itself, driven one tick at a time. A job that states the sentence writes its plan, stops
// for a person, and only then does any work.

// plannedJob is a job at the top that states what a person does with what it builds, which is what
// makes it planned.
func plannedJob() *job.Job {
	one := declaredJob("the transcript page")
	one.Brief = "build what the design describes"
	one.Product = "you paste a link and get the text back"
	// Past the stage in front of this one. What it understood is on the row and a person answered it,
	// which is what leaves the job owing a plan rather than a reading. The reading itself has its own
	// tests beside these.
	one.Ideation = understoodAndAnswered
	one.IdeationAnswer = "1: on the command line"
	return one
}

// understoodAndAnswered is a reading the system can read back, as it keeps it.
const understoodAndAnswered = "Understood: a page that takes a link and gives back the text\n" +
	"Not: a page that takes an identifier\n" +
	"Told: the person pastes a link\n" +
	"Assumed: the transcript is already stored\n" +
	"Unknown: which surface this is read on\n" +
	"Confidence: fairly sure of the shape\n" +
	"Question 1: which surface does a person read this on"

// aPlan is what a session answers with when it is asked for one.
const aPlan = "Here is the plan.\n\nStep 1: read the design\nStep 2: build the address that takes a link"

func TestAPlannedJobIsAskedForItsPlanBeforeAnyWork(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(plannedJob())
	ctx := context.Background()

	controller.Tick(ctx)

	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1", plane.sent())
	}
	sent := plane.lastText()
	for _, phrase := range []string{"Do no work yet", "you paste a link and get the text back"} {
		if !strings.Contains(sent, phrase) {
			t.Fatalf("the first task is %q, want it to say %q", sent, phrase)
		}
	}
	if strings.Contains(sent, "record it with its number") {
		t.Fatalf("a session that owes a plan was told to do the work: %q", sent)
	}
	_ = one
}

// The moment the gate exists for. The plan lands on the row, the job stops, and a person is asked
// one question about it. Nothing is built.
func TestThePlanLandsOnTheRowAndTheJobStopsForAPerson(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(plannedJob())
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands(aPlan)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseAsking {
		t.Fatalf("the job is %q, want asking", got.Phase)
	}
	if got.Plan != "Step 1: read the design\nStep 2: build the address that takes a link" {
		t.Fatalf("the plan on the row is %q", got.Plan)
	}
	if got.PlanApproved {
		t.Fatal("the plan is approved and nobody approved it")
	}
	for _, phrase := range []string{"you paste a link and get the text back", "Step 1: read the design"} {
		if !strings.Contains(got.Question, phrase) {
			t.Fatalf("the question is %q, want it to say %q", got.Question, phrase)
		}
	}
	// Nothing else was sent, and nothing was landed. The gate costs one task.
	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1", plane.sent())
	}
	if got.Answer != "" {
		t.Fatalf("a job waiting for its plan to be approved carries an answer: %q", got.Answer)
	}
}

// A reply that carries no plan is asked once more, and stops the job the second time. A job whose
// plan nobody could read is a job nobody approved, so running it is running the thing this stops.
func TestASessionWithNoPlanIsAskedTwiceAndThenTheJobStops(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(plannedJob())
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I will read the design and then build the page.")
	controller.Tick(ctx)

	if plane.sent() != 2 {
		t.Fatalf("the system was asked to run %d tasks, want 2", plane.sent())
	}
	if got := kept.get(one.ID); got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q while it is being asked again, want running", got.Phase)
	}

	plane.lands("I really will build the page.")
	controller.Tick(ctx)

	if plane.sent() != 2 {
		t.Fatalf("the system was asked to run %d tasks, want 2: it asked a third time", plane.sent())
	}
	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q, want stopped", got.Phase)
	}
	if !strings.Contains(got.Reason, "asked twice") {
		t.Fatalf("the reason is %q, want it to say it asked twice", got.Reason)
	}
}

// Approved, and the work is what comes next. The plan travels with it, and so does the instruction
// that makes the work accountable to the plan.
func TestOnceApprovedTheWorkCarriesThePlan(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(plannedJob())
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands(aPlan)
	controller.Tick(ctx)
	kept.approvePlan(one.ID)

	controller.Tick(ctx)

	if plane.sent() != 2 {
		t.Fatalf("the system was asked to run %d tasks, want 2", plane.sent())
	}
	sent := plane.lastText()
	for _, phrase := range []string{
		"build what the design describes", "Step 1: read the design", "record it with its number",
	} {
		if !strings.Contains(sent, phrase) {
			t.Fatalf("the work task is %q, want it to say %q", sent, phrase)
		}
	}
	if strings.Contains(sent, "Do no work yet") {
		t.Fatalf("a session with an approved plan was told to do no work: %q", sent)
	}
}

// The test that matters most. An approval is worth nothing if the work can walk away from the thing
// that was approved, so a step of the plan that nothing accounts for stops the job and the reason
// names it.
func TestAPlanApprovedAndThenNotFollowedStopsTheJob(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(plannedJob())
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands(aPlan)
	controller.Tick(ctx)
	kept.approvePlan(one.ID)
	controller.Tick(ctx)

	// The session did the first step and never accounted for the second.
	kept.recordStep(one.ID, "1: read the design")
	plane.lands("built the page")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q, want stopped", got.Phase)
	}
	for _, phrase := range []string{"step 2", "build the address that takes a link"} {
		if !strings.Contains(got.Reason, phrase) {
			t.Fatalf("the reason is %q, want it to name %q", got.Reason, phrase)
		}
	}
	// What the work produced is not thrown away. It is unapproved, which is a different thing from
	// lost, and the operator reads the answer next to the reason.
	if got.Answer != landed("built the page") {
		t.Fatalf("the answer is %q, want the work to still be on the row", got.Answer)
	}
}

// The other half, and a check that always fires is the same as no check. A job whose record accounts
// for every step of the plan finishes, with no reason and nothing said.
func TestAPlanApprovedAndFollowedFinishesInSilence(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(plannedJob())
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands(aPlan)
	controller.Tick(ctx)
	kept.approvePlan(one.ID)
	controller.Tick(ctx)

	kept.recordStep(one.ID, "1: read the design")
	kept.recordStep(one.ID, "2: built the address")
	plane.lands("the page takes a link and gives the text back")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseDone {
		t.Fatalf("the job is %q, want done: %s", got.Phase, got.Reason)
	}
	if got.Reason != "" {
		t.Fatalf("a job that followed its plan carries a reason: %q", got.Reason)
	}
	if got.Answer != landed("the page takes a link and gives the text back") {
		t.Fatalf("the answer is %q", got.Answer)
	}
}

// A job that states no sentence is an errand, and an errand is not planned, not asked about, and not
// held to anything. This is what keeps the gate off every job the crew declares for itself.
func TestAnErrandRunsWithNoPlanAndNoQuestion(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseDone {
		t.Fatalf("the errand is %q, want done: %s", got.Phase, got.Reason)
	}
	if got.Plan != "" || got.Question != "" {
		t.Fatalf("an errand was asked to plan: plan %q, question %q", got.Plan, got.Question)
	}
	if plane.sent() != 1 {
		t.Fatalf("an errand cost %d tasks, want 1", plane.sent())
	}
}

// A job declared under another is one part of a plan a person already approved. Stopping at each one
// puts a person back in the loop for every job in the tree.
func TestAJobUnderAnotherIsNotPlanned(t *testing.T) {
	controller, kept, plane := aController(t)
	one := plannedJob()
	one.Parent, one.Depth = "job-above", 1
	kept.add(one)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the page is built")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseDone {
		t.Fatalf("a job under another is %q, want done: %s", got.Phase, got.Reason)
	}
	if !strings.Contains(plane.lastText(), "build what the design describes") {
		t.Fatalf("a job under another was not given its brief: %q", plane.lastText())
	}
}
