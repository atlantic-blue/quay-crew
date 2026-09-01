package job_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/capacity"
	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// The machine the incident happened on, in the small: a fixed number of slots, one per sandbox, and
// a slot comes back only when a container goes.
//
// It counts rather than measuring bytes, because the fault is not in the arithmetic. What broke is
// that the reservation ends with the container and a session outlives its job, so a slot per
// container is the whole of what has to be modelled. The real ledger is held to the byte arithmetic
// by internal/capacity.
type aFullMachine struct {
	mu    sync.Mutex
	slots int
	// held is what each key is holding, and placed maps a session to the key its container took, which
	// is how a reclaim gives the slot back. The real system does this in Ledger.ReleaseSession.
	held   map[string]bool
	placed map[string]string
}

func aMachineWithSlots(slots int) *aFullMachine {
	return &aFullMachine{slots: slots, held: map[string]bool{}, placed: map[string]string{}}
}

func (m *aFullMachine) Admit(_ context.Context, key string, _ capacity.Request) capacity.Verdict {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.held[key] {
		return capacity.Verdict{OK: true}
	}
	if len(m.held) >= m.slots {
		return capacity.Verdict{
			OK: false, Resource: capacity.ResourceProcessor,
			Reason: fmt.Sprintf("there is not enough processor for this job's sandbox: it asks for "+
				"100%%, 0%% of %d00%% is unallocated", m.slots),
		}
	}
	m.held[key] = true
	return capacity.Verdict{OK: true}
}

func (m *aFullMachine) Release(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.held, key)
}

// place records that a session's container took the slot admitted under this key, the way the
// control plane does when it actually builds the sandbox.
func (m *aFullMachine) place(session, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.placed[session] = key
}

// releaseSession gives back the slot a container held, which happens when the container goes and at
// no other moment. That is the whole of the second fault: an idle sandbox keeps its container, so it
// keeps its slot, however long it has been doing nothing.
func (m *aFullMachine) releaseSession(session string) {
	m.mu.Lock()
	key, placed := m.placed[session]
	if placed {
		delete(m.placed, session)
		delete(m.held, key)
	}
	m.mu.Unlock()
}

func (m *aFullMachine) unallocated() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.slots - len(m.held)
}

// aQueue is a controller over a machine with this many slots, where a session that finishes keeps
// its container the way a real one does and a reclaim gives the slot back.
func aQueue(t *testing.T, slots int) (*job.Controller, *rows, *system, *aFullMachine, *eyes) {
	t.Helper()
	kept, plane, machine := newRows(), newSystem(), aMachineWithSlots(slots)
	plane.store = kept
	plane.machine = machine
	watching := &eyes{watching: map[string]bool{}}
	controller := job.NewController(kept, plane, nil, nil, nil).
		Placing(machine).Watching(watching).Owned("controller-1")
	return controller, kept, plane, machine, watching
}

// aJob is one declared job with an identifier of its own, for the tests that hold two at once. The
// shared declaredJob gives every job the same identifier, which is right for a test about one row
// and wrong for a test about a queue.
func aJob(id, title string) *job.Job {
	one := declaredJob(title)
	one.ID = id
	return one
}

// declaredJobNumbered is one of a queue of them.
func declaredJobNumbered(n int) *job.Job {
	return aJob(fmt.Sprintf("job-%02d", n), fmt.Sprintf("read bill %d", n))
}

// runToStandstill ticks until nothing more moves, landing every task the system starts, and answers
// how many ticks it took. It stops at a ceiling rather than looping for ever, because the failure
// this test is about is a system that never moves again.
func runToStandstill(t *testing.T, controller *job.Controller, plane *system, ticks int) {
	t.Helper()
	ctx := context.Background()
	for range ticks {
		controller.Tick(ctx)
		plane.lands("read it, it is due on the fourth")
		controller.Tick(ctx)
	}
}

// The deadlock itself. This is the state of the system on 31 August 2026: every job that could run
// has run, the sandboxes they left behind hold the whole machine, and the rest of the queue can
// never start.
//
// It is built here rather than only in the recovery test, because a recovery tested against a queue
// that was never stuck proves nothing. The one thing holding the rescue off is the one thing that
// legitimately holds it off: an operator is in every container. So the queue is genuinely stopped,
// the machine is genuinely full, and both are asserted.
func TestTheQueueDeadlocksWhenIdleSandboxesHoldTheMachine(t *testing.T) {
	controller, kept, plane, machine, watching := aQueue(t, 3)
	// The workspace has no reclaim time, which is how the system ships and how the reclaim was never
	// going to give this room back on its own.
	kept.allow(job.Limits{Workspace: "workspace-1"})
	watching.everything = true
	for i := range 8 {
		kept.add(declaredJobNumbered(i))
	}

	runToStandstill(t, controller, plane, 12)

	done, held := 0, 0
	for _, one := range kept.every() {
		switch {
		case one.Phase == job.PhaseDone:
			done++
		case one.Phase == job.PhasePending && one.Reason != "":
			held++
		}
	}
	if done != 3 || held != 5 {
		t.Fatalf("%d jobs finished and %d are held, want the machine's three slots used and the "+
			"other five waiting", done, held)
	}
	if machine.unallocated() != 0 {
		t.Fatalf("%d slots are unallocated, so the machine was never full and this is not the "+
			"deadlock", machine.unallocated())
	}
	if got := len(plane.reclaimed()); got != 0 {
		t.Fatalf("%d containers came back while somebody was in every one of them", got)
	}
	// And the queue says what it is, so a person reading it is not told the machine is merely busy.
	for _, one := range kept.every() {
		if one.Phase != job.PhasePending || one.Reason == "" {
			continue
		}
		if !strings.Contains(one.Reason, "stopped rather than busy") {
			t.Fatalf("a held job says %q, and nothing in it says this system is running nothing", one.Reason)
		}
	}
}

// The reclaim clock cannot answer this, whatever number it is set to. It measures how long a session
// has been quiet and never what the machine is holding, so a workspace that sets thirty minutes is a
// queue that waits thirty minutes and then waits for the next one. Half an hour of a stopped system,
// per container, is not a recovery.
func TestTheReclaimClockNeverLooksAtTheMachine(t *testing.T) {
	controller, kept, plane, machine, watching := aQueue(t, 3)
	kept.allow(job.Limits{Workspace: "workspace-1", ReclaimSeconds: 1800})
	watching.everything = true
	for i := range 8 {
		kept.add(declaredJobNumbered(i))
	}

	runToStandstill(t, controller, plane, 12)

	if machine.unallocated() != 0 {
		t.Fatalf("%d slots came back, and a reclaim time of thirty minutes cannot free a machine "+
			"whose sandboxes went idle a moment ago", machine.unallocated())
	}
	if got := len(plane.reclaimed()); got != 0 {
		t.Fatalf("%d containers came back inside the reclaim time", got)
	}
}

// And the recovery, over the same machine. The queue that could never move on its own now empties,
// with nobody declaring anything again and nobody setting a reclaim time.
func TestAStoppedQueueTakesAContainerBackAndStartsAgain(t *testing.T) {
	controller, kept, plane, _, _ := aQueue(t, 3)
	kept.allow(job.Limits{Workspace: "workspace-1"})
	for i := range 8 {
		kept.add(declaredJobNumbered(i))
	}

	runToStandstill(t, controller, plane, 30)

	for _, one := range kept.every() {
		if one.Phase != job.PhaseDone {
			t.Fatalf("job %s is %q saying %q, and every one of the eight should have run", one.ID,
				one.Phase, one.Reason)
		}
	}
}

// One container per tick, and the one idle longest. Taking every idle sandbox at once would throw
// away warm containers to answer a question that one container answers.
func TestOnlyOneContainerIsTakenBackPerTick(t *testing.T) {
	controller, kept, plane, _, _ := aQueue(t, 0)
	kept.allow(job.Limits{Workspace: "workspace-1"})
	kept.addSession(aSettledSession("session-oldest", 3*time.Hour))
	kept.addSession(aSettledSession("session-middle", 2*time.Hour))
	kept.addSession(aSettledSession("session-newest", time.Hour))
	kept.add(declaredJob("read the electricity bill"))

	controller.Tick(context.Background())

	if got := plane.reclaimed(); len(got) != 1 || got[0] != "session-oldest" {
		t.Fatalf("one tick took back %v, want the one session that has been idle longest", got)
	}
}

// The guard that must never break. A container an operator is typing into is not taken, however
// stopped the queue is, and a system that cannot tell reads that as attached.
func TestAContainerSomebodyIsInIsNeverTakenToUnstickTheQueue(t *testing.T) {
	controller, kept, plane, _, watching := aQueue(t, 0)
	kept.allow(job.Limits{Workspace: "workspace-1"})
	kept.addSession(aSettledSession("session-a", 3*time.Hour))
	watching.watching["session-a"] = true
	kept.add(declaredJob("read the electricity bill"))

	controller.Tick(context.Background())

	if got := plane.reclaimed(); len(got) != 0 {
		t.Fatalf("the system took back %v while somebody was in it", got)
	}
}

// A session the system cannot see into is left alone too. Being wrong one way holds a container a
// little longer; being wrong the other way closes a conversation under somebody's hands.
func TestASessionTheSystemCannotSeeIntoIsNeverTakenToUnstickTheQueue(t *testing.T) {
	controller, kept, plane, _, watching := aQueue(t, 0)
	kept.allow(job.Limits{Workspace: "workspace-1"})
	kept.addSession(aSettledSession("session-a", 3*time.Hour))
	watching.refuse = errors.New("the daemon did not answer")
	kept.add(declaredJob("read the electricity bill"))

	controller.Tick(context.Background())

	if got := plane.reclaimed(); len(got) != 0 {
		t.Fatalf("the system took back %v while it could not tell whether anybody was in it", got)
	}
}

// A working machine is not a stopped one. This is the case that decides between reclaiming on
// pressure and reclaiming on the pair, and getting it wrong takes a warm container from a session
// that is about to get its next task.
func TestAFullMachineWithSomethingRunningIsLeftAlone(t *testing.T) {
	controller, kept, plane, _, _ := aQueue(t, 0)
	kept.allow(job.Limits{Workspace: "workspace-1"})
	kept.addSession(aSettledSession("session-idle", 3*time.Hour))
	// Held by another controller, so this one leaves it running rather than reading its task and
	// landing it before the fifth comparison ever runs.
	running := kept.add(aJob("job-running", "the one that is working"))
	kept.claim(running.ID, "controller-2", time.Now().Add(time.Minute))
	kept.setSession(running.ID, "session-busy")
	kept.add(aJob("job-waiting", "the one that is waiting"))

	controller.Tick(context.Background())

	if got := plane.reclaimed(); len(got) != 0 {
		t.Fatalf("the system took back %v while a job was running, and a full machine with work on "+
			"it is a healthy machine", got)
	}
}

// A job waiting for a person is waiting correctly, not stalled, and its session is wanted alive.
func TestAJobAskingSomebodyCountsAsMoving(t *testing.T) {
	controller, kept, plane, _, _ := aQueue(t, 0)
	kept.allow(job.Limits{Workspace: "workspace-1"})
	kept.addSession(aSettledSession("session-idle", 3*time.Hour))
	asking := kept.add(aJob("job-asking", "the one that asked"))
	kept.setPhase(asking.ID, job.PhaseAsking)
	kept.add(aJob("job-waiting", "the one that is waiting"))

	controller.Tick(context.Background())

	if got := plane.reclaimed(); len(got) != 0 {
		t.Fatalf("the system took back %v while a job was waiting for a person to answer it", got)
	}
}

// An idle system is the ordinary state of one. Nothing running and nothing waiting must never read
// as a fault, or every quiet machine loses its containers.
func TestAnIdleSystemWithNothingWaitingIsLeftAlone(t *testing.T) {
	controller, kept, plane, _, _ := aQueue(t, 0)
	kept.allow(job.Limits{Workspace: "workspace-1"})
	kept.addSession(aSettledSession("session-a", 3*time.Hour))

	for range 5 {
		controller.Tick(context.Background())
	}

	if got := plane.reclaimed(); len(got) != 0 {
		t.Fatalf("the system took back %v on an idle machine with no work waiting for it", got)
	}
}

// The record of the rescue, on the job that was being denied. A container the system took back must
// never read the same as one that went for any other reason.
func TestFreeingRoomIsOnTheRecordOfTheJobItFreedItFor(t *testing.T) {
	controller, kept, plane, _, _ := aQueue(t, 0)
	kept.allow(job.Limits{Workspace: "workspace-1"})
	kept.addSession(aSettledSession("read-the-electricity-bill", 3*time.Hour))
	held := kept.add(declaredJob("read the electricity bill"))

	controller.Tick(context.Background())

	var said string
	for _, record := range kept.recorded(held.ID) {
		if record.Kind == job.EventUnstuck {
			said = record.Detail
		}
	}
	if said == "" {
		t.Fatalf("the records read %v, and none of them says the system started its own queue again",
			kept.kinds(held.ID))
	}
	for _, want := range []string{"nothing was running", "1 job was waiting", "read-the"} {
		if !strings.Contains(said, want) {
			t.Errorf("the record says %q, and it does not say %q", said, want)
		}
	}
	if plane.reclaimed()[0] != "read-the-electricity-bill" {
		t.Fatalf("the record is about session-a and the system took back %v", plane.reclaimed())
	}
}

// What a person reads on the job. "There is not enough processor" is the same sentence on a busy
// machine and on a stopped one, and only one of them means waiting is the right thing to do.
func TestAJobHeldWhileNothingRunsSaysTheSystemIsStopped(t *testing.T) {
	kept, plane := newRows(), newSystem()
	machine := aMachineWithSlots(0)
	controller := job.NewController(kept, plane, nil, nil, nil).Placing(machine)
	one := kept.add(declaredJob("read the electricity bill"))

	controller.Tick(context.Background())

	got := kept.get(one.ID).Reason
	if !strings.Contains(got, "not enough processor") {
		t.Fatalf("the job says %q, and it no longer says which resource ran out", got)
	}
	if !strings.Contains(got, "stopped rather than busy") {
		t.Fatalf("the job says %q, and nothing in it says the system is running nothing at all", got)
	}
}

// And it is written once. A controller ticks every few seconds and a stopped system stays stopped,
// so a write per tick is a row update and a record a second saying what the last one said.
func TestTheStoppedSentenceIsWrittenOnce(t *testing.T) {
	kept, plane := newRows(), newSystem()
	controller := job.NewController(kept, plane, nil, nil, nil).Placing(aMachineWithSlots(0))
	one := kept.add(declaredJob("read the electricity bill"))

	for range 6 {
		controller.Tick(context.Background())
	}

	held := 0
	for _, record := range kept.recorded(one.ID) {
		if record.Kind == job.EventHeld {
			held++
		}
	}
	if held != 1 {
		t.Fatalf("the record says the job was held %d times over six ticks, want 1", held)
	}
}

// A machine that is busy says the shorter sentence, because on a busy machine waiting is the right
// thing to do and telling somebody the system is stopped sends them to look at a machine that is fine.
func TestAJobHeldOnABusyMachineSaysNothingAboutBeingStopped(t *testing.T) {
	controller, kept, _, _, _ := aQueue(t, 0)
	kept.allow(job.Limits{Workspace: "workspace-1"})
	// Held by another controller, so this one leaves it running rather than reading its task and
	// landing it before the fifth comparison ever runs.
	running := kept.add(aJob("job-running", "the one that is working"))
	kept.claim(running.ID, "controller-2", time.Now().Add(time.Minute))
	kept.setSession(running.ID, "session-busy")
	waiting := kept.add(aJob("job-waiting", "the one that is waiting"))

	controller.Tick(context.Background())

	if got := kept.get(waiting.ID).Reason; strings.Contains(got, "stopped rather than busy") {
		t.Fatalf("a job held on a machine with work running on it says %q", got)
	}
}

// The starvation. Twenty rows whose container has already gone must not hide the one row that still
// holds a container: that is what stopped the reclaim reaching a sandbox for an hour.
func TestReclaimedSessionsDoNotCrowdASandboxOutOfTheBatch(t *testing.T) {
	controller, kept, plane, _, _ := aQueue(t, 1)
	kept.allow(job.Limits{Workspace: "workspace-1", ReclaimSeconds: 1800})
	for i := range 25 {
		kept.addSession(aReclaimedSession(fmt.Sprintf("session-gone-%02d", i),
			2*time.Hour+time.Duration(i)*time.Minute))
	}
	for i := range 12 {
		kept.addSession(aSettledSession(fmt.Sprintf("session-live-%02d", i), time.Hour))
	}

	controller.Tick(context.Background())

	if got := plane.reclaimed(); len(got) == 0 {
		t.Fatal("one tick took back nothing, and twelve sessions are an hour past a thirty minute " +
			"reclaim time: the rows whose container has already gone took the whole batch")
	}
}

// The other half of that split: with no archive time set, the rows waiting to be filed stay waiting
// and take nothing with them.
func TestSessionsWaitingToBeFiledAreNotTakenForSandboxes(t *testing.T) {
	controller, kept, plane, _, _ := aQueue(t, 1)
	kept.allow(job.Limits{Workspace: "workspace-1", ReclaimSeconds: 1800})
	kept.addSession(aReclaimedSession("session-gone", 3*time.Hour))

	controller.Tick(context.Background())

	if got := plane.reclaimed(); len(got) != 0 {
		t.Fatalf("the system reclaimed %v, and a session whose container has already gone has "+
			"nothing left to take", got)
	}
	if got := plane.archived(); len(got) != 0 {
		t.Fatalf("the system archived %v with no archive time set", got)
	}
}

var _ = quaycrewv1.Session{}
