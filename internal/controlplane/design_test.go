package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A job says what it would build, a person accepts the list or sends it back, and the plan is written
// from the list they accepted. Driven through the control plane's own calls, and asserted on what the
// session was actually given: a test that stops at the row has proved half of it.

// aJobThatSaidWhatItWouldBuild ticks a job through the reading, answers it, and stops where the list
// is waiting for a person.
func aJobThatSaidWhatItWouldBuild(t *testing.T) planning {
	t.Helper()
	system := aJobThatSaidWhatItUnderstood(t)
	ctx := context.Background()
	if _, err := system.server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: theAnswerToTheReading,
	}); err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}
	system.server.TickJob(ctx)
	system.landed(t)
	system.server.TickJob(ctx)
	return system
}

func TestAJobPutsTheListItWouldBuildToAPerson(t *testing.T) {
	system := aJobThatSaidWhatItWouldBuild(t)

	got := system.reading(t)
	if got.GetPhase() != job.PhaseAsking {
		t.Fatalf("the job is %q, want asking: %s", got.GetPhase(), got.GetReason())
	}
	for _, phrase := range []string{"Vertical 1:", "Shown 1:"} {
		if !strings.Contains(got.GetDesign(), phrase) {
			t.Fatalf("the list on the row is %q, want it to carry %q", got.GetDesign(), phrase)
		}
	}
	if got.GetDesignAccepted() {
		t.Fatal("the list reads as accepted and nobody accepted it")
	}
	for _, phrase := range []string{theSentence, "Does this list get that sentence?"} {
		if !strings.Contains(got.GetQuestion(), phrase) {
			t.Fatalf("the question is %q, want it to say %q", got.GetQuestion(), phrase)
		}
	}
	// And no plan, because the plan is written from the list a person accepts.
	if got.GetPlan() != "" {
		t.Fatalf("a plan was written before anybody accepted the list: %q", got.GetPlan())
	}
}

// The one word starts the planning, and the plan task carries the list it was accepted as.
func TestAnAnswerOfYesAcceptsTheListAndThePlanIsWrittenFromIt(t *testing.T) {
	system := aJobThatSaidWhatItWouldBuild(t)
	ctx := context.Background()

	if _, err := system.server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: "yes",
	}); err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}

	got := system.reading(t)
	if !got.GetDesignAccepted() || got.GetPhase() != job.PhasePending {
		t.Fatalf("the accepted job is %q, accepted %t", got.GetPhase(), got.GetDesignAccepted())
	}
	// What it was told is cleared, because an acceptance is not an instruction to carry on with work.
	if got.GetTold() != "" {
		t.Fatalf("an accepted job carries %q as the thing it was told", got.GetTold())
	}

	system.server.TickJob(ctx)
	asked := system.asked(t)
	for _, phrase := range []string{"Step 1:", "A person accepted this list", "Vertical 1:"} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("the plan task is %q, want it to say %q", asked, phrase)
		}
	}
}

// An acceptance that arrives twice moves nothing. By then the job is pending and a controller is
// about to start it, and a second one would start the planning twice on one list.
func TestASecondAcceptanceIsRefusedAndTheFirstStands(t *testing.T) {
	system := aJobThatSaidWhatItWouldBuild(t)
	ctx := context.Background()

	if _, err := system.server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: "yes",
	}); err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}
	_, err := system.server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: "yes",
	})
	if err == nil {
		t.Fatal("a list was accepted twice")
	}
	if !strings.Contains(err.Error(), "waiting") {
		t.Fatalf("the refusal is %q, want it to say what is waiting", err)
	}
	got := system.reading(t)
	if !got.GetDesignAccepted() || got.GetPhase() != job.PhasePending {
		t.Fatalf("the second acceptance left the job %q, accepted %t",
			got.GetPhase(), got.GetDesignAccepted())
	}
}

// Anything but the word is the correction. The job goes back to the session with what the person
// said, and the list is written again from it: the person who says what is wrong writes no list.
func TestAnAnswerThatIsNotTheAcceptanceSendsTheListBack(t *testing.T) {
	system := aJobThatSaidWhatItWouldBuild(t)
	ctx := context.Background()

	const said = "the browser one is not needed, an export is"
	if _, err := system.server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: said,
	}); err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}

	got := system.reading(t)
	if got.GetDesignAccepted() {
		t.Fatal("a list somebody sent back reads as accepted")
	}
	if got.GetPhase() != job.PhasePending {
		t.Fatalf("the job is %q after an answer that is not the acceptance, want pending so the crew "+
			"writes the list again", got.GetPhase())
	}

	system.server.TickJob(ctx)
	asked := system.asked(t)
	for _, phrase := range []string{"was not accepted", said, "Yours 2:"} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("the second list task is %q, want it to say %q", asked, phrase)
		}
	}
	if strings.Contains(asked, "Step 1:") {
		t.Fatalf("a job whose list was sent back was asked for a plan: %q", asked)
	}
}
