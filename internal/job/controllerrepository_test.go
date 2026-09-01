package job_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/model"
)

// A job that names a repository ends in a pull request against it, and the system reads the address off
// the answer rather than believing the model reported one.

const thePullRequest = "https://github.com/atlantic-blue/quay-crew/pull/454"

// inARepository is a job declared against one, the way CreateJob leaves it.
func inARepository(title string) *job.Job {
	declared := declaredJob(title)
	declared.Repository = "atlantic-blue/quay-crew"
	return declared
}

// The session is told how the job ends, by the system rather than by whoever wrote the brief. A brief
// that forgets to ask for a push produces work nobody can see, and every brief forgets eventually.
func TestASessionDoingAJobInARepositoryIsToldItEndsInAPullRequest(t *testing.T) {
	controller, kept, plane := aController(t)
	kept.add(inARepository("make the listing sort by the clock it shows"))

	controller.Tick(context.Background())

	asked := plane.dispatched[0].GetText()
	if !strings.Contains(asked, "make the listing") && !strings.Contains(asked, "open the bill") {
		t.Fatalf("the session was asked %q, want it to carry the brief", asked)
	}
	for _, phrase := range []string{"atlantic-blue/quay-crew", "pull request", "Do not merge"} {
		if !strings.Contains(asked, phrase) {
			t.Errorf("the session was asked %q, want it to say %q", asked, phrase)
		}
	}
}

// A job naming no repository is asked for no pull request, so nothing a job declares reaches a
// session that did not declare it.
func TestASessionDoingAJobInNoRepositoryIsAskedForNoPullRequest(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))

	controller.Tick(context.Background())

	asked := plane.dispatched[0].GetText()
	if !strings.Contains(asked, one.Brief) {
		t.Fatalf("the session was asked %q, want its brief", asked)
	}
	if strings.Contains(asked, "pull request") {
		t.Fatalf("the session was asked %q, and this job names no repository", asked)
	}
}

func TestAnAnswerNamingThePullRequestLeavesTheJobDoneAndSaysWhereTheWorkIs(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(inARepository("make the listing sort by the clock it shows"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("Pushed the branch and opened " + thePullRequest)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseDone {
		t.Fatalf("the job is %q saying %q, want done", got.Phase, got.Reason)
	}
	if got.PullRequest != thePullRequest {
		t.Fatalf("the job says the pull request is %q, want %s", got.PullRequest, thePullRequest)
	}
	// Read once and asked once. A session that answered with the address is not asked again.
	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1", plane.sent())
	}
}

// The refusal. A job that answered without a pull request is not landed: the session that did the
// work is still open, opening the pull request is one command, and a job that ends "done, and
// nowhere anybody can read it" is the silence this exists to end.
func TestAnAnswerNamingNoPullRequestSendsTheSessionBackForOne(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(inARepository("make the listing sort by the clock it shows"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I made the change and the tests pass")
	controller.Tick(ctx)

	if got := kept.get(one.ID); got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q saying %q, want it still running while the session is asked",
			got.Phase, got.Reason)
	}
	if plane.sent() != 2 {
		t.Fatalf("the system was asked to run %d tasks, want 2: the work and the ask", plane.sent())
	}
	asked := plane.dispatched[1].GetText()
	for _, phrase := range []string{"atlantic-blue/quay-crew", "no pull request", "Do not merge"} {
		if !strings.Contains(asked, phrase) {
			t.Errorf("the session was asked %q, want it to say %q", asked, phrase)
		}
	}
	// The second task lands in the same conversation. A second session would leave the branch behind
	// in the first one, with nothing able to push it.
	if got, want := plane.dispatched[1].GetHandle(), plane.dispatched[0].GetHandle(); got != want {
		t.Fatalf("the session was asked again in %q, want the conversation that did the work, %q", got, want)
	}
}

// And the ask works: the session opens the pull request and the job is done, carrying the address.
func TestASessionThatOpensThePullRequestWhenAskedLeavesTheJobDone(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(inARepository("make the listing sort by the clock it shows"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I made the change and the tests pass")
	controller.Tick(ctx)
	plane.lands("Opened " + thePullRequest)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseDone {
		t.Fatalf("the job is %q saying %q, want done", got.Phase, got.Reason)
	}
	if got.PullRequest != thePullRequest {
		t.Fatalf("the job says the pull request is %q, want %s", got.PullRequest, thePullRequest)
	}
}

// Asked once and no more. A session that cannot push would otherwise be asked forever, and every ask
// is a task somebody pays for.
func TestASessionThatStillNamesNoPullRequestStopsTheJobRatherThanBeingAskedAgain(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(inARepository("make the listing sort by the clock it shows"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I made the change and the tests pass")
	controller.Tick(ctx)
	plane.lands("There is no token in this session, so I could not push")
	controller.Tick(ctx)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q, want stopped", got.Phase)
	}
	if !strings.Contains(got.Reason, "atlantic-blue/quay-crew") {
		t.Fatalf("the reason is %q, want it to name the repository", got.Reason)
	}
	if plane.sent() != 2 {
		t.Fatalf("the system was asked to run %d tasks, want 2: asked once and no more", plane.sent())
	}
	// What the session said is kept. It is how somebody works out why nothing was pushed, and here it
	// says exactly what stopped it.
	if !strings.Contains(got.Answer, "no token") {
		t.Fatalf("the answer is %q, want what the session said", got.Answer)
	}
}

// A system that cannot ask again lands the job with the reason rather than holding a running row open
// waiting for a task nobody sent.
func TestAJobWhoseSessionCannotBeAskedAgainStopsWithTheReason(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(inARepository("make the listing sort by the clock it shows"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I made the change and the tests pass")
	plane.refuse = errors.New("no sandbox could be made")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q, want stopped", got.Phase)
	}
	if !strings.Contains(got.Reason, "atlantic-blue/quay-crew") {
		t.Fatalf("the reason is %q, want it to name the repository", got.Reason)
	}
}

// The claim the job made is read first. A job that did not do the work is stopped rather than asked
// to publish work that is not there.
func TestAnUnmetClaimStopsAJobInARepositoryBeforeItIsAskedForAPullRequest(t *testing.T) {
	controller, kept, plane := aController(t)
	declared := inARepository("pay the electricity bill")
	declared.ExpectContains = "paid"
	one := kept.add(declared)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q, want stopped", got.Phase)
	}
	if !strings.Contains(got.Reason, "paid") {
		t.Fatalf("the reason is %q, want it to name what was claimed", got.Reason)
	}
	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1", plane.sent())
	}
}

// The second ask is one whole task, so it is not spent on a session that cannot answer it. A job
// running in a mode that asks a person before it runs a network command could never have pushed, so
// asking it to push ends exactly where the first task ended.
//
// Such a job is refused at the declaration now, so what this covers is the row declared before that
// rule existed: the loop reads the mode on the row rather than trusting that nothing got past.
func TestAJobInAModeThatCannotPushIsNotAskedAgainForThePullRequest(t *testing.T) {
	controller, kept, plane := aController(t)
	declared := inARepository("make the listing sort by the clock it shows")
	declared.Mode = model.PermissionAcceptEdits
	one := kept.add(declared)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I made the change and the tests pass")
	controller.Tick(ctx)

	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1: the ask cannot be answered in this mode",
			plane.sent())
	}
	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q saying %q, want stopped", got.Phase, got.Reason)
	}
	// The mode is the reason, said as the reason. A job stopped for a push that was never going to
	// happen must not read like one where somebody forgot to push.
	for _, phrase := range []string{"atlantic-blue/quay-crew", "mode edits", "--mode dangerous"} {
		if !strings.Contains(got.Reason, phrase) {
			t.Errorf("the reason is %q, want it to say %q", got.Reason, phrase)
		}
	}
}

// And the job that can push is still asked, so the guard is about the mode and not about the ask.
func TestAJobInTheModeThatCanPushIsStillAskedAgain(t *testing.T) {
	controller, kept, plane := aController(t)
	declared := inARepository("make the listing sort by the clock it shows")
	declared.Mode = model.PermissionBypass
	kept.add(declared)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I made the change and the tests pass")
	controller.Tick(ctx)

	if plane.sent() != 2 {
		t.Fatalf("the system was asked to run %d tasks, want 2: the work and the ask", plane.sent())
	}
}
