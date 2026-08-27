package work_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/work"
)

// A controller is disposable. Kill the one that started a piece of work and the task keeps running,
// because the sandbox belongs to the control plane rather than to the controller. What these prove
// is that the next controller picks the work up, takes the answer that landed, and never sends a
// second task for work that has already been paid for.

// aLeasedController is a controller with a name and a lease short enough for a test to outlive.
func aLeasedController(t *testing.T, owner string, lease time.Duration) (*work.Controller, *rows, *crew) {
	t.Helper()
	kept, plane := newRows(), newCrew()
	return work.NewController(kept, plane, nil, nil, nil).Owned(owner).Leasing(lease), kept, plane
}

// The claim writes who holds the work and until when, so a reader can tell work in hand from work
// nobody is watching.
func TestClaimingWorkWritesTheLease(t *testing.T) {
	controller, kept, _ := aLeasedController(t, "controller-a", time.Minute)
	one := kept.add(declaredWork("read the electricity bill"))

	controller.Tick(context.Background())

	got := kept.get(one.ID)
	if got.LeaseOwner != "controller-a" {
		t.Fatalf("the work is held by %q, want controller-a", got.LeaseOwner)
	}
	if got.LeaseUntil == nil || !got.LeaseUntil.After(time.Now()) {
		t.Fatalf("the lease runs until %v, want a moment still ahead", got.LeaseUntil)
	}
}

// Work in hand belongs to whoever holds it. A second controller must not touch it while the lease
// runs, or two controllers would both write what came of it.
func TestWorkHeldByAnotherControllerIsLeftAlone(t *testing.T) {
	first, kept, plane := aLeasedController(t, "controller-a", time.Minute)
	one := kept.add(declaredWork("read the electricity bill"))
	ctx := context.Background()
	first.Tick(ctx)
	plane.lands("the bill is due on the 14th")

	second := work.NewController(kept, plane, nil, nil, nil).Owned("controller-b").Leasing(time.Minute)
	second.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != work.PhaseRunning {
		t.Fatalf("a controller that holds no lease moved the work to %q", got.Phase)
	}
	if got.LeaseOwner != "controller-a" {
		t.Fatalf("the work is now held by %q, and the lease had not run out", got.LeaseOwner)
	}
}

// The test that matters: the controller that started the work is killed, its task answers anyway,
// and the next controller adopts that answer without sending a second task.
func TestAControllerThatDiedMidTaskLeavesTheAnswerToBeAdoptedOnce(t *testing.T) {
	// A lease that has already run out is what a controller that stopped renewing looks like.
	dead, kept, plane := aLeasedController(t, "controller-a", time.Millisecond)
	one := kept.add(declaredWork("read the electricity bill"))
	ctx := context.Background()

	dead.Tick(ctx)
	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks, want 1", plane.sent())
	}
	// The controller is gone from here on. Its task lands anyway, because the sandbox is the crew's.
	plane.lands("the bill is due on the 14th")
	waitForLeaseToRunOut(t, kept, one.ID)

	next := work.NewController(kept, plane, nil, nil, nil).Owned("controller-b").Leasing(time.Minute)
	next.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != work.PhaseDone {
		t.Fatalf("the work is %q saying %q, want done", got.Phase, got.Reason)
	}
	if got.Answer != "the bill is due on the 14th" {
		t.Fatalf("the answer is %q, want the one the dead controller's task left behind", got.Answer)
	}
	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks, want the one that was already paid for", plane.sent())
	}
	if got.Attempts != 1 {
		t.Fatalf("the work is on attempt %d, want 1", got.Attempts)
	}
}

// The same death while the task is still going. The next controller takes the lease, waits, and
// still never sends a second task.
func TestAControllerThatDiedWhileTheTaskRunsTakesTheLeaseAndWaits(t *testing.T) {
	dead, kept, plane := aLeasedController(t, "controller-a", time.Millisecond)
	one := kept.add(declaredWork("read the electricity bill"))
	ctx := context.Background()

	dead.Tick(ctx)
	waitForLeaseToRunOut(t, kept, one.ID)

	next := work.NewController(kept, plane, nil, nil, nil).Owned("controller-b").Leasing(time.Minute)
	next.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != work.PhaseRunning {
		t.Fatalf("the work is %q, want running while its task is still open", got.Phase)
	}
	if got.LeaseOwner != "controller-b" {
		t.Fatalf("the work is held by %q, want the controller that took it over", got.LeaseOwner)
	}
	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks, want the one already under way", plane.sent())
	}

	// And when the task does answer, the controller that took it over writes what came back.
	plane.lands("the bill is due on the 14th")
	next.Tick(ctx)
	if got := kept.get(one.ID); got.Phase != work.PhaseDone || got.Answer != "the bill is due on the 14th" {
		t.Fatalf("the work is %q saying %q", got.Phase, got.Answer)
	}
}

// A controller that died before it dispatched left nothing behind. Nothing was paid for, so the work
// goes back to pending and is run properly.
func TestWorkWhoseControllerDiedBeforeDispatchingGoesBackToPending(t *testing.T) {
	kept, plane := newRows(), newCrew()
	// A crew that refuses the dispatch leaves the row exactly as a controller killed between the
	// claim and the call would: running, with a lease, and no session.
	one := kept.add(declaredWork("read the electricity bill"))
	kept.claim(one.ID, "controller-a", time.Now().Add(-time.Second))

	next := work.NewController(kept, plane, nil, nil, nil).Owned("controller-b").Leasing(time.Minute)
	next.Tick(context.Background())

	got := kept.get(one.ID)
	if got.Phase != work.PhaseRunning || got.LeaseOwner != "controller-b" {
		t.Fatalf("the work is %q held by %q, want it claimed and run by the controller that found it",
			got.Phase, got.LeaseOwner)
	}
	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks, want the one nobody had sent yet", plane.sent())
	}
}

// The hole the deterministic session name closes: the controller died between the dispatch and
// writing the session onto the row. A task is running and the row does not say so. Releasing that
// work to pending would send a second task and pay twice.
func TestWorkDispatchedButNeverRecordedIsAdoptedRatherThanSentAgain(t *testing.T) {
	kept, plane := newRows(), newCrew()
	one := kept.add(declaredWork("read the electricity bill"))
	kept.claim(one.ID, "controller-a", time.Now().Add(-time.Second))
	// The task the dead controller sent, in the session named after the work, with nothing on the
	// row to say so.
	plane.dispatchedInto(work.SessionFor(one.ID), one.Project, one.Brief)
	plane.lands("the bill is due on the 14th")

	next := work.NewController(kept, plane, nil, nil, nil).Owned("controller-b").Leasing(time.Minute)
	next.Tick(context.Background())
	next.Tick(context.Background())

	got := kept.get(one.ID)
	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks, want the one the dead controller had already sent", plane.sent())
	}
	if got.Phase != work.PhaseDone {
		t.Fatalf("the work is %q, want done from the answer that was already there", got.Phase)
	}
	if got.Answer != "the bill is due on the 14th" {
		t.Fatalf("the answer is %q", got.Answer)
	}
}

// A controller that is alive keeps its hold by renewing it. Without that the lease would run out
// under a task that is running perfectly well, and another controller would take work nobody lost.
func TestAControllerRenewsTheLeaseWhileItsTaskRuns(t *testing.T) {
	controller, kept, _ := aLeasedController(t, "controller-a", 50*time.Millisecond)
	one := kept.add(declaredWork("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	first := *kept.get(one.ID).LeaseUntil
	time.Sleep(5 * time.Millisecond)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.LeaseUntil == nil || !got.LeaseUntil.After(first) {
		t.Fatalf("the lease still runs to %v, want it moved on from %v", got.LeaseUntil, first)
	}
	if got.LeaseOwner != "controller-a" {
		t.Fatalf("the work is held by %q after a renewal", got.LeaseOwner)
	}
}

// Two controllers claiming the same row at the same moment. One wins, and the loser leaves it alone.
func TestTwoControllersClaimingAtOnceLeaveOneTask(t *testing.T) {
	kept, plane := newRows(), newCrew()
	first := work.NewController(kept, plane, nil, nil, nil).Owned("controller-a").Leasing(time.Minute)
	second := work.NewController(kept, plane, nil, nil, nil).Owned("controller-b").Leasing(time.Minute)
	one := kept.add(declaredWork("read the electricity bill"))
	ctx := context.Background()

	var arrived sync.WaitGroup
	arrived.Add(2)
	kept.beforeStart = func() {
		arrived.Done()
		arrived.Wait()
	}
	var waiting sync.WaitGroup
	waiting.Add(2)
	go func() { defer waiting.Done(); first.Tick(ctx) }()
	go func() { defer waiting.Done(); second.Tick(ctx) }()
	waiting.Wait()

	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks, want 1", plane.sent())
	}
	if holder := kept.get(one.ID).LeaseOwner; holder != "controller-a" && holder != "controller-b" {
		t.Fatalf("the work is held by %q, want one of the two that raced", holder)
	}
}

// Two controllers taking over the same abandoned row at the same moment. The same rule, one step
// later in the life of a piece of work.
func TestTwoControllersTakingOverAtOnceLeaveOneHolder(t *testing.T) {
	kept, plane := newRows(), newCrew()
	one := kept.add(declaredWork("read the electricity bill"))
	kept.claim(one.ID, "controller-a", time.Now().Add(-time.Second))
	kept.setSession(one.ID, "session-work-"+one.ID)
	plane.dispatchedInto(work.SessionFor(one.ID), one.Project, one.Brief)

	first := work.NewController(kept, plane, nil, nil, nil).Owned("controller-b").Leasing(time.Minute)
	second := work.NewController(kept, plane, nil, nil, nil).Owned("controller-c").Leasing(time.Minute)
	ctx := context.Background()

	var arrived sync.WaitGroup
	arrived.Add(2)
	kept.beforeTakeOver = func() {
		arrived.Done()
		arrived.Wait()
	}
	var waiting sync.WaitGroup
	waiting.Add(2)
	go func() { defer waiting.Done(); first.Tick(ctx) }()
	go func() { defer waiting.Done(); second.Tick(ctx) }()
	waiting.Wait()

	takeOvers := 0
	for _, kind := range kept.kinds(one.ID) {
		if kind == work.EventClaimed {
			takeOvers++
		}
	}
	if takeOvers != 1 {
		t.Fatalf("%d controllers took the work over, want 1", takeOvers)
	}
}

// The record is the whole story of a death: who was holding it, what phase it was in when it was
// found, and who has it now. There is no second start, and that absence is the evidence.
func TestARecoveryIsOnTheRecord(t *testing.T) {
	dead, kept, plane := aLeasedController(t, "controller-a", time.Millisecond)
	one := kept.add(declaredWork("read the electricity bill"))
	ctx := context.Background()

	dead.Tick(ctx)
	plane.lands("the bill is due on the 14th")
	waitForLeaseToRunOut(t, kept, one.ID)
	work.NewController(kept, plane, nil, nil, nil).Owned("controller-b").Leasing(time.Minute).Tick(ctx)

	got := strings.Join(kept.kinds(one.ID), ",")
	want := strings.Join([]string{
		work.EventClaimed, work.EventStarted, work.EventReleased, work.EventClaimed, work.EventAnswered,
	}, ",")
	if got != want {
		t.Fatalf("the records read %q, want %q", got, want)
	}
	// The released record names the controller that stopped and the phase its work was found in.
	for _, event := range kept.recorded(one.ID) {
		if event.Kind != work.EventReleased {
			continue
		}
		if !strings.Contains(event.Detail, "controller-a") || !strings.Contains(event.Detail, work.PhaseRunning) {
			t.Fatalf("the released record says %q, want the controller that stopped and the phase it was found in", event.Detail)
		}
	}
	// And exactly one start, because a second start would be a second bill.
	starts := 0
	for _, kind := range kept.kinds(one.ID) {
		if kind == work.EventStarted {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("%d starts are on the record, want 1", starts)
	}
}

// Work that ended lets go of its lease. A lease left behind on finished work would make it look
// held forever to anything that reads the column.
func TestWorkThatEndedHoldsNoLease(t *testing.T) {
	controller, kept, plane := aLeasedController(t, "controller-a", time.Minute)
	one := kept.add(declaredWork("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.LeaseOwner != "" || got.LeaseUntil != nil {
		t.Fatalf("work that ended is held by %q until %v", got.LeaseOwner, got.LeaseUntil)
	}
}

// The lease is a number the crew can say out loud, and a controller given none takes the measured
// default rather than holding work forever.
func TestAControllerGivenNoLeaseTakesTheMeasuredDefault(t *testing.T) {
	kept, plane := newRows(), newCrew()
	controller := work.NewController(kept, plane, nil, nil, nil).Owned("controller-a")
	one := kept.add(declaredWork("read the electricity bill"))

	controller.Tick(context.Background())

	got := kept.get(one.ID)
	if got.LeaseUntil == nil {
		t.Fatal("the work is held with no end to the hold")
	}
	held := time.Until(*got.LeaseUntil)
	if held < work.DefaultLease-time.Second || held > work.DefaultLease+time.Second {
		t.Fatalf("the lease runs for about %s, want the default of %s", held.Round(time.Second), work.DefaultLease)
	}
}

// A controller with no name of its own still has one, or two controllers would be indistinguishable
// on the record and neither could tell its own work from the other's.
func TestAControllerAlwaysHasAName(t *testing.T) {
	kept, plane := newRows(), newCrew()
	first := work.NewController(kept, plane, nil, nil, nil)
	second := work.NewController(kept, plane, nil, nil, nil)
	one := kept.add(declaredWork("read the electricity bill"))
	two := declaredWork("pay the electricity bill")
	two.ID = "work-2"
	kept.add(two)

	first.Tick(context.Background())

	held := kept.get(one.ID).LeaseOwner
	if held == "" {
		t.Fatal("the work is held by nobody in particular")
	}
	if second.Owner() == first.Owner() {
		t.Fatalf("two controllers are both called %q, so neither can tell its own work from the other's", held)
	}
}

// waitForLeaseToRunOut waits out a lease a test made short on purpose, so a death is a lease that
// has expired rather than a clock a test had to fake.
func waitForLeaseToRunOut(t *testing.T, kept *rows, id string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		one := kept.get(id)
		if one.LeaseUntil == nil || one.LeaseUntil.Before(time.Now()) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the lease on %s still runs to %v", id, one.LeaseUntil)
		}
		time.Sleep(time.Millisecond)
	}
}

var _ = quaycrewv1.Work{}
