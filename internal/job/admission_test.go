package job_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/capacity"
	"github.com/atlantic-blue/quay-crew/internal/job"
)

// A machine, as the controller sees it. It answers the one question a controller asks before it
// starts anything, and it records what was asked so a test can say the arithmetic happened rather
// than infer it from an outcome that might have come from anywhere.
type aMachine struct {
	mu sync.Mutex
	// full is what the machine says: nothing fits while it is set.
	full bool
	// held is what is reserved on it, by key.
	held map[string]capacity.Request
	// asked counts the admissions, and released the room given back.
	asked, released int
}

func newMachine() *aMachine { return &aMachine{held: map[string]capacity.Request{}} }

func (m *aMachine) Admit(_ context.Context, key string, want capacity.Request) capacity.Verdict {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.asked++
	if m.full {
		return capacity.Verdict{
			OK: false, Resource: capacity.ResourceMemory,
			Reason: "there is not enough memory for this job's sandbox: it asks for 1536 MiB, " +
				"512 MiB of 5605 MiB is unallocated",
		}
	}
	m.held[key] = want
	return capacity.Verdict{OK: true}
}

func (m *aMachine) Release(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.held, key)
	m.released++
}

func (m *aMachine) holding() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.held)
}

// The acceptance test for the whole change, in one scenario: a job the machine cannot host waits,
// and the system says which resource ran out. It is not dispatched, so it cannot be failed on a
// timeout two minutes later, which is what took the runtime down on 30 August 2026.
func TestAJobTheMachineHasNoRoomForWaitsRatherThanRunning(t *testing.T) {
	kept, plane := newRows(), newSystem()
	machine := newMachine()
	machine.full = true
	controller := job.NewController(kept, plane, nil, nil, nil).Placing(machine)
	one := kept.add(declaredJob("read the electricity bill"))

	controller.Tick(context.Background())

	if plane.sent() != 0 {
		t.Fatalf("the system was asked to run %d tasks on a full machine, want 0", plane.sent())
	}
	got := kept.get(one.ID)
	if got.Phase != job.PhasePending {
		t.Fatalf("the job is %q, want pending: a machine that is full now has room later", got.Phase)
	}
	if !strings.Contains(got.Reason, "not enough memory") {
		t.Fatalf("the job says %q, and it does not name the resource that ran out", got.Reason)
	}
	if got.Attempts != 0 {
		t.Errorf("the job has %d attempts against it, and it never started", got.Attempts)
	}
}

// And it runs the moment there is room, without anybody touching it. The job was never failed, so
// there is nothing to declare again.
func TestTheHeldJobRunsOnceTheMachineHasRoom(t *testing.T) {
	kept, plane := newRows(), newSystem()
	machine := newMachine()
	machine.full = true
	controller := job.NewController(kept, plane, nil, nil, nil).Placing(machine)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	if kept.get(one.ID).Phase != job.PhasePending {
		t.Fatal("the job did not wait")
	}

	machine.mu.Lock()
	machine.full = false
	machine.mu.Unlock()
	controller.Tick(ctx)

	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks once there was room, want 1", plane.sent())
	}
	got := kept.get(one.ID)
	if got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q, want running", got.Phase)
	}
	if got.Reason != "" {
		t.Errorf("a running job still carries %q, which described the wait it is out of", got.Reason)
	}
}

// The reason is written once rather than every tick. A controller ticks every few seconds and a
// machine stays full for minutes, so a write per tick is a row update and a record a second saying
// what the last one said.
func TestAHeldJobIsWrittenOnceForOneReason(t *testing.T) {
	kept, plane := newRows(), newSystem()
	machine := newMachine()
	machine.full = true
	controller := job.NewController(kept, plane, nil, nil, nil).Placing(machine)
	one := kept.add(declaredJob("read the electricity bill"))

	for range 4 {
		controller.Tick(context.Background())
	}

	held := 0
	for _, record := range kept.recorded(one.ID) {
		if record.Kind == job.EventHeld {
			held++
		}
	}
	if held != 1 {
		t.Fatalf("the record says the job was held %d times over four ticks, want 1", held)
	}
}

// Room taken for a job that never became a sandbox goes back. Otherwise a machine loses a sandbox's
// worth of capacity to every dispatch that failed, and admits less work after every failure.
func TestRoomIsGivenBackWhenTheDispatchFails(t *testing.T) {
	kept, plane := newRows(), newSystem()
	machine := newMachine()
	controller := job.NewController(kept, plane, nil, nil, nil).Placing(machine)
	plane.refuse = errors.New("no sandbox could be made")
	one := kept.add(declaredJob("read the electricity bill"))

	controller.Tick(context.Background())

	if got := kept.get(one.ID); got.Phase != job.PhaseFailed {
		t.Fatalf("the job is %q, want failed", got.Phase)
	}
	if machine.holding() != 0 {
		t.Fatalf("the machine is still holding room for %d sandboxes that were never made",
			machine.holding())
	}
}

// A job that reaches a container keeps its room, under the key the system will place the container
// with. Releasing here would let the next job in against capacity that is already spoken for, which
// is the burst that put nine jobs on a machine with room for eight.
func TestAJobThatDispatchedKeepsItsRoom(t *testing.T) {
	kept, plane := newRows(), newSystem()
	machine := newMachine()
	controller := job.NewController(kept, plane, nil, nil, nil).Placing(machine)
	one := kept.add(declaredJob("read the electricity bill"))

	controller.Tick(context.Background())

	if machine.holding() != 1 {
		t.Fatalf("the machine holds room for %d sandboxes, want 1", machine.holding())
	}
	key := capacity.KeyFor(one.Project, job.SessionFor(one.ID))
	machine.mu.Lock()
	defer machine.mu.Unlock()
	if _, found := machine.held[key]; !found {
		t.Fatalf("the room is held under %v, and the system will place the container under %q",
			keysOf(machine.held), key)
	}
}

// A controller with no machine to ask runs what it reads, which is what every controller did before
// admission was arithmetic. A system whose sessions do not run on a daemon has no runtime to read.
func TestAControllerWithNoMachineStillRunsJobs(t *testing.T) {
	controller, kept, plane := aController(t)
	kept.add(declaredJob("read the electricity bill"))

	controller.Tick(context.Background())

	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1", plane.sent())
	}
}

func keysOf(held map[string]capacity.Request) []string {
	out := make([]string, 0, len(held))
	for key := range held {
		out = append(out, key)
	}
	return out
}
