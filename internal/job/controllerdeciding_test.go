package job_test

import (
	"context"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A session that stops for a person, and a record that says so.
//
// The incident: four jobs stopped for a person, every conversation held a question, and the record
// read running for all four. Nothing on the record ever learned there was a question, so the only
// way to find out was to open each conversation. These hold the two halves of the fix: a session
// that says a person must decide reaches the phase every reader of what waits on you already reads,
// and a session that is working reaches nothing at all.

// theDecision is what a session writes under the line when the work stops at a choice it cannot
// settle. It is the question a person answers.
const theDecision = "The store for the transcripts. Aurora Serverless version two bills a minimum " +
	"capacity continuously. DynamoDB on demand bills nothing at rest. Which do you want?"

func TestASessionThatSaysAPersonMustDecideStopsTheJobWithThatPerson(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("choose where the transcripts are kept"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.landsExactly(theDecision + "\n\n" + job.OutcomeMarker + " " + job.OutcomeDecide)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseAsking {
		t.Fatalf("the job is %q saying %q, want asking: a person has to decide and the record says nothing",
			got.Phase, got.Reason)
	}
	if got.Question != theDecision {
		t.Fatalf("the question on the row is %q, want what the session wrote under the line", got.Question)
	}
	// The outcome line is the system's own signal, so it is not part of the question a person reads.
	if strings.Contains(got.Question, job.OutcomeMarker) {
		t.Fatalf("the question carries the system's own line: %q", got.Question)
	}
	// A job waiting on a person is nobody's to hold. A controller still holding it would keep renewing
	// a lease on work that is not moving, and no other controller could take it after that one died.
	if got.LeaseOwner != "" || got.LeaseUntil != nil {
		t.Fatalf("the job is still held by %q, and it is waiting on a person", got.LeaseOwner)
	}
	if got.FinishedAt != nil {
		t.Fatal("the job is marked finished, and it is waiting to be told something")
	}
	if kinds := kept.kinds(one.ID); !contains(kinds, job.EventAsked) {
		t.Fatalf("the record holds %v, want a %s among them", kinds, job.EventAsked)
	}
}

// The quiet case, and the reason it matters: a false alarm every few minutes trains a person to
// ignore the true one. A session in the middle of its work has answered nothing, so nothing is put
// to anybody.
func TestASessionInTheMiddleOfItsWorkPutsNothingToAPerson(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	// No landing. The task is still running, which is what work looks like.
	controller.Tick(ctx)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q, want running: its task has not answered", got.Phase)
	}
	if got.Question != "" {
		t.Fatalf("a working job carries the question %q", got.Question)
	}
	if kinds := kept.kinds(one.ID); contains(kinds, job.EventAsked) {
		t.Fatalf("the record holds %v, and this session is working", kinds)
	}
	if plane.sent() != 1 {
		t.Fatalf("the system ran %d tasks, want the one the job declared", plane.sent())
	}
}

// The other three words are the work reporting on itself, and they settle the job. Only decide says
// the job stopped with a person, so only decide stops it there.
func TestAnAnswerThatStatesAnyOtherOutcomeDoesNotWaitOnAnybody(t *testing.T) {
	for _, outcome := range []string{job.OutcomeProved, job.OutcomeUnproved, job.OutcomeBlocked} {
		t.Run(outcome, func(t *testing.T) {
			controller, kept, plane := aController(t)
			one := kept.add(declaredJob("read the electricity bill"))
			ctx := context.Background()

			controller.Tick(ctx)
			plane.landsExactly("The bill is due on the 14th.\n\n" + job.OutcomeMarker + " " + outcome)
			controller.Tick(ctx)

			if got := kept.get(one.ID); got.Phase == job.PhaseAsking {
				t.Fatalf("a job that ended on %q is waiting on a person", outcome)
			}
		})
	}
}

// A session that states the word and writes nothing under it is still a session waiting on a person.
// Dropping it because the prose was blank would be the same failure one case further along.
func TestASessionThatDecidesAndSaysNothingStillReachesAPerson(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("choose where the transcripts are kept"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.landsExactly(job.OutcomeMarker + " " + job.OutcomeDecide)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseAsking {
		t.Fatalf("the job is %q, want asking", got.Phase)
	}
	if got.Question == "" {
		t.Fatal("the job is asking and carries no question, so a person is given nothing to answer")
	}
	if !strings.Contains(got.Question, got.Session) {
		t.Fatalf("the question is %q, want it to name the conversation to read", got.Question)
	}
}

// contains says whether a list of kinds holds one.
func contains(kinds []string, want string) bool {
	for _, kind := range kinds {
		if kind == want {
			return true
		}
	}
	return false
}
