package capacity_test

import (
	"testing"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/capacity"
)

// The shape of the incident. Nine jobs were admitted inside one reading of the machine, because a
// container appears seconds after the job that asked for it and the reading is ten seconds wide.
// The ledger is what makes the second job count the first.
func TestTheSecondJobCountsTheFirstBeforeItsContainerExists(t *testing.T) {
	ledger := capacity.NewLedger()
	node := capacity.Node{
		Known:    true,
		Capacity: capacity.Request{Memory: 4 << 30, Processor: 400},
		Reserve:  capacity.Request{Memory: 1 << 30, Processor: 100},
	}
	want := capacity.Request{Memory: 1536 << 20, Processor: 100}

	// Two fit in the 3 gibibytes left, and nothing has been created yet.
	for _, key := range []string{"house-bills/job-one", "house-bills/job-two"} {
		if verdict := capacity.Fits(node, ledger.Placed(), want); !verdict.OK {
			t.Fatalf("%s was refused: %s", key, verdict.Reason)
		}
		ledger.Reserve(key, want)
	}
	if verdict := capacity.Fits(node, ledger.Placed(), want); verdict.OK {
		t.Fatal("a third job was admitted against two reservations nobody has built yet")
	}
}

// A job that never became a container gives its room back, or a machine loses a sandbox's worth of
// capacity to every dispatch that failed.
func TestRoomReleasedGoesBack(t *testing.T) {
	ledger := capacity.NewLedger()
	want := capacity.Request{Memory: 1 << 30, Processor: 100}
	ledger.Reserve("house-bills/job-one", want)
	ledger.Release("house-bills/job-one")
	if placed := ledger.Placed(); placed.Memory != 0 || placed.Processor != 0 {
		t.Fatalf("the ledger still holds %s", placed)
	}
}

// The container arrives under the key its reservation was taken with, so it replaces the promise
// rather than being counted beside it.
func TestAPlacedSandboxReplacesItsOwnReservation(t *testing.T) {
	ledger := capacity.NewLedger()
	want := capacity.Request{Memory: 1 << 30, Processor: 100}
	ledger.Reserve("house-bills/job-one", want)
	ledger.Place("house-bills/job-one", "session-a", want)
	if count := ledger.Count(); count != 1 {
		t.Fatalf("one sandbox counts as %d", count)
	}
	if placed := ledger.Placed(); placed.Memory != want.Memory {
		t.Fatalf("the ledger holds %s for one sandbox that asked for %s", placed, want)
	}
}

// The system closes a sandbox by session, and the room has to go back on that road too: a machine that
// keeps counting containers it has removed admits less work every time one is stopped.
func TestAClosedSandboxGivesItsRoomBackBySession(t *testing.T) {
	ledger := capacity.NewLedger()
	want := capacity.Request{Memory: 1 << 30, Processor: 100}
	ledger.Place("house-bills/job-one", "session-a", want)
	ledger.ReleaseSession("session-a")
	if placed := ledger.Placed(); placed.Memory != 0 {
		t.Fatalf("the ledger still holds %s for a container that is gone", placed)
	}
}

// After a restart the containers are still there and the ledger is empty. A system that started
// counting from zero would admit a whole machine's worth of work onto a full machine, which is the
// counting failure arriving again on every restart.
func TestSandboxesThatOutlivedTheSystemAreCounted(t *testing.T) {
	ledger := capacity.NewLedger()
	ledger.Seed([]string{"session-a", "session-b"}, capacity.DefaultRequest())
	if count := ledger.Count(); count != 2 {
		t.Fatalf("two sandboxes that survived a restart count as %d", count)
	}
	// And the one the system then adopts is the same sandbox, not a third.
	ledger.Place("house-bills/job-one", "session-a", capacity.DefaultRequest())
	if count := ledger.Count(); count != 2 {
		t.Fatalf("adopting a seeded sandbox made it %d", count)
	}
}

// A controller that died between reserving and dispatching would otherwise hold that room for the
// life of the system. The expiry is the backstop and never the mechanism: every road out of a start
// releases explicitly.
func TestAPromiseNobodyKeptRunsOut(t *testing.T) {
	now := time.Now()
	ledger := capacity.NewLedger().Clocked(func() time.Time { return now })
	ledger.Reserve("house-bills/job-one", capacity.DefaultRequest())
	if ledger.Count() != 1 {
		t.Fatal("the reservation was not taken")
	}
	now = now.Add(capacity.ReservedFor + time.Second)
	if count := ledger.Count(); count != 0 {
		t.Fatalf("a reservation nobody kept is still holding room after %s", capacity.ReservedFor)
	}
}

// A container that exists holds its room until it goes, however long that is. Only a promise runs
// out: a sandbox running a job for three hours is not a promise anybody forgot.
func TestAPlacedSandboxNeverRunsOut(t *testing.T) {
	now := time.Now()
	ledger := capacity.NewLedger().Clocked(func() time.Time { return now })
	ledger.Place("house-bills/job-one", "session-a", capacity.DefaultRequest())
	now = now.Add(24 * time.Hour)
	if count := ledger.Count(); count != 1 {
		t.Fatal("a running sandbox stopped being counted because it had been running a while")
	}
}

// The placement asks what is on the machine apart from itself. Counting its own reservation against
// itself would refuse every job the system had correctly admitted a moment earlier.
func TestAPlacementDoesNotCountItsOwnReservation(t *testing.T) {
	ledger := capacity.NewLedger()
	want := capacity.Request{Memory: 1 << 30, Processor: 100}
	ledger.Reserve("house-bills/job-one", want)
	ledger.Reserve("house-bills/job-two", want)
	without := ledger.Without("house-bills/job-one")
	if without.Memory != want.Memory {
		t.Fatalf("the machine holds %s apart from this sandbox, want one other sandbox", without)
	}
}
