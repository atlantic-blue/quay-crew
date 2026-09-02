package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// A job says what it understood, a person answers it in their own words, and the plan is written
// from what they wrote. Driven through the control plane's own calls, and asserted on what the
// session was actually given: a test that stops at the row has proved half of it, because the half
// that decides whether this works is what the session is handed next.

// aJobThatSaidWhatItUnderstood declares a job at the top that states the sentence, and ticks until it
// is waiting for a person to answer what it understood.
func aJobThatSaidWhatItUnderstood(t *testing.T) planning {
	t.Helper()
	runner := &model.FakeRunner{Reply: thePlan}
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	_, project := newProject(t, server)
	declared, err := server.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "the transcript page",
		Brief: "build what the design describes", Product: theSentence,
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	system := planning{server: server, runner: runner, job: declared.GetJob()}
	ctx := context.Background()
	server.TickJob(ctx)
	system.landed(t)
	server.TickJob(ctx)
	return system
}

// The stage, end to end. Nothing is planned, what it understood is on the row, and a person holds
// the questions.
func TestAJobSaysWhatItUnderstoodBeforeItPlansAnything(t *testing.T) {
	system := aJobThatSaidWhatItUnderstood(t)

	got := system.reading(t)
	if got.GetPhase() != job.PhaseAsking {
		t.Fatalf("the job is %q, want asking: %s", got.GetPhase(), got.GetReason())
	}
	if got.GetPlan() != "" {
		t.Fatalf("a job nobody has answered wrote the plan %q", got.GetPlan())
	}
	if got.GetIdeationAnswer() != "" {
		t.Fatalf("the row reads as answered by %q, and nobody answered", got.GetIdeationAnswer())
	}
	// What the record has to carry: the reading, what it was told against what it assumed, what it
	// does not know, how sure it is, and the questions.
	for _, phrase := range []string{"Understood:", "Not:", "Told:", "Assumed:", "Unknown:",
		"Confidence:", "Question 1:"} {
		if !strings.Contains(got.GetIdeation(), phrase) {
			t.Fatalf("what it understood is %q, want it to carry %q", got.GetIdeation(), phrase)
		}
	}
	// And the question a person reads carries the sentence, the record, and how to answer it.
	for _, phrase := range []string{theSentence, "Question 1:", "in your own words", "nothing to approve"} {
		if !strings.Contains(got.GetQuestion(), phrase) {
			t.Fatalf("the question is %q, want it to say %q", got.GetQuestion(), phrase)
		}
	}
	// One task, which is what the stage costs.
	if sent := tasksOf(t, system.server, got.GetId()); len(sent) != 1 {
		t.Fatalf("the session ran %d tasks before anybody answered, want 1", len(sent))
	}
}

// The answer is content rather than consent. What a person wrote is kept whole, the job plans from
// it, and the plan task carries their words and the marks the session put on its own footings.
func TestTheAnswerIsKeptWholeAndThePlanIsWrittenFromIt(t *testing.T) {
	system := aJobThatSaidWhatItUnderstood(t)
	ctx := context.Background()

	const answered = "1: on the command line first, the briefing panel can come later"
	if _, err := system.server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: answered,
	}); err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}

	got := system.reading(t)
	if got.GetIdeationAnswer() != answered {
		t.Fatalf("what the person wrote is kept as %q", got.GetIdeationAnswer())
	}
	if got.GetPhase() != job.PhasePending {
		t.Fatalf("the answered job is %q, want pending", got.GetPhase())
	}

	system.server.TickJob(ctx)
	asked := system.asked(t)
	for _, phrase := range []string{"Step 1:", answered, "Assumed:", "still an assumption"} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("the plan task is %q, want it to say %q", asked, phrase)
		}
	}
}

// An answer that touches no question leaves every one of them unknown, and the session writing the
// plan is told which. The word that approves a plan says nothing about the work, and reading that
// silence as agreement is the failure this stage exists for.
func TestAnAnswerThatTouchesNoQuestionLeavesItUnknown(t *testing.T) {
	system := aJobThatSaidWhatItUnderstood(t)
	ctx := context.Background()

	if _, err := system.server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: "yes",
	}); err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}
	got := system.reading(t)
	if got.GetIdeationAnswer() != "yes" {
		t.Fatalf("the answer is kept as %q", got.GetIdeationAnswer())
	}
	if got.GetPlanApproved() {
		t.Fatal("the word yes approved a plan that does not exist yet")
	}

	system.server.TickJob(ctx)
	asked := system.asked(t)
	if !strings.Contains(asked, "still unknown") {
		t.Fatalf("the plan task is %q, want it to name what is still unknown", asked)
	}
}

// A second answer is refused, because by then the plan is being written from the first. A record
// that changed under a plan would leave a reader unable to say which words the plan came from.
func TestAnAnswerThatArrivesTwiceIsRefused(t *testing.T) {
	system := aJobThatSaidWhatItUnderstood(t)
	ctx := context.Background()

	const first = "1: on the command line"
	if _, err := system.server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: first,
	}); err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}
	_, err := system.server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: "2: actually the briefing panel",
	})
	if err == nil {
		t.Fatal("a second answer was taken")
	}
	if !strings.Contains(err.Error(), "waiting") {
		t.Fatalf("the refusal is %q, want it to say which jobs are waiting", err)
	}
	if kept := system.reading(t).GetIdeationAnswer(); kept != first {
		t.Fatalf("the second answer replaced the first: %q", kept)
	}
}
