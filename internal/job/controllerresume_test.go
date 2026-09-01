package job_test

import (
	"context"
	"strings"
	"testing"

	"github.com/atlantic-blue/krewe/internal/job"
)

// A job that is being continued after a failure has to say what moved under the base its work stands
// on. The system runs no git, so it states the expectation and reads the answer against it, the way it
// already does with the address of a pull request.
//
// The silence is what these are about. A continued attempt that never looked at its base is the second
// failure a resume can cause, and it looks exactly like a job that went well.

// beingContinued is a job the operator continued: pending again, keeping the failure it is carrying on
// past, the steps its first attempt finished, and the pull request one of those steps named.
func beingContinued(title string) *job.Job {
	one := inARepository(title)
	one.Resuming = "the sandbox went away"
	one.Steps = []job.Step{
		{Seq: 1, Summary: "read the issue"},
		{Seq: 2, Summary: "opened " + thePullRequest},
	}
	one.PullRequest = thePullRequest
	return one
}

// The refusal, first. A continued attempt that says nothing about its base is asked for it rather than
// landed, because the answer decides whether the work is worth anything and only the session can say.
func TestAContinuedAttemptThatSaysNothingAboutItsBaseIsAskedWhatMoved(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(beingContinued("make the listing sort by the clock it shows"))
	ctx := context.Background()

	controller.Tick(ctx)
	// The pull request is in the answer, so what stops this landing is the base and nothing else.
	plane.lands("I carried on and opened " + thePullRequest)
	controller.Tick(ctx)

	if got := kept.get(one.ID); got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q saying %q, want it still running while the session is asked",
			got.Phase, got.Reason)
	}
	if plane.sent() != 2 {
		t.Fatalf("the system was asked to run %d tasks, want 2: the work and the ask", plane.sent())
	}
	asked := plane.dispatched[1].GetText()
	for _, phrase := range []string{"atlantic-blue/quay-crew", "Base:", "Fetch the branch"} {
		if !strings.Contains(asked, phrase) {
			t.Errorf("the session was asked %q, want it to say %q", asked, phrase)
		}
	}
	// In the conversation the work is in. A second session would be asking about a working directory
	// nobody is standing in.
	if got, want := plane.dispatched[1].GetHandle(), plane.dispatched[0].GetHandle(); got != want {
		t.Fatalf("the session was asked again in %q, want the conversation that did the work, %q", got, want)
	}
}

// Asked once and no more. Every ask is a task somebody pays for, and a session that will not say what
// moved will not say it on the third attempt either.
func TestAContinuedAttemptThatStillSaysNothingStopsTheJobRatherThanBeingAskedAgain(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(beingContinued("make the listing sort by the clock it shows"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I carried on and opened " + thePullRequest)
	controller.Tick(ctx)
	plane.lands("It is all done, the tests pass")
	controller.Tick(ctx)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q saying %q, want stopped", got.Phase, got.Reason)
	}
	for _, want := range []string{"what moved under its base", "asked twice"} {
		if !strings.Contains(got.Reason, want) {
			t.Errorf("the reason is %q, want it to say %q", got.Reason, want)
		}
	}
	if plane.sent() != 2 {
		t.Fatalf("the system was asked to run %d tasks, want 2: asked once and no more", plane.sent())
	}
	// What it produced is still on the row. The end of this attempt is not the end of the work, and a
	// reader who cannot find the pull request declares the job a second time.
	if got.PullRequest != thePullRequest {
		t.Fatalf("the stopped job names the pull request %q, want %s", got.PullRequest, thePullRequest)
	}
}

// And the ask works: the session says what moved and the job is done, with the report in the answer a
// person reads.
func TestAContinuedAttemptThatSaysWhatMovedWhenAskedLeavesTheJobDone(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(withTheGateOff(beingContinued("make the listing sort by the clock it shows")))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I carried on and opened " + thePullRequest)
	controller.Tick(ctx)
	plane.lands("Base: origin/main moved on by 4 commits, none in the files this branch edits.\nStill " + thePullRequest)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseDone {
		t.Fatalf("the job is %q saying %q, want done", got.Phase, got.Reason)
	}
	if !strings.Contains(got.Answer, "moved on by 4 commits") {
		t.Fatalf("the answer is %q, want what the session said moved under its base", got.Answer)
	}
}

// The first answer says it, so nothing is asked twice. This is the path a continued job takes when it
// does what it was told, and it costs one task.
func TestAContinuedAttemptThatSaysWhatMovedStraightAwayIsDoneInOneTask(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(withTheGateOff(beingContinued("make the listing sort by the clock it shows")))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("Base: nothing moved. I carried on and opened " + thePullRequest)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseDone {
		t.Fatalf("the job is %q saying %q, want done", got.Phase, got.Reason)
	}
	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1", plane.sent())
	}
}

// The expectation belongs to a continued attempt and to no other job. An attempt from nothing cut its
// own worktree a moment ago, so there is no base it was away from, and asking every job about one
// would be a gate nobody could pass on the first try.
func TestAJobThatWasNeverContinuedIsNotAskedWhatMovedUnderItsBase(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(withTheGateOff(inARepository("make the listing sort by the clock it shows")))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I made the change and opened " + thePullRequest)
	controller.Tick(ctx)

	if got := kept.get(one.ID); got.Phase != job.PhaseDone {
		t.Fatalf("the job is %q saying %q, want done", got.Phase, got.Reason)
	}
	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1", plane.sent())
	}
}

// A continued job that names no repository is not held to it either. The system knows of no branch it
// was away from, and a job stopped for not reporting on a base nobody declared is work thrown away.
func TestAContinuedJobInNoRepositoryIsNotHeldToTheBase(t *testing.T) {
	controller, kept, plane := aController(t)
	declared := declaredJob("read the electricity bill")
	declared.Resuming = "the sandbox went away"
	one := kept.add(declared)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	if got := kept.get(one.ID); got.Phase != job.PhaseDone {
		t.Fatalf("the job is %q saying %q, want done", got.Phase, got.Reason)
	}
	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1", plane.sent())
	}
}
