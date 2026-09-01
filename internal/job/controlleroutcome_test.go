package job_test

import (
	"context"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A job ends by stating one outcome, and the controller reads that word rather than the prose around
// it. The refusals come first: a gate that always passes satisfies every test about passing.

// The whole of the failure this answers. Four jobs reported "done", "complete", "the pull request is
// open" and "I could not finish because the credential expired", and all four settled the same way.
func TestAnAnswerThatStatesNoOutcomeDoesNotSettleTheJob(t *testing.T) {
	for name, answer := range map[string]string{
		"a report":               "I read the issue, cut the worktree and pushed the branch.",
		"the word in a sentence": "The tests proved it, so this is complete.",
		"a word not in the set":  "Outcome: complete",
		"the marker alone":       "Outcome:",
	} {
		t.Run(name, func(t *testing.T) {
			controller, kept, plane := aController(t)
			one := kept.add(declaredJob("read the electricity bill"))
			ctx := context.Background()

			controller.Tick(ctx)
			plane.landsExactly(answer)
			controller.Tick(ctx)

			got := kept.get(one.ID)
			if got.Phase == job.PhaseDone {
				t.Fatalf("the job is done on the answer %q, which states no outcome", answer)
			}
			if got.Outcome != "" {
				t.Fatalf("the job says its outcome is %q, and the answer stated none", got.Outcome)
			}
			if !strings.Contains(got.Reason, job.OutcomeMarker) {
				t.Fatalf("the job stopped saying %q, want it to say what line was missing", got.Reason)
			}
			// The answer survives. The end of an attempt is not the end of what it produced, and a
			// reader has to be able to see what the session actually said.
			if got.Answer != answer {
				t.Fatalf("the answer on the record is %q, want %q", got.Answer, answer)
			}
		})
	}
}

// Asked once is what a pull request gets, because that is work done and not published. An outcome is
// one line the session was told to write in the task it has just answered, so asking again is paying
// a model to read its own instructions.
func TestAJobStatingNoOutcomeIsNotAskedAgain(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.landsExactly("I read the bill and it is due on the 14th")
	controller.Tick(ctx)
	controller.Tick(ctx)

	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want the one the job declared", plane.sent())
	}
	if got := kept.get(one.ID); got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q, want stopped", got.Phase)
	}
}

func TestAnAnswerStatingAnOutcomeSettlesTheJobWithThatWord(t *testing.T) {
	for _, outcome := range job.Outcomes() {
		t.Run(outcome, func(t *testing.T) {
			controller, kept, plane := aController(t)
			one := kept.add(declaredJob("read the electricity bill"))
			ctx := context.Background()

			controller.Tick(ctx)
			plane.landsExactly("The bill is due on the 14th.\n\n" + job.OutcomeMarker + " " + outcome)
			controller.Tick(ctx)

			got := kept.get(one.ID)
			if got.Phase != job.PhaseDone {
				t.Fatalf("the job is %q saying %q, want done", got.Phase, got.Reason)
			}
			if got.Outcome != outcome {
				t.Fatalf("the job says its outcome is %q, want %q", got.Outcome, outcome)
			}
		})
	}
}

// The session is told what its answer has to end with, by the system rather than by whoever wrote the
// brief. A brief that forgets it produces a job nothing downstream can read.
func TestEverySessionDoingAJobIsToldToStateAnOutcome(t *testing.T) {
	controller, kept, plane := aController(t)
	kept.add(declaredJob("read the electricity bill"))

	controller.Tick(context.Background())

	asked := plane.dispatched[0].GetText()
	if !strings.Contains(asked, job.OutcomeMarker) {
		t.Fatalf("the session was asked %q, want it to say to write the outcome line", asked)
	}
	for _, word := range job.Outcomes() {
		if !strings.Contains(asked, word) {
			t.Fatalf("the session was asked %q, want it to offer %q", asked, word)
		}
	}
}

// A job asked a second time for its pull request answers once more, and that answer is the one that
// ends the job. A session never told so would state its outcome in the answer nobody landed.
func TestTheAskForAPullRequestAlsoAsksForTheOutcome(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(withTheGateOff(inARepository("make the listing sort by the clock it shows")))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.landsExactly("I pushed the branch.\n\n" + job.OutcomeMarker + " " + job.OutcomeProved)
	controller.Tick(ctx)

	if plane.sent() != 2 {
		t.Fatalf("the system was asked to run %d tasks, want the job and the ask for the address", plane.sent())
	}
	asked := plane.dispatched[1].GetText()
	if !strings.Contains(asked, job.OutcomeMarker) {
		t.Fatalf("the second ask says %q, want it to ask for the outcome as well", asked)
	}

	plane.landsExactly("Opened " + thePullRequest + "\n\n" + job.OutcomeMarker + " " + job.OutcomeUnproved)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseDone || got.Outcome != job.OutcomeUnproved {
		t.Fatalf("the job is %q with the outcome %q, want done and unproved", got.Phase, got.Outcome)
	}
	if got.PullRequest != thePullRequest {
		t.Fatalf("the job names the pull request %q, want %q", got.PullRequest, thePullRequest)
	}
}

// A job the model never finished states nothing, and the system must not invent a word for it. A
// failure carrying an outcome would be the system reporting on work nobody did.
func TestAJobThatFailedStatesNoOutcome(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.fails("the credential expired")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseFailed {
		t.Fatalf("the job is %q, want failed", got.Phase)
	}
	if got.Outcome != "" {
		t.Fatalf("the failed job says its outcome is %q, and nothing stated one", got.Outcome)
	}
}

// The record carries the word too, so a reader counting a tree reads the records rather than opening
// every row.
func TestTheRecordOfAnEndedJobCarriesItsOutcome(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.landsExactly("The bill is due.\n\n" + job.OutcomeMarker + " " + job.OutcomeBlocked)
	controller.Tick(ctx)

	for _, record := range kept.recorded(one.ID) {
		if record.Kind != job.EventAnswered {
			continue
		}
		if !strings.Contains(record.Detail, job.OutcomeBlocked) {
			t.Fatalf("the record says %q, want it to carry the outcome", record.Detail)
		}
		return
	}
	t.Fatalf("nothing recorded the job being answered")
}
