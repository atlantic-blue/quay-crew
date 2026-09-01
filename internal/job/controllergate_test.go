package job_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A job that names a repository does not settle on its own answer. Two sessions that did not do the
// work read it first, and the loop's decisions about them are what these drive.
//
// The refusals come first, and they are the reason the gate exists. A test that a job both gates
// passed reaches done passes just as happily against a gate that passes everything, which is the
// state the system was already in.

// landsIn closes the open task of one session, so a scenario can say what the reviewer answered
// without saying what the tester answered. The double's own lands answers every open task at once,
// which is the wrong shape here: three conversations are in flight and they say different things.
func (c *system) landsIn(handle, reply string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, task := range c.tasks["session-"+handle] {
		if task.Status == "running" {
			task.Status, task.Reply = "idle", reply
		}
	}
}

// failsIn closes the open task of one session as the model refusing it, which is a gate that could
// not be run rather than one that passed or failed the work.
func (c *system) failsIn(handle, why string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, task := range c.tasks["session-"+handle] {
		if task.Status == "running" {
			task.Status, task.Failure = "failed", why
		}
	}
}

// asked is what one session was told to do, in the order it was told, so a test reads what the
// system really sent rather than what it meant to send.
func (c *system) asked(handle string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var said []string
	for _, one := range c.dispatched {
		if one.GetHandle() == handle {
			said = append(said, one.GetText())
		}
	}
	return said
}

// aJobPastItsWork is a gated job whose working session has answered with the address of its pull
// request, ticked far enough that the gate is the only thing left.
func aJobPastItsWork(t *testing.T) (*job.Controller, *rows, *system, *job.Job) {
	t.Helper()
	controller, kept, plane := aController(t)
	one := kept.add(inARepository("make the listing sort by the clock it shows"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I made the change and opened " + thePullRequest)
	return controller, kept, plane, one
}

// The whole of the defect, at the smallest scale it can be seen: the work answered, and the job did
// not settle on it.
func TestAJobDoesNotSettleOnItsOwnAnswer(t *testing.T) {
	controller, kept, plane, one := aJobPastItsWork(t)

	controller.Tick(context.Background())

	got := kept.get(one.ID)
	if got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q saying %q, want running: nothing has read this work", got.Phase, got.Reason)
	}
	if got.Reviewed || got.Tested {
		t.Fatalf("the job says reviewed=%v tested=%v with neither gate having answered", got.Reviewed, got.Tested)
	}
	// And the reviewer is what it is waiting for, in a conversation of its own.
	review := plane.asked(job.ReviewerFor(one.ID))
	if len(review) != 1 {
		t.Fatalf("the reviewer was asked %d times, want once", len(review))
	}
	if !strings.Contains(review[0], thePullRequest) {
		t.Fatalf("the reviewer was asked %q, want the address of the change", review[0])
	}
	// The reviewer holds no job, so the system mints it no credential and it may call nothing. That is
	// the boundary, and it is the credential rather than a sentence in the text.
	for _, sent := range plane.dispatched {
		if sent.GetHandle() == job.ReviewerFor(one.ID) && sent.GetJob() != "" {
			t.Fatalf("the reviewer was dispatched carrying job %q, so it holds that job's credential",
				sent.GetJob())
		}
	}
}

// The refusal this issue is about. The reviewer fails the work, and the job goes back to the session
// that did it rather than ending, and rather than reaching the operator.
func TestAReviewerThatFailsItSendsTheWorkBackAndTheJobStaysOpen(t *testing.T) {
	controller, kept, plane, one := aJobPastItsWork(t)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.landsIn(job.ReviewerFor(one.ID), "Verdict: fail the change adds a column and no migration")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q saying %q, want running: a fail is the next task, not the end", got.Phase, got.Reason)
	}
	work := plane.asked(job.SessionFor(one.ID))
	if len(work) != 2 {
		t.Fatalf("the session that did the work was asked %d things, want 2: the work and the fail", len(work))
	}
	for _, want := range []string{"a column and no migration", job.GateReviewer, "pull request"} {
		if !strings.Contains(work[1], want) {
			t.Errorf("the work went back saying %q, want it to say %q", work[1], want)
		}
	}
	// The tester is never asked. A change the reviewer failed does not need testing, and a container
	// nobody needed is a bill nobody agreed to.
	if asked := plane.asked(job.TesterFor(one.ID)); len(asked) != 0 {
		t.Fatalf("the tester was asked %d times about work the reviewer failed", len(asked))
	}
}

// And the round ends. Every ask is a task somebody pays for, so a second fail stops the job with what
// the gate said on the row rather than sending the work round again.
func TestWorkTheReviewerFailsTwiceStopsTheJob(t *testing.T) {
	controller, kept, plane, one := aJobPastItsWork(t)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.landsIn(job.ReviewerFor(one.ID), "Verdict: fail the migration is missing")
	controller.Tick(ctx)
	plane.landsIn(job.SessionFor(one.ID), "I could not fix it. Still "+thePullRequest)
	controller.Tick(ctx)
	plane.landsIn(job.ReviewerFor(one.ID), "Verdict: fail the migration is still missing")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q saying %q, want stopped", got.Phase, got.Reason)
	}
	for _, want := range []string{job.GateReviewer, "twice", "still missing"} {
		if !strings.Contains(got.Reason, want) {
			t.Errorf("the reason is %q, want it to say %q", got.Reason, want)
		}
	}
	if got.Reviewed || got.Tested {
		t.Fatalf("a job nothing passed says reviewed=%v tested=%v", got.Reviewed, got.Tested)
	}
	// What it produced is still on the row. The end of the job is not the end of the work, and a
	// reader who cannot find the pull request declares the job a second time.
	if got.PullRequest != thePullRequest {
		t.Fatalf("the stopped job names the pull request %q, want %s", got.PullRequest, thePullRequest)
	}
	// Four tasks and no more: the work, the review, the work again, the review again. A third round
	// would be a container and a bill nobody agreed to.
	if plane.sent() != 4 {
		t.Fatalf("the system was asked to run %d tasks, want 4", plane.sent())
	}
}

// A gate that answered without a verdict judged nothing, and reading that as a pass is the exact
// false green the gate exists to prevent.
func TestAGateThatSaysNothingStopsTheJobRatherThanPassingIt(t *testing.T) {
	controller, kept, plane, one := aJobPastItsWork(t)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.landsIn(job.ReviewerFor(one.ID), "I read the change and it seems reasonable enough to me.")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q saying %q, want stopped", got.Phase, got.Reason)
	}
	for _, want := range []string{job.GateReviewer, "without a verdict", job.VerdictMarker} {
		if !strings.Contains(got.Reason, want) {
			t.Errorf("the reason is %q, want it to say %q", got.Reason, want)
		}
	}
	// And the work is not sent back. Nothing has told the session what to fix, so a task asking it to
	// fix nothing is a task nobody can answer.
	if work := plane.asked(job.SessionFor(one.ID)); len(work) != 1 {
		t.Fatalf("the session that did the work was asked %d things, want 1", len(work))
	}
}

// A gate whose own task failed has passed nothing either. This is the rule the prover already
// applies: a check that quietly passes when it could not be run is no check at all.
func TestAGateThatCouldNotRunStopsTheJob(t *testing.T) {
	controller, kept, plane, one := aJobPastItsWork(t)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.failsIn(job.ReviewerFor(one.ID), "the sandbox went away")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q saying %q, want stopped", got.Phase, got.Reason)
	}
	for _, want := range []string{job.GateReviewer, "could not run", "the sandbox went away"} {
		if !strings.Contains(got.Reason, want) {
			t.Errorf("the reason is %q, want it to say %q", got.Reason, want)
		}
	}
}

// The machine being full is a moment rather than a verdict, so the job waits for the gate the way a
// job waits for a sandbox. Settling here would settle work nothing read.
func TestAGateTheSystemCouldNotStartLeavesTheJobWaiting(t *testing.T) {
	controller, kept, plane, one := aJobPastItsWork(t)
	ctx := context.Background()

	plane.refuse = errors.New("the daemon had no room")
	controller.Tick(ctx)

	if got := kept.get(one.ID); got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q saying %q, want running", got.Phase, got.Reason)
	}
	// And the next tick asks again, which is what makes waiting worth anything.
	controller.Tick(ctx)
	if asked := plane.asked(job.ReviewerFor(one.ID)); len(asked) != 1 {
		t.Fatalf("the reviewer was asked %d times, want once the room came back", len(asked))
	}
}

// The reviewer passes and the tester does not, which is the case a gate that stopped at the reviewer
// would let through: the change reads correctly and its suite is red.
func TestATesterThatFailsItSendsTheWorkBackToo(t *testing.T) {
	controller, kept, plane, one := aJobPastItsWork(t)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.landsIn(job.ReviewerFor(one.ID), "Verdict: pass it does what the brief asked")
	controller.Tick(ctx)
	if asked := plane.asked(job.TesterFor(one.ID)); len(asked) != 1 {
		t.Fatalf("the tester was asked %d times after the reviewer passed the work, want once", len(asked))
	}
	plane.landsIn(job.TesterFor(one.ID), "Verdict: fail 3 of 540 test files are red")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q saying %q, want running", got.Phase, got.Reason)
	}
	work := plane.asked(job.SessionFor(one.ID))
	if len(work) != 2 {
		t.Fatalf("the session that did the work was asked %d things, want 2", len(work))
	}
	for _, want := range []string{"540 test files are red", job.GateTester} {
		if !strings.Contains(work[1], want) {
			t.Errorf("the work went back saying %q, want it to say %q", work[1], want)
		}
	}
}

// And the road through. Both gates pass, the job settles, and the row says what passed it.
func TestAJobBothGatesPassSettlesAndSaysWhatPassedIt(t *testing.T) {
	controller, kept, plane, one := aJobPastItsWork(t)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.landsIn(job.ReviewerFor(one.ID), "Verdict: pass it does what the brief asked")
	controller.Tick(ctx)
	plane.landsIn(job.TesterFor(one.ID), "Verdict: pass 540 test files ran, 6034 tests, all green")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseDone {
		t.Fatalf("the job is %q saying %q, want done", got.Phase, got.Reason)
	}
	if !got.Reviewed || !got.Tested {
		t.Fatalf("the settled job says reviewed=%v tested=%v, want both", got.Reviewed, got.Tested)
	}
	if got.PullRequest != thePullRequest {
		t.Fatalf("the settled job names the pull request %q, want %s", got.PullRequest, thePullRequest)
	}
	// The answer on the row is still the work's own answer. The gates are the evidence beside it, not
	// a replacement for it.
	if !strings.Contains(got.Answer, thePullRequest) {
		t.Fatalf("the answer is %q, want what the session that did the work said", got.Answer)
	}
	if plane.sent() != 3 {
		t.Fatalf("the system was asked to run %d tasks, want 3: the work, the reviewer and the tester", plane.sent())
	}
}

// A gate is asked once per round, however often the loop ticks. A controller that asked on every tick
// would pay for a container a second at a time.
func TestTheGateIsAskedOncePerRoundHoweverOftenTheLoopTicks(t *testing.T) {
	controller, _, plane, one := aJobPastItsWork(t)
	ctx := context.Background()

	controller.Tick(ctx)
	controller.Tick(ctx)
	controller.Tick(ctx)

	if asked := plane.asked(job.ReviewerFor(one.ID)); len(asked) != 1 {
		t.Fatalf("the reviewer was asked %d times over three ticks, want once", len(asked))
	}
}

// A controller that died is replaced by one that reads the same records and reaches the same answer.
// The gate keeps nothing in the process, which is what makes that true.
func TestAnotherControllerReadsTheGateTheSameWay(t *testing.T) {
	controller, kept, plane, one := aJobPastItsWork(t)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.landsIn(job.ReviewerFor(one.ID), "Verdict: pass it does what the brief asked")

	// A second controller, holding the lease the first one wrote, which is what a controller that came
	// back to this row is. It has nothing in its own memory about the gate.
	second := job.NewController(kept, plane, nil, nil, nil).Owned(controller.Owner())
	second.Tick(ctx)
	plane.landsIn(job.TesterFor(one.ID), "Verdict: pass 540 test files ran, all green")
	second.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseDone || !got.Reviewed || !got.Tested {
		t.Fatalf("the job is %q reviewed=%v tested=%v, want done and passed by both",
			got.Phase, got.Reviewed, got.Tested)
	}
	// And it asked the reviewer nothing again: what the reviewer said is on the record of its session.
	if asked := plane.asked(job.ReviewerFor(one.ID)); len(asked) != 1 {
		t.Fatalf("the reviewer was asked %d times across two controllers, want once", len(asked))
	}
}

// A job declared with the gate off settles on its own answer, and that is the point of the flag: it
// is refusable rather than optional, and the row says which was chosen.
func TestAJobWithTheGateOffSettlesOnItsOwnAnswer(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(withTheGateOff(inARepository("make the listing sort by the clock it shows")))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I made the change and opened " + thePullRequest)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseDone {
		t.Fatalf("the job is %q saying %q, want done", got.Phase, got.Reason)
	}
	if got.Reviewed || got.Tested {
		t.Fatalf("a job with the gate off says reviewed=%v tested=%v", got.Reviewed, got.Tested)
	}
	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1: no gate was asked", plane.sent())
	}
	// And it reads differently from a job two sessions passed, which is what the record is for.
	if said := got.Gate().PassedBy(); !strings.Contains(said, "gate off") {
		t.Fatalf("the job says %q about what read it", said)
	}
}

// A job that names no repository has no change for anything to read, so nothing is asked and nothing
// is held back. The gate reaches the jobs that produce work and no others.
func TestAJobInNoRepositoryReachesNoGate(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
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

// A job that failed does not reach the gate either. There is nothing for a reviewer to read, and a
// failure would come back from two gates as a stop with the wrong reason on it.
func TestAJobThatFailedIsNotHeldBackByTheGate(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(inARepository("make the listing sort by the clock it shows"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.fails("the credential ran out")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseFailed {
		t.Fatalf("the job is %q saying %q, want failed", got.Phase, got.Reason)
	}
	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1", plane.sent())
	}
}

// The gate reads the task record of its own session rather than the last task the system ran, which
// is what keeps three conversations in flight from being read as one.
func TestTheGateIsNotConfusedByTheWorkingSessionsOwnTasks(t *testing.T) {
	controller, kept, plane, one := aJobPastItsWork(t)
	ctx := context.Background()

	controller.Tick(ctx)
	// The working session says something else while the reviewer is still reading. It is not a verdict
	// and it is not the reviewer's, so nothing about the gate moves.
	plane.landsIn(job.SessionFor(one.ID), "Verdict: pass I am sure my own work is fine")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q saying %q, want running: the reviewer has not answered", got.Phase, got.Reason)
	}
	if got.Reviewed {
		t.Fatal("the working session passed itself, which is the whole thing the gate exists to stop")
	}
}
