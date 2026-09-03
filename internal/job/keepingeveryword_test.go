package job_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A person writes at whatever length the work needs, and the system keeps every word.
//
// A length cap used to refuse the text whole. A job wrote a correct reading of 859 bytes against a
// ceiling of 600, was asked once more, and was stopped for good on the second reply. The work was
// right and it was thrown away for being a few hundred bytes long, which is the failure these hold
// against.
//
// The five kinds of text below are the ones a person writes or reads: the brief, a step of the plan,
// the claim, a question and an answer. Each was held to a ceiling of its own, and none of them may
// refuse anything now. Every case asserts the text comes back byte for byte rather than asserting
// only that nothing failed, because text that is accepted and then cut is text a person lost.

// aLongBrief is a brief far past the ceiling a brief used to carry, written as prose so a case that
// passes proves the words survived rather than proving a count did.
func aLongBrief(t *testing.T) string {
	t.Helper()
	return paragraphs(job.BriefLimit*2, "The transcript page takes a link and gives back the text. "+
		"What the person types is the address of a video they are looking at, and what they get back "+
		"is the words, in order, with nothing else on the page. ")
}

// paragraphs is prose of at least this many bytes, built by repeating one paragraph and numbering
// each copy so no two lines are the same. A test that repeats one letter proves a length; this
// proves the words came back in the order they went in.
func paragraphs(atLeast int, one string) string {
	var built strings.Builder
	for at := 1; built.Len() < atLeast; at++ {
		fmt.Fprintf(&built, "Paragraph %d. %s\n\n", at, one)
	}
	return strings.TrimSpace(built.String())
}

func TestABriefOfAnyLengthIsAcceptedAndKeptWordForWord(t *testing.T) {
	brief := aLongBrief(t)
	one := job.Declaration{Title: "the transcript page", Brief: brief}

	if err := one.Validate(); err != nil {
		t.Fatalf("a brief of %d bytes was refused: %v", len(brief), err)
	}
	if kept := one.Tidied().Brief; kept != brief {
		t.Fatalf("the brief was kept as %d bytes of the %d it was written with", len(kept), len(brief))
	}
}

// The request is the brief's twin: what was asked for, in the words it was asked in. A ceiling on it
// asks somebody to shorten what another person said, which is the one thing it exists not to do.
func TestARequestOfAnyLengthIsAcceptedAndKeptWordForWord(t *testing.T) {
	request := paragraphs(job.RequestLimit*2, "Remove the cap. Work that is correct is thrown away "+
		"for being a few hundred bytes long, and the advice it leaves is to write a different brief. ")
	one := job.Declaration{Title: "the transcript page", Brief: "build what the design describes", Request: request}

	if err := one.Validate(); err != nil {
		t.Fatalf("a request of %d bytes was refused: %v", len(request), err)
	}
	if kept := one.Tidied().Request; kept != request {
		t.Fatalf("the request was kept as %d bytes of the %d it was written with", len(kept), len(request))
	}
}

func TestAClaimOfAnyLengthIsAcceptedAndKeptWordForWord(t *testing.T) {
	claim := "atlantic-blue/quay-krewe#647 " +
		strings.TrimSpace(strings.Repeat("the length cap that refuses work and stops the job ", 8))
	if len(claim) <= job.ClaimLimit {
		t.Fatalf("this case writes a claim of %d bytes, which is inside the old ceiling of %d and "+
			"proves nothing", len(claim), job.ClaimLimit)
	}
	one := job.Declaration{Title: "remove the cap", Brief: "no length cap refuses text", Claim: claim}

	if err := one.Validate(); err != nil {
		t.Fatalf("a claim of %d bytes was refused: %v", len(claim), err)
	}
	if kept := one.Tidied().Claim; kept != claim {
		t.Fatalf("the claim was kept as %q, and it was written as %q", kept, claim)
	}
}

func TestAQuestionOfAnyLengthIsAcceptedAndKeptWordForWord(t *testing.T) {
	asked := paragraphs(job.QuestionLimit*2, "The store for the transcripts. Aurora Serverless "+
		"version two bills a minimum capacity continuously, about 43 dollars a month at rest, and "+
		"DynamoDB on demand bills nothing at rest. Which do you want, and why. ")

	kept, err := job.TidyQuestion(asked)
	if err != nil {
		t.Fatalf("a question of %d bytes was refused: %v", len(asked), err)
	}
	if kept != asked {
		t.Fatalf("the question was kept as %d bytes of the %d it was asked with", len(kept), len(asked))
	}
}

func TestAnAnswerOfAnyLengthIsAcceptedAndKeptWordForWord(t *testing.T) {
	told := paragraphs(job.TellingLimit*2, "DynamoDB on demand, because nothing bills while nobody "+
		"uses it. The read pattern is by key, the writes come one at a time, and the table is empty "+
		"between runs. ")

	kept, err := job.TidyTelling(told)
	if err != nil {
		t.Fatalf("an answer of %d bytes was refused: %v", len(told), err)
	}
	if kept != told {
		t.Fatalf("the answer was kept as %d bytes of the %d it was written with", len(kept), len(told))
	}
}

func TestAPlanStepOfAnyLengthIsReadAndKeptWordForWord(t *testing.T) {
	step := "read the design, then build the address that takes a link, " +
		strings.TrimSpace(strings.Repeat("and keep the reading of it beside the plan so a person can hold one against the other, ", 6))
	if len(step) <= job.PlanStepLimit {
		t.Fatalf("this case writes a step of %d bytes, which is inside the old ceiling of %d and "+
			"proves nothing", len(step), job.PlanStepLimit)
	}

	steps, err := job.ReadPlan("Step 1: " + step)
	if err != nil {
		t.Fatalf("a step of %d bytes was refused: %v", len(step), err)
	}
	if len(steps) != 1 {
		t.Fatalf("the reply carries %d steps, want the one written", len(steps))
	}
	if steps[0].Text != step {
		t.Fatalf("the step was read as %q, and it was written as %q", steps[0].Text, step)
	}
	// And it survives being kept and read again, which is the road it actually travels: the system
	// writes the plan down in its own rendering and hands that back to a person and to a session.
	again := job.PlanIn(job.PlanText(steps))
	if len(again) != 1 || again[0].Text != step {
		t.Fatalf("the kept plan reads back as %v, want the step it was written with", again)
	}
}

// And the same step on the road it travels, which is where the refusal cost the job. A step the
// system would not read is a plan the system would not keep: the session is asked once more, and the
// second reply over the ceiling stops the job for good. So the length has to reach the row, not only
// the reader.
func TestALongPlanStepLandsOnTheRowAndTheJobStopsForAPersonRatherThanForItsLength(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(plannedJob())
	ctx := context.Background()

	step := "read the design, then build the address that takes a link, " +
		strings.TrimSpace(strings.Repeat("and keep the reading of it beside the plan so a person can hold one against the other, ", 6))
	controller.Tick(ctx)
	plane.lands("Here is the plan.\n\nStep 1: " + step)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseAsking {
		t.Fatalf("the job is %q with the reason %q, want it asking a person about the plan it wrote",
			got.Phase, got.Reason)
	}
	if got.Plan != "Step 1: "+step {
		t.Fatalf("the plan on the row is %q, want the step the session wrote", got.Plan)
	}
	// One task. A step that is asked for again costs a second one, and the second reply over the old
	// ceiling is what stopped the job.
	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1: it asked again for a shorter plan", plane.sent())
	}
}
