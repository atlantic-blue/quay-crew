package job_test

import (
	"context"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// The stage between the reading and the plan, driven one tick at a time. A job whose reading a person
// answered says what it would build, stops for a person, and plans nothing until they accept.

// aListReply is what a session answers when it is asked what it would build.
const aListReply = "Here is what I would build.\n\n" +
	"Vertical 1: a person pastes a link on the command line and gets the text back\n" +
	"Shown 1: the transcript prints in the terminal for a link the person chooses\n" +
	"Vertical 2: a person opens the same transcript in a browser and shares the address\n" +
	"Shown 2: the page renders that transcript at an address the person can send on"

// aListOfPlumbing is the shape the rule exists to refuse: three pieces of required work, none of
// which anybody can be shown working.
const aListOfPlumbing = "Vertical 1: a schema for the transcripts with an index on the link\n" +
	"Shown 1: the migration applies and the index exists\n" +
	"Vertical 2: a queue between the fetcher and the writer\n" +
	"Shown 2: the topic exists and the consumer is subscribed\n" +
	"Vertical 3: a role for the session that fetches\n" +
	"Shown 3: the role directory holds the model and the brief"

// listingJob is a job whose reading a person has already answered, which is what makes it owe a list.
func listingJob() *job.Job {
	one := readingJob()
	one.Ideation = "Understood: a page that takes a link and gives back the text\n" +
		"Not: a page that takes an identifier\n" +
		"Confidence: fairly sure\n" +
		"Question 1: which surface does a person read this on"
	one.IdeationAnswer = "1: on the command line first, the browser can come later"
	return one
}

// The moment the stage exists for. The list lands on the row, the job stops, and the question goes to
// a person. Nothing is planned and nothing is built.
func TestWhatItWouldBuildLandsOnTheRowAndTheJobStopsForAPerson(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(listingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands(aListReply)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseAsking {
		t.Fatalf("the job is %q, want asking: %s", got.Phase, got.Reason)
	}
	if !strings.Contains(got.Design, "Vertical 1: a person pastes a link") {
		t.Fatalf("what it would build is %q", got.Design)
	}
	if got.DesignAccepted {
		t.Fatal("the list reads as accepted, and nobody accepted it")
	}
	if got.Plan != "" {
		t.Fatalf("a plan was written before the list was accepted: %q", got.Plan)
	}
	for _, phrase := range []string{
		"you paste a link and get the text back", "Vertical 1: a person pastes a link",
		"Does this list get that sentence?",
	} {
		if !strings.Contains(got.Question, phrase) {
			t.Fatalf("the question is %q, want it to say %q", got.Question, phrase)
		}
	}
	// One task, which is what this stage costs, and nothing was landed on the row.
	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1", plane.sent())
	}
}

// The rule the stage is here for. A list of plumbing never reaches a person: the session is sent back
// with the refusal, so a person is never asked to accept three pieces of required work as three
// deliverables.
func TestAListOfPlumbingNeverReachesAPerson(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(listingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands(aListOfPlumbing)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Design != "" || got.Phase == job.PhaseAsking {
		t.Fatalf("a list of plumbing reached a person: %q, phase %q", got.Design, got.Phase)
	}
	asked := plane.lastText()
	for _, phrase := range []string{
		"asked for the list once already", "one vertical with its plumbing inside it",
	} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("the second ask is %q, want it to say %q", asked, phrase)
		}
	}
}

// An answer that is not the acceptance is the correction, and the session writes the list again from
// it. The person who says what is wrong writes no list.
func TestAListSentBackIsWrittenAgainFromWhatThePersonSaid(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(listingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands(aListReply)
	controller.Tick(ctx)
	kept.sendTheListBack(one.ID, "the browser one is not needed, an export is")

	controller.Tick(ctx)

	asked := plane.lastText()
	for _, phrase := range []string{
		"was not accepted", "the browser one is not needed, an export is",
		"Vertical 1: a person pastes a link", "Yours 2:",
	} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("the second list task is %q, want it to say %q", asked, phrase)
		}
	}
	if strings.Contains(asked, "Step 1:") {
		t.Fatalf("a job whose list was sent back was asked for a plan: %q", asked)
	}
	if kept.get(one.ID).DesignAccepted {
		t.Fatal("a list that was sent back reads as accepted")
	}
}

// And once a person accepts it, the plan is asked for, against the list they accepted.
func TestOnceAcceptedTheSessionIsAskedForThePlanAgainstTheList(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(listingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands(aListReply)
	controller.Tick(ctx)
	kept.acceptTheList(one.ID)

	controller.Tick(ctx)

	asked := plane.lastText()
	for _, phrase := range []string{
		"Step 1:", "A person accepted this list", "Vertical 1: a person pastes a link",
	} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("the plan task is %q, want it to say %q", asked, phrase)
		}
	}
}

// A reply the system cannot read is asked for once more, carrying the refusal, and never a third
// time. The bound is the record rather than a counter of the system's own, so a controller that took
// the row over after another died reads the same history and stops where this one would.
func TestASessionThatAnswersWithNoListIsAskedOnceMoreThenTheJobStops(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(listingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I have read the brief and I know what to build.")
	controller.Tick(ctx)

	if plane.sent() != 2 {
		t.Fatalf("the system was asked to run %d tasks, want 2", plane.sent())
	}
	asked := plane.lastText()
	if !strings.Contains(asked, "asked for the list once already") {
		t.Fatalf("the second ask is %q, want it to name itself as the second", asked)
	}
	if kept.get(one.ID).Phase != job.PhaseRunning {
		t.Fatalf("the job is %q while a task is in flight", kept.get(one.ID).Phase)
	}

	plane.lands("I still know what to build.")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q, want stopped", got.Phase)
	}
	if !strings.Contains(got.Reason, "asked twice") {
		t.Fatalf("the reason is %q, want it to say it was asked twice", got.Reason)
	}
	// Asked twice and no more. Every ask is a task somebody pays for.
	if plane.sent() != 2 {
		t.Fatalf("the system was asked to run %d tasks, want 2", plane.sent())
	}
	// What the session did say is still on the row, because the work is unread rather than lost.
	if got.Answer == "" {
		t.Fatal("the job stopped and threw away what its session answered")
	}
}

// A session that answers the list ask with a plan is a session that did not do this stage. The plan
// is not read as one, so nothing is planned and nothing is approved.
func TestAPlanAnsweredToTheListIsNotTakenAsAPlan(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(listingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("Step 1: read the design\nStep 2: build the address")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Plan != "" || got.PlanApproved {
		t.Fatalf("a plan reached the row through the list: %q, approved %t",
			got.Plan, got.PlanApproved)
	}
	if got.Design != "" {
		t.Fatalf("a plan was kept as a list: %q", got.Design)
	}
}

// An errand states no sentence, so there is nothing to build a list against and nothing stops.
func TestAnErrandIsNeverAskedWhatItWouldBuild(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Design != "" {
		t.Fatalf("an errand was asked what it would build: %q", got.Design)
	}
	if plane.sent() != 1 {
		t.Fatalf("an errand cost %d tasks, want 1", plane.sent())
	}
}
