package capacity_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/krewe/internal/capacity"
)

// The machine that broke: a runtime holding 7,653 mebibytes and fourteen processors, with the system's
// own containers taking 2,048 mebibytes and two processors out of it. What is left for sandboxes is
// 5,605 mebibytes and 1,200 per cent.
func aMachine() capacity.Node {
	return capacity.Node{
		Known:    true,
		Capacity: capacity.Request{Memory: mib(7653), Processor: 1400},
		Reserve:  capacity.Request{Memory: mib(2048), Processor: 200},
	}
}

func mib(count int64) int64 { return count << 20 }

func TestAnEmptyMachineAdmitsASandbox(t *testing.T) {
	verdict := capacity.Fits(aMachine(), capacity.Request{}, capacity.DefaultRequest())
	if !verdict.OK {
		t.Fatalf("an empty machine refused a sandbox: %s", verdict.Reason)
	}
	if verdict.Unmeasured {
		t.Error("a machine the system read reads as unmeasured")
	}
}

// The arithmetic, at the point where it turns over. Allocatable memory is 5,605 mebibytes, so three
// sandboxes at 1,536 fit with 997 to spare and the fourth is 509 short.
func TestTheBoundaryIsTheLastSandboxThatFits(t *testing.T) {
	node, want := aMachine(), capacity.DefaultRequest()
	placed := capacity.Request{}
	for fitted := 1; fitted <= 3; fitted++ {
		if verdict := capacity.Fits(node, placed, want); !verdict.OK {
			t.Fatalf("sandbox %d was refused with %s free: %s",
				fitted, capacity.Memory(node.Allocatable().Memory-placed.Memory), verdict.Reason)
		}
		placed = placed.Plus(want)
	}
	verdict := capacity.Fits(node, placed, want)
	if verdict.OK {
		t.Fatal("a fourth sandbox was admitted onto a machine with room for three")
	}
	if verdict.Resource != capacity.ResourceMemory {
		t.Errorf("the fourth was refused for %q, want memory", verdict.Resource)
	}
}

// Exactly what is left, to the byte. A machine with room for precisely one more sandbox admits it:
// the check is what fits, not what fits comfortably, and a system that refused this would strand the
// last sandbox on every machine.
func TestASandboxThatFitsExactlyIsAdmitted(t *testing.T) {
	node := capacity.Node{
		Known:    true,
		Capacity: capacity.Request{Memory: mib(4096), Processor: 400},
		Reserve:  capacity.Request{Memory: mib(1024), Processor: 100},
	}
	placed := capacity.Request{Memory: mib(1536), Processor: 200}
	want := capacity.Request{Memory: mib(1536), Processor: 100}
	if verdict := capacity.Fits(node, placed, want); !verdict.OK {
		t.Fatalf("a sandbox that fits exactly was refused: %s", verdict.Reason)
	}
	// And one byte more does not.
	want.Memory++
	if verdict := capacity.Fits(node, placed, want); verdict.OK {
		t.Fatal("a sandbox one byte over what is left was admitted")
	}
}

// Memory was not the axis that broke. Eight sandboxes held 913 per cent of a processor on a fourteen
// processor machine and the daemon stopped answering, with memory to spare the whole time.
func TestAMachineWithMemoryLeftAndNoProcessorsRefuses(t *testing.T) {
	node := capacity.Node{
		Known:    true,
		Capacity: capacity.Request{Memory: mib(23996), Processor: 1400},
		Reserve:  capacity.Request{Memory: mib(2048), Processor: 200},
	}
	// Twelve sandboxes: 18 gibibytes of the 21 available, and every processor spoken for.
	placed := capacity.Request{Memory: mib(1536) * 12, Processor: 1200}
	verdict := capacity.Fits(node, placed, capacity.DefaultRequest())
	if verdict.OK {
		t.Fatal("a machine with no processors left admitted a sandbox because it had memory")
	}
	if verdict.Resource != capacity.ResourceProcessor {
		t.Fatalf("it was refused for %q, want processor", verdict.Resource)
	}
	if !strings.Contains(verdict.Reason, "processor") {
		t.Errorf("the reason does not name the resource: %s", verdict.Reason)
	}
}

// The reason is what an operator acts on, so it carries the three figures that make the arithmetic
// checkable against the room view: what was asked for, what was left, and what there is in total.
func TestTheRefusalSaysWhatWasAskedForAndWhatWasLeft(t *testing.T) {
	node := aMachine()
	placed := capacity.Request{Memory: mib(5000), Processor: 300}
	verdict := capacity.Fits(node, placed, capacity.Request{Memory: mib(1536), Processor: 100})
	if verdict.OK {
		t.Fatal("that sandbox does not fit and it was admitted")
	}
	for _, figure := range []string{"1536 MiB", "605 MiB", "5605 MiB"} {
		if !strings.Contains(verdict.Reason, figure) {
			t.Errorf("the reason does not carry %s: %s", figure, verdict.Reason)
		}
	}
}

// A system that cannot read its runtime admits work and says so. Refusing everything would stop dead
// every system whose sessions do not run on a daemon at all, and there is no arithmetic to do for one.
func TestAMachineNobodyReadAdmitsAndSaysItIsUnmeasured(t *testing.T) {
	verdict := capacity.Fits(capacity.Node{}, capacity.Request{}, capacity.DefaultRequest())
	if !verdict.OK {
		t.Fatal("a system with no runtime to read refused a job")
	}
	if !verdict.Unmeasured {
		t.Fatal("it admitted the job without saying the arithmetic never happened")
	}
}

// A reserve larger than the machine leaves nothing to give out. The other way round it would leave a
// negative amount, and a negative amount is a machine that admits everything.
func TestAReserveLargerThanTheMachineAdmitsNothing(t *testing.T) {
	node := capacity.Node{
		Known:    true,
		Capacity: capacity.Request{Memory: mib(1024), Processor: 100},
		Reserve:  capacity.Request{Memory: mib(4096), Processor: 400},
	}
	if left := node.Allocatable(); left.Memory != 0 || left.Processor != 0 {
		t.Fatalf("allocatable is %s, want nothing", left)
	}
	if verdict := capacity.Fits(node, capacity.Request{}, capacity.DefaultRequest()); verdict.OK {
		t.Fatal("a machine reserved down to nothing admitted a sandbox")
	}
}

// A workspace that declared one half of a request takes the system's own for the other. Two numbers,
// separately, because a workspace whose jobs are memory heavy and processor light is a real thing.
func TestAWorkspaceRequestStandsInFrontOfTheSystemsOwn(t *testing.T) {
	want := capacity.Request{Memory: mib(4096)}.Or(capacity.DefaultRequest())
	if want.Memory != mib(4096) {
		t.Errorf("the workspace's memory was overwritten: %s", want)
	}
	if want.Processor != capacity.RequestProcessor {
		t.Errorf("the system's own processor request was not taken: %s", want)
	}
}

// One string on both sides of the dispatch, and different projects are different machines' worth of
// room even when their handles read the same.
func TestTheKeyTellsTwoProjectsApart(t *testing.T) {
	if capacity.KeyFor("house-bills", "review") == capacity.KeyFor("garden", "review") {
		t.Fatal("two projects with one handle share a key, so one sandbox counts for two")
	}
}
