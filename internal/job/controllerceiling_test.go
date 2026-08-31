package job_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/atlantic-blue/krewe/internal/job"
)

// A session that has filled its context window is given no new work on the job it is doing, and the
// rest of that job goes to a fresh conversation carrying what it wrote down.
//
// The failure these are about is quiet. A session at eighty per cent used to keep taking tasks, and
// the last task of a long job is the one that opens the pull request and writes the answer, so the
// work that matters most was done at the point where the model is worst. Nothing failed. The job
// finished and looked exactly like one that went well.
//
// The refusals come first, because a gate that only ever passes satisfies every test about passing.

// fullness is how full each session's context window is, as the control plane reads it. A session
// nothing has been recorded for is empty, which is what a conversation nobody has spoken in looks
// like and what every fresh session here is.
type fullness struct {
	mu   sync.Mutex
	size int64
	used map[string]int64
}

func windowsOf(size int64) *fullness { return &fullness{size: size, used: map[string]int64{}} }

func (f *fullness) fills(session string, used int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.used[session] = used
}

func (f *fullness) SessionWindow(_ context.Context, session string) (used, size int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.used[session], f.size
}

// aControllerMeasuring is the controller with a reader for how full each session's window is, which
// is what turns the ceiling on.
func aControllerMeasuring(t *testing.T, size int64) (*job.Controller, *rows, *system, *fullness) {
	t.Helper()
	controller, kept, plane := aController(t)
	windows := windowsOf(size)
	return controller.Measuring(windows), kept, plane, windows
}

// atTheCeiling drives a job to the moment the gate matters: it answered without naming its pull
// request, so the system is about to ask it for one, and the session doing it is over the ceiling.
func atTheCeiling(t *testing.T, ceiling int64) (*job.Controller, *rows, *system, *job.Job) {
	t.Helper()
	controller, kept, plane, windows := aControllerMeasuring(t, 1_000_000)
	one := kept.add(inARepository("make the listing sort by the clock it shows"))
	ctx := context.Background()

	controller.Tick(ctx)
	kept.step(one.ID, "read the issue")
	windows.fills("session-"+job.SessionFor(one.ID), ceiling)
	plane.lands("I moved the query onto the new index and the tests pass")
	controller.Tick(ctx)
	return controller, kept, plane, one
}

// The sad path, and the one that decides whether this behaviour is worth having. A session asked to
// hand over and writing nothing leaves nothing for a fresh session to start from, so starting one
// would pay for every discovery the last session made and then read afterwards like a handover that
// went well.
func TestASessionThatWritesNoHandoffStopsTheJobRatherThanStartingAFreshOneFromNothing(t *testing.T) {
	controller, kept, plane, one := atTheCeiling(t, 820_000)
	ctx := context.Background()

	// Asked, and it answered without writing anything down.
	plane.lands("I am out of room, sorry")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q saying %q, want stopped", got.Phase, got.Reason)
	}
	for _, want := range []string{"context ceiling", "nothing for a fresh session to start from"} {
		if !strings.Contains(got.Reason, want) {
			t.Errorf("the reason is %q, want it to say %q", got.Reason, want)
		}
	}
	if plane.sent() != 2 {
		t.Fatalf("the system was asked to run %d tasks, want 2: the work and the ask for a handoff",
			plane.sent())
	}
}

// A window nobody has measured refuses nothing. The size of a context window is what the model
// runtime last told a session in that workspace, and a system nobody has told holds no opinion: a
// gate that read that silence as full would stop every job on it.
func TestASessionWhoseWindowNothingMeasuredIsAskedTheWayItAlwaysWas(t *testing.T) {
	controller, kept, plane, windows := aControllerMeasuring(t, 0)
	one := kept.add(inARepository("make the listing sort by the clock it shows"))
	ctx := context.Background()

	controller.Tick(ctx)
	// Nine hundred thousand tokens carried, and nothing anywhere says how big the window is.
	windows.fills("session-"+job.SessionFor(one.ID), 900_000)
	plane.lands("I moved the query onto the new index and the tests pass")
	controller.Tick(ctx)

	if got := kept.get(one.ID); got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q saying %q, want it running while the session is asked", got.Phase, got.Reason)
	}
	asked := plane.dispatched[1].GetText()
	if job.AskingForAHandoff(asked) {
		t.Fatalf("a session whose window nobody measured was made to hand over:\n%s", asked)
	}
	if !strings.Contains(asked, "pull request") {
		t.Fatalf("the session was asked %q, want the ask it would have got before the ceiling existed", asked)
	}
}

// A controller with no reader wired gives every session work, however full it is. That is every
// controller before this, and a system that cannot measure should behave like one.
func TestAControllerWithNoReaderRefusesNothing(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(inARepository("make the listing sort by the clock it shows"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I moved the query onto the new index and the tests pass")
	controller.Tick(ctx)

	if got := kept.get(one.ID); got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q saying %q, want it running while the session is asked", got.Phase, got.Reason)
	}
	if job.AskingForAHandoff(plane.dispatched[1].GetText()) {
		t.Fatal("a controller that cannot read a context window made a session hand over")
	}
}

// A session under the ceiling is asked for the pull request, in its own conversation, exactly as
// before. The gate has to be invisible until it fires, or it changes what a job costs on every path.
func TestASessionUnderTheCeilingIsAskedForThePullRequestAsBefore(t *testing.T) {
	_, kept, plane, one := atTheCeiling(t, 260_000)

	if got := kept.get(one.ID); got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q saying %q, want it running while the session is asked", got.Phase, got.Reason)
	}
	if job.AskingForAHandoff(plane.dispatched[1].GetText()) {
		t.Fatal("a session at 26 per cent was made to hand over")
	}
	if got, want := plane.dispatched[1].GetHandle(), plane.dispatched[0].GetHandle(); got != want {
		t.Fatalf("the session was asked again in %q, want the conversation that did the work, %q", got, want)
	}
}

// The whole of what this buys. The session over the ceiling is asked for a handoff instead of for the
// pull request, and once it has written one the rest of the job goes to a conversation with an empty
// window, carrying what was finished and what is left.
func TestTheRestOfAJobGoesToAFreshSessionCarryingWhatTheLastOneWroteDown(t *testing.T) {
	controller, kept, plane, one := atTheCeiling(t, 820_000)
	ctx := context.Background()

	asked := plane.dispatched[1].GetText()
	if !job.AskingForAHandoff(asked) {
		t.Fatalf("the session over the ceiling was asked %q, want the handoff", asked)
	}
	if got, want := plane.dispatched[1].GetHandle(), plane.dispatched[0].GetHandle(); got != want {
		t.Fatalf("the handoff was asked for in %q, want the conversation that is full, %q", got, want)
	}

	// The session writes it, the way krewe job handoff does over its credential, and answers.
	kept.handoff(one.ID, "the index is written, the query still reads the old one: branch 539-feat-index",
		"adding the index inside the migration that renames the column, which deadlocks")
	plane.lands("Handed over: the query still reads the old index. Branch 539-feat-index is pushed.")
	controller.Tick(ctx)

	// One tick both lets go of the full conversation and starts the fresh one. The job goes back to
	// pending in the same pass that starts it, so an operator does not watch a handover wait a tick for
	// nothing.
	if plane.sent() != 3 {
		t.Fatalf("the system was asked to run %d tasks, want 3: the work, the handoff, and the rest of the job",
			plane.sent())
	}
	// A different conversation. The point of handing over is a window that is empty, and the same
	// handle would be the same full conversation.
	if got, want := plane.dispatched[2].GetHandle(), plane.dispatched[0].GetHandle(); got == want {
		t.Fatalf("the rest of the job went back into %q, the conversation that was full", got)
	}
	// Carrying something. A test that a second session starts passes whether or not the handoff has
	// anything in it, so this reads the words the last session wrote.
	carried := plane.dispatched[2].GetText()
	for _, want := range []string{
		"the query still reads the old one",
		"539-feat-index",
		"which deadlocks",
		"read the issue",
		// The brief. A fresh conversation has never seen this job, which is where handing over differs
		// from continuing one in the session that did the work.
		"open the bill and say when it is due",
	} {
		if !strings.Contains(carried, want) {
			t.Errorf("the fresh session is not told %q:\n%s", want, carried)
		}
	}

	// And it is the same job. Nothing restarted: the steps, the identity and the record are the ones
	// the first session was working on.
	going := kept.get(one.ID)
	if going.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q saying %q, want running in the fresh session", going.Phase, going.Reason)
	}
	if len(going.Steps) != 1 {
		t.Fatalf("the job says it finished %d steps, want the one the first session recorded", len(going.Steps))
	}
	if going.Session == "" || going.Session == "session-"+job.SessionFor(one.ID) {
		t.Fatalf("the job is running in %q, want the fresh conversation", going.Session)
	}
}

// The fresh session finishes the job, in one task, and the answer lands on the job that was declared
// rather than on a second one. This is the end of the road the issue asks for: the work carries on
// and the job does not restart.
func TestTheFreshSessionFinishesTheJobThatWasDeclared(t *testing.T) {
	controller, kept, plane, one := atTheCeiling(t, 820_000)
	ctx := context.Background()

	kept.handoff(one.ID, "the query still reads the old index: branch 539-feat-index", "")
	plane.lands("Handed over, branch 539-feat-index is pushed")
	controller.Tick(ctx)
	plane.lands("Done, and it is open at " + thePullRequest)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseDone {
		t.Fatalf("the job is %q saying %q, want done", got.Phase, got.Reason)
	}
	if got.PullRequest != thePullRequest {
		t.Fatalf("the job names the pull request %q, want %s", got.PullRequest, thePullRequest)
	}
	if got.Attempts != 2 {
		t.Fatalf("the job records %d attempts, want 2: one session before the ceiling and one after",
			got.Attempts)
	}
}

// A job continued after a failure goes back into the conversation that did the work, and that
// conversation can be the full one. So the ceiling is read before that task is sent too, and the
// resume hands over rather than doing the work where the model is worst.
func TestAJobContinuedIntoAFullConversationHandsOverInstead(t *testing.T) {
	controller, kept, plane, windows := aControllerMeasuring(t, 1_000_000)
	one := kept.add(beingContinued("make the listing sort by the clock it shows"))
	ctx := context.Background()

	// The conversation it would be continued in, already full from the attempt that failed.
	kept.setSession(one.ID, "session-"+job.SessionFor(one.ID))
	windows.fills("session-"+job.SessionFor(one.ID), 900_000)
	controller.Tick(ctx)

	asked := plane.dispatched[0].GetText()
	if !job.AskingForAHandoff(asked) {
		t.Fatalf("a job continued into a full conversation was asked %q, want the handoff", asked)
	}
	if strings.Contains(asked, "Base:") {
		t.Fatalf("the full conversation was given the work as well as the handoff:\n%s", asked)
	}
}
