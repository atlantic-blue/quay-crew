package job_test

import (
	"context"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// The stage in front of the plan, driven one tick at a time. A job that states the sentence says
// what it understood, stops for a person, and writes no plan until somebody answers.

// readingJob is a job at the top that states the sentence and has said nothing yet, which is what
// makes it owe a reading.
func readingJob() *job.Job {
	one := declaredJob("the transcript page")
	one.Brief = "build what the design describes"
	one.Product = "you paste a link and get the text back"
	return one
}

// aReadingReply is what a session answers when it is asked what it understood.
const aReadingReply = "Here is what I make of it.\n\n" +
	"Understood: a page that takes a link and gives back the text\n" +
	"Not: a page that takes an identifier\n" +
	"Told: the person pastes a link\n" +
	"Assumed: the transcript is already stored\n" +
	"Unknown: which surface a person reads it on\n" +
	"Confidence: fairly sure of the shape, least sure of the surface\n" +
	"Question 1: which surface does a person read this on"

func TestAPlannedJobIsAskedWhatItUnderstoodBeforeAnythingElse(t *testing.T) {
	controller, kept, plane := aController(t)
	kept.add(readingJob())
	ctx := context.Background()

	controller.Tick(ctx)

	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1", plane.sent())
	}
	sent := plane.lastText()
	for _, phrase := range []string{
		"write no plan yet", "you paste a link and get the text back", "Question 1:",
	} {
		if !strings.Contains(sent, phrase) {
			t.Fatalf("the first task is %q, want it to say %q", sent, phrase)
		}
	}
	for _, phrase := range []string{"Step 1:", "record it with its number"} {
		if strings.Contains(sent, phrase) {
			t.Fatalf("a session that owes a reading was asked to plan or to work: %q", sent)
		}
	}
}

// The moment the stage exists for. What it understood lands on the row, the job stops, and the
// questions go to a person. Nothing is planned and nothing is built.
func TestWhatItUnderstoodLandsOnTheRowAndTheJobStopsForAPerson(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(readingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands(aReadingReply)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseAsking {
		t.Fatalf("the job is %q, want asking: %s", got.Phase, got.Reason)
	}
	if !strings.Contains(got.Ideation, "Understood: a page that takes a link and gives back the text") {
		t.Fatalf("what it understood is %q", got.Ideation)
	}
	if got.IdeationAnswer != "" {
		t.Fatalf("the row reads as answered by %q, and nobody answered", got.IdeationAnswer)
	}
	if got.Plan != "" {
		t.Fatalf("a job that has not been answered wrote the plan %q", got.Plan)
	}
	// The question carries the sentence and the record, and it says there is nothing to approve: a
	// person answering yes out of habit answers nothing at all.
	for _, phrase := range []string{
		"you paste a link and get the text back", "Assumed: the transcript is already stored",
		"in your own words", "nothing to approve",
	} {
		if !strings.Contains(got.Question, phrase) {
			t.Fatalf("the question is %q, want it to say %q", got.Question, phrase)
		}
	}
	// One task, which is what the stage costs, and nothing was landed on the row.
	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1", plane.sent())
	}
	if got.Answer != "" {
		t.Fatalf("a job waiting to be answered carries the answer %q", got.Answer)
	}
}

// Once a person has answered, the list is asked for, and it is asked for against what they wrote.
// The plan comes after the list, so this is the ask the answer reaches first.
func TestOnceAnsweredTheSessionIsAskedForTheListAgainstTheAnswer(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(readingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands(aReadingReply)
	controller.Tick(ctx)
	kept.answerReading(one.ID, "1: on the command line, the way every other listing is read")

	controller.Tick(ctx)

	sent := plane.lastText()
	for _, phrase := range []string{
		"Vertical 1:", "Assumed: the transcript is already stored",
		"on the command line, the way every other listing is read", "still an assumption",
	} {
		if !strings.Contains(sent, phrase) {
			t.Fatalf("the list task is %q, want it to say %q", sent, phrase)
		}
	}
	if strings.Contains(sent, "Step 1:") {
		t.Fatalf("a session that owes a list was asked for a plan: %q", sent)
	}
}

// A reply the system cannot read is asked for once more, carrying the refusal, and never a third
// time. The bound is the record rather than a counter of the system's own, so a controller that took
// the row over after another died reads the same history and stops where this one would.
func TestASessionThatAnswersWithNoReadingIsAskedOnceMoreThenTheJobStops(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(readingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I have read the brief and I understand what to do.")
	controller.Tick(ctx)

	if plane.sent() != 2 {
		t.Fatalf("the system was asked to run %d tasks, want 2", plane.sent())
	}
	asked := plane.lastText()
	if !strings.Contains(asked, "asked what you understood once already") {
		t.Fatalf("the second ask is %q, want it to name itself as the second", asked)
	}
	if kept.get(one.ID).Phase != job.PhaseRunning {
		t.Fatalf("the job is %q while a task is in flight", kept.get(one.ID).Phase)
	}

	plane.lands("I still understand it.")
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

// A session that answers the reading ask with a plan is a session that did not do this stage. The
// plan is not read as one, so nothing is planned and nothing is approved.
func TestAPlanAnsweredToTheReadingIsNotTakenAsAPlan(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(readingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("Step 1: read the design\nStep 2: build the address")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Plan != "" || got.PlanApproved {
		t.Fatalf("a plan reached the row through the reading: %q, approved %t",
			got.Plan, got.PlanApproved)
	}
	if got.Ideation != "" {
		t.Fatalf("a plan was kept as a reading: %q", got.Ideation)
	}
}

// An errand states no sentence, so there is nothing to read the work against and nothing stops.
func TestAnErrandIsNeverAskedWhatItUnderstood(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Ideation != "" || got.Question != "" {
		t.Fatalf("an errand was asked what it understood: %q, %q", got.Ideation, got.Question)
	}
	if plane.sent() != 1 {
		t.Fatalf("an errand cost %d tasks, want 1", plane.sent())
	}
}

// A step of a flow run follows the graph a person imported, so nobody is asked what it understood.
func TestAStepOfAFlowRunIsNeverAskedWhatItUnderstood(t *testing.T) {
	controller, kept, plane := aController(t)
	one := readingJob()
	one.Run = "a-run"
	kept.add(one)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the page is built")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Ideation != "" {
		t.Fatalf("a step of a flow run was asked what it understood: %q", got.Ideation)
	}
	if !strings.Contains(plane.lastText(), "build what the design describes") {
		t.Fatalf("a job under another was not given its brief: %q", plane.lastText())
	}
}
