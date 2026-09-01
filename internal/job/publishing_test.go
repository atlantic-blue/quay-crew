package job_test

import (
	"context"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/publish"
)

// Why a job that names a repository stopped without a pull request, and where its work is.
//
// The sentence used to be "the work is in the session and nowhere else: open it, and push what is
// there", which made the operator the transport. These hold the four outcomes apart, and hold all of
// them to saying something an operator can act on without opening a container.

const theRepository = "atlantic-blue/quay-krewe"

const theHostPath = "/qdata/workspaces/e5b4c0ac/projects/12e5b9b0/sessions/145c0173/workspace"

// The empty case, first. A reason that names a branch nobody made sends the operator looking for
// work that was never done, which is worse than saying nothing.
func TestAJobWhoseSessionCommittedNothingSaysSoAndNamesNoBranch(t *testing.T) {
	said := job.NoPullRequest(theRepository, "145c0173", publish.Work{
		State: publish.Nothing, Host: theHostPath,
	})

	if !strings.Contains(said, "committed nothing") {
		t.Fatalf("the reason is %q, want it to say the session committed nothing", said)
	}
	if strings.Contains(said, "branch ") && !strings.Contains(said, "no branch") {
		t.Fatalf("the reason names a branch that was never made:\n%s", said)
	}
	mustCarryThePath(t, said)
}

// A session that cloned nothing. The working directory is still named, because whatever it wrote is
// in there and the operator has to be told where rather than that there is nothing.
func TestAJobWhoseSessionHoldsNoRepositorySaysSoAndNamesTheDirectory(t *testing.T) {
	said := job.NoPullRequest(theRepository, "145c0173", publish.Work{
		State: publish.Absent, Host: theHostPath,
	})

	if !strings.Contains(said, "no repository") {
		t.Fatalf("the reason is %q, want it to say the session holds no repository", said)
	}
	mustCarryThePath(t, said)
}

// Work the system could not push. This is the case the issue was written about: the branch and the
// path are what the operator acts on, and neither of them used to be there.
func TestWorkThatCouldNotBePushedNamesTheBranchAndThePath(t *testing.T) {
	said := job.NoPullRequest(theRepository, "145c0173", publish.Work{
		State: publish.Held, Branch: "sort-the-listing", Host: theHostPath,
		Why: "remote: Permission to atlantic-blue/quay-krewe.git denied",
	})

	for _, want := range []string{"sort-the-listing", "Permission to", theRepository} {
		if !strings.Contains(said, want) {
			t.Fatalf("the reason is %q, want it to say %q", said, want)
		}
	}
	mustCarryThePath(t, said)
}

// A system with nowhere to look says that, rather than printing an empty path. It is the one case
// where the answer is that there is no answer, and it has to read as such.
func TestASystemThatKeepsNothingOnDiskSaysSoRatherThanNamingAnEmptyPath(t *testing.T) {
	said := job.NoPullRequest(theRepository, "145c0173", publish.Work{
		State: publish.Unreadable, Why: "this system keeps no working directory on disk",
	})

	if !strings.Contains(said, "no working directory on disk") {
		t.Fatalf("the reason is %q, want it to say the system keeps nothing on disk", said)
	}
	if strings.Contains(said, "The work is at  ") || strings.Contains(said, "at  on") {
		t.Fatalf("the reason prints an empty path:\n%s", said)
	}
}

// The happy path. The system pushed, so the work is readable and one step is left.
func TestAJobWhoseBranchTheSystemPushedSaysWhereTheWorkWent(t *testing.T) {
	said := job.NoPullRequest(theRepository, "145c0173", publish.Work{
		State: publish.Pushed, Branch: "sort-the-listing", Pushed: true, Host: theHostPath,
	})

	for _, want := range []string{"pushed the branch sort-the-listing", "open the pull request"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the reason is %q, want it to say %q", said, want)
		}
	}
}

// A branch the session pushed itself reads as work that is already readable, and the system does not
// claim the push.
func TestABranchAlreadyOnTheRemoteIsNotReportedAsSomethingTheSystemDid(t *testing.T) {
	said := job.NoPullRequest(theRepository, "145c0173", publish.Work{
		State: publish.Pushed, Branch: "sort-the-listing", Host: theHostPath,
	})

	if strings.Contains(said, "The system pushed") {
		t.Fatalf("the reason claims a push the system did not make:\n%s", said)
	}
	if !strings.Contains(said, "already in the repository") {
		t.Fatalf("the reason is %q, want it to say the branch is already there", said)
	}
}

// The rule over the whole class, so the next state added cannot bring the sentence back. No reason
// this writes may send a person into a container to rescue work: that is the fault, in one line.
func TestNoReasonEverTellsAPersonToOpenAContainer(t *testing.T) {
	every := []publish.Work{
		{State: publish.Pushed, Branch: "sort-the-listing", Pushed: true, Host: theHostPath},
		{State: publish.Pushed, Branch: "sort-the-listing", Host: theHostPath},
		{State: publish.Held, Branch: "sort-the-listing", Host: theHostPath, Why: "no such remote"},
		{State: publish.Nothing, Host: theHostPath},
		{State: publish.Absent, Host: theHostPath},
		{State: publish.Unreadable, Host: theHostPath, Why: "the session has no container"},
		{State: publish.Unreadable, Why: "this system keeps no working directory on disk"},
		{},
	}
	// Both sentences, because there are two now: the one about the mode and the one about the pull
	// request. A rule that held only one of them would be a rule the next sentence walks past.
	for _, found := range every {
		for _, mode := range []string{"", model.PermissionAcceptEdits} {
			mustNotSendAnybodyIn(t, job.WhyNoPullRequest(theRepository, mode, "145c0173", found), found.State)
		}
		mustNotSendAnybodyIn(t, job.NoPullRequest(theRepository, "145c0173", found), found.State)
	}
}

// mustCarryThePath holds the half an operator acts on: the directory on the machine, and the command
// that reads it without opening anything.
func mustCarryThePath(t *testing.T, said string) {
	t.Helper()
	if !strings.Contains(said, theHostPath) {
		t.Fatalf("the reason does not say where the work is:\n%s", said)
	}
	if !strings.Contains(said, "krewe read 145c0173") {
		t.Fatalf("the reason does not say how to read it:\n%s", said)
	}
}

// What the controller does with the work when it stops a job that named a repository and answered
// without a pull request. The system publishes rather than asking a person to.

// aPublisher is the system going to look at what a session left behind, recording who it was asked
// about so a test can say it was asked once and about the right session.
type aPublisher struct {
	found publish.Work
	asked []string
}

func (p *aPublisher) PublishSessionWork(_ context.Context, session string) publish.Work {
	p.asked = append(p.asked, session)
	return p.found
}

// The sad path first, because a publisher that is never asked satisfies every test about what it
// says. A controller with none cannot reach a session's files, and the reason has to say that rather
// than name a path this system does not have.
func TestAControllerWithNoPublisherSaysItCannotReachTheFiles(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(inARepository("make the listing sort by the clock it shows"))
	ctx := context.Background()

	stopWithoutAPullRequest(ctx, controller, plane)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q, want stopped", got.Phase)
	}
	if !strings.Contains(got.Reason, "no way to reach a session's files") {
		t.Fatalf("the reason is %q, want it to say the system cannot reach the files", got.Reason)
	}
	if strings.Contains(strings.ToLower(got.Reason), "open it") {
		t.Fatalf("the reason sends a person into a container:\n%s", got.Reason)
	}
}

// The work is looked at once, at the moment of stopping, and about the session that did it. Looking
// costs commands inside a container, and a job still working has not finished the work this is about.
func TestTheWorkIsLookedAtOnceAndOnlyWhenTheJobStops(t *testing.T) {
	kept, plane := newRows(), newSystem()
	publisher := &aPublisher{found: publish.Work{
		State: publish.Pushed, Branch: "sort-the-listing", Pushed: true, Host: theHostPath,
	}}
	controller := job.NewController(kept, plane, nil, nil, nil).Publishing(publisher)
	one := kept.add(inARepository("make the listing sort by the clock it shows"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I made the change and the tests pass")
	// The first answer sends the session back for the pull request. Nothing is published yet: the
	// session is open and opening it is one command, which is cheaper than anything the system can do.
	controller.Tick(ctx)
	if len(publisher.asked) != 0 {
		t.Fatalf("the system looked at the work while the session was still being asked: %v", publisher.asked)
	}

	plane.lands("There is no token in this session, so I could not push")
	controller.Tick(ctx)
	controller.Tick(ctx)

	if len(publisher.asked) != 1 {
		t.Fatalf("the system looked at the work %d times, want once", len(publisher.asked))
	}
	if publisher.asked[0] != kept.get(one.ID).Session {
		t.Fatalf("the system looked at session %q, want the one that did the job, %q",
			publisher.asked[0], kept.get(one.ID).Session)
	}
	if !strings.Contains(kept.get(one.ID).Reason, "pushed the branch sort-the-listing") {
		t.Fatalf("the reason is %q, want it to say what the system did with the work",
			kept.get(one.ID).Reason)
	}
}

// The whole point, on the row a person reads: a job that stops holding work that was never pushed
// says where that work is, rather than telling somebody to go into a container and fetch it.
func TestAJobThatStopsHoldingUnpushedWorkSaysWhereItIs(t *testing.T) {
	kept, plane := newRows(), newSystem()
	publisher := &aPublisher{found: publish.Work{
		State: publish.Held, Branch: "sort-the-listing", Host: theHostPath,
		Why: "remote: Permission to atlantic-blue/quay-krewe.git denied",
	}}
	controller := job.NewController(kept, plane, nil, nil, nil).Publishing(publisher)
	one := kept.add(inARepository("make the listing sort by the clock it shows"))
	ctx := context.Background()

	stopWithoutAPullRequest(ctx, controller, plane)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q, want stopped", got.Phase)
	}
	for _, want := range []string{"sort-the-listing", theHostPath, "Permission to", "krewe read "} {
		if !strings.Contains(got.Reason, want) {
			t.Fatalf("the reason is %q, want it to say %q", got.Reason, want)
		}
	}
}

// stopWithoutAPullRequest drives a job through the two answers that end it: the work, then the ask,
// then the answer that still names none.
func stopWithoutAPullRequest(ctx context.Context, controller *job.Controller, plane *system) {
	controller.Tick(ctx)
	plane.lands("I made the change and the tests pass")
	controller.Tick(ctx)
	plane.lands("There is no token in this session, so I could not push")
	controller.Tick(ctx)
	controller.Tick(ctx)
}

// A job in a mode that could never push. The mode holds the session, not the system, so the work is
// still published: what the mode cost is the pull request, never the branch.
//
// The two halves have to compose, because they arrived from two directions at once. A reason that
// explained the mode and then went back to "the work is in the session" would answer the question
// nobody asked and drop the one that matters.
func TestAJobInAModeThatCouldNeverPushStillHasItsWorkPublished(t *testing.T) {
	kept, plane := newRows(), newSystem()
	publisher := &aPublisher{found: publish.Work{
		State: publish.Pushed, Branch: "sort-the-listing", Pushed: true, Host: theHostPath,
	}}
	controller := job.NewController(kept, plane, nil, nil, nil).Publishing(publisher)
	declared := inARepository("make the listing sort by the clock it shows")
	declared.Mode = model.PermissionAcceptEdits
	one := kept.add(declared)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I made the change, and I could not push")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q saying %q, want stopped", got.Phase, got.Reason)
	}
	// The mode is still the reason nothing was pushed by the session, said as the reason.
	for _, want := range []string{"mode edits", "--mode dangerous"} {
		if !strings.Contains(got.Reason, want) {
			t.Fatalf("the reason is %q, want it to say %q", got.Reason, want)
		}
	}
	// And the work went somewhere anyway.
	if len(publisher.asked) != 1 {
		t.Fatalf("the system looked at the work %d times, want once even in a mode that cannot push",
			len(publisher.asked))
	}
	if !strings.Contains(got.Reason, "pushed the branch sort-the-listing") {
		t.Fatalf("the reason is %q, want it to say the system published the work", got.Reason)
	}
	if strings.Contains(got.Reason, "The work is in the session") {
		t.Fatalf("the reason leaves the work in the session:\n%s", got.Reason)
	}
}

// mustNotSendAnybodyIn is the rule over the class: no reason the system writes about a job that
// stopped without a pull request may send a person into a container, and every one of them names the
// repository, which is what the listing is read for.
func mustNotSendAnybodyIn(t *testing.T, said, state string) {
	t.Helper()
	for _, word := range []string{"open it", "open the container", "attach", "and push what is there"} {
		if strings.Contains(strings.ToLower(said), word) {
			t.Fatalf("the reason for %q says %q, which makes the operator the transport:\n%s",
				state, word, said)
		}
	}
	if !strings.Contains(said, theRepository) {
		t.Fatalf("the reason for %q does not name the repository:\n%s", state, said)
	}
}
