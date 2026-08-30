package room_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/atlantic-blue/krewe/internal/room"
)

// machine builds the accounting a sandbox reads, so a test says what the kernel is reporting rather
// than what this package does with it.
type machine struct {
	total, available string
	max, current     string
	peak             string
	events           string
}

func (m machine) fs() fstest.MapFS {
	files := fstest.MapFS{
		"proc/meminfo": &fstest.MapFile{Data: []byte(
			"MemTotal:       " + m.total + " kB\n" +
				"MemFree:          208832 kB\n" +
				"MemAvailable:   " + m.available + " kB\n")},
	}
	for name, value := range map[string]string{
		"memory.max": m.max, "memory.current": m.current, "memory.peak": m.peak, "memory.events": m.events,
	} {
		if value == "" {
			continue
		}
		files["sys/fs/cgroup/"+name] = &fstest.MapFile{Data: []byte(value + "\n")}
	}
	return files
}

// noLimit is the sandbox this issue was raised about: 8 gigabytes advertised, and what the rest of
// the machine has not taken actually there.
var noLimit = machine{total: "8024876", available: "1539300", max: "max", current: "294182912"}

// TestASandboxWithNoLimitAdvertisesTheWholeMachine. Every tool in a container reads MemTotal, so a
// container with no limit of its own tells node, Go, jest and webpack that eight gigabytes are
// theirs. The kernel kills them against what is free instead.
func TestASandboxWithNoLimitAdvertisesTheWholeMachine(t *testing.T) {
	reading, err := room.Read(noLimit.fs())
	if err != nil {
		t.Fatalf("reading the machine: %v", err)
	}
	if reading.Limited() {
		t.Fatalf("a sandbox whose memory.max reads max is reported as limited")
	}
	if reading.Total != 8024876*1024 {
		t.Fatalf("what the sandbox advertises is %d, want %d", reading.Total, 8024876*1024)
	}
	if reading.Free() != 1539300*1024 {
		t.Fatalf("what is free is %d, want %d", reading.Free(), 1539300*1024)
	}

	said := room.Say(reading)
	for _, want := range []string{"no memory limit of its own", "7836 MiB", "1503 MiB"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the session is not told %q:\n%s", want, said)
		}
	}
}

// TestALimitedSandboxIsNeverToldItHasMoreThanIsLeftOfItsLimit. A limit is the point at which the
// kernel takes the process, so a session that budgeted against the machine's free memory would still
// be killed by its own limit first.
func TestALimitedSandboxIsNeverToldItHasMoreThanIsLeftOfItsLimit(t *testing.T) {
	limited := machine{
		total: "8024876", available: "1539300",
		max: "2147483648", current: "1073741824", peak: "1181116006",
	}
	reading, err := room.Read(limited.fs())
	if err != nil {
		t.Fatalf("reading the machine: %v", err)
	}
	if !reading.Limited() {
		t.Fatalf("a sandbox with a memory.max is reported as unlimited")
	}
	if want := int64(1073741824); reading.Free() != want {
		t.Fatalf("what is free is %d, want what is left of the limit, %d", reading.Free(), want)
	}

	said := room.Say(reading)
	for _, want := range []string{"may take 2048 MiB", "free right now", "1024 MiB", "the most it has held"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the session is not told %q:\n%s", want, said)
		}
	}
}

// TestALimitedSandboxIsToldWhatTheMachineHasWhenThatIsLess. A limit is a ceiling, never a
// reservation: a sandbox allowed four gigabytes on a machine with one left still has one.
func TestALimitedSandboxIsToldWhatTheMachineHasWhenThatIsLess(t *testing.T) {
	tight := machine{total: "8024876", available: "1048576", max: "4294967296", current: "268435456"}
	reading, err := room.Read(tight.fs())
	if err != nil {
		t.Fatalf("reading the machine: %v", err)
	}
	if want := int64(1048576 * 1024); reading.Free() != want {
		t.Fatalf("what is free is %d, want what the machine has, %d", reading.Free(), want)
	}
}

// TestTheKillCountsSayWhichMemoryRanOut. This is the whole reason the pair is read. Inside a
// container the kernel log is not readable, so exit 137 carries nothing. A kill by the machine's own
// out of memory killer raises oom_kill and leaves max at zero, because a sandbox with no limit never
// reaches one. Measured on this machine: the counters went from all zero to oom_kill 1, max 0.
func TestTheKillCountsSayWhichMemoryRanOut(t *testing.T) {
	machineRanOut := noLimit
	machineRanOut.events = "low 0\nhigh 0\nmax 0\noom 0\noom_kill 1\noom_group_kill 0"
	reading, err := room.Read(machineRanOut.fs())
	if err != nil {
		t.Fatalf("reading the machine: %v", err)
	}
	if reading.Kills != 1 || reading.AtLimit != 0 {
		t.Fatalf("the counters read kills %d at limit %d, want 1 and 0", reading.Kills, reading.AtLimit)
	}
	said := room.Say(reading)
	if !strings.Contains(said, "the machine ran out rather than this session") {
		t.Fatalf("the session is not told which memory ran out:\n%s", said)
	}

	sessionRanOut := machine{
		total: "8024876", available: "4194304", max: "2147483648", current: "2147483648",
		events: "low 0\nhigh 0\nmax 12\noom 2\noom_kill 3\noom_group_kill 0",
	}
	reading, err = room.Read(sessionRanOut.fs())
	if err != nil {
		t.Fatalf("reading the machine: %v", err)
	}
	said = room.Say(reading)
	if !strings.Contains(said, "took 3 processes") || !strings.Contains(said, "this\nsession ran out") {
		t.Fatalf("the session is not told that it ran out itself:\n%s", said)
	}
}

// TestASandboxThatWasNeverKilledIsNotToldItWas. A count of zero is not news, and a warning nobody
// needs is one a reader learns to skip.
func TestASandboxThatWasNeverKilledIsNotToldItWas(t *testing.T) {
	reading, err := room.Read(noLimit.fs())
	if err != nil {
		t.Fatalf("reading the machine: %v", err)
	}
	if said := room.Say(reading); strings.Contains(said, "out of memory killer took") {
		t.Fatalf("a sandbox nothing was killed in is told something was:\n%s", said)
	}
}

// TestEverySessionIsToldTheSameThingToDoAboutAGateThatDoesNotFit, which is the point of the advice
// living in the tool. A session that invents the answer each time gives a different one each time.
func TestEverySessionIsToldTheSameThingToDoAboutAGateThatDoesNotFit(t *testing.T) {
	said := room.Say(mustRead(t, noLimit.fs()))
	for _, want := range []string{
		"exit 137",
		"NODE_OPTIONS=--max-old-space-size",
		"GOMEMLIMIT",
		"--maxWorkers=1",
		"say what you could not run",
		"Never\nreport a partial check as a check",
	} {
		if !strings.Contains(said, want) {
			t.Fatalf("the advice does not say %q:\n%s", want, said)
		}
	}
}

// TestAMachineWithNoAccountingIsRefusedWithTheReason, rather than reporting zero of everything. A
// sandbox is not the only place this command runs, and zero megabytes free would be read as a
// finding.
func TestAMachineWithNoAccountingIsRefusedWithTheReason(t *testing.T) {
	_, err := room.Read(fstest.MapFS{})
	if err == nil {
		t.Fatalf("a machine with no accounting was read as a sandbox")
	}
	if !strings.Contains(err.Error(), "linux sandbox") {
		t.Fatalf("the refusal does not say what it wanted: %v", err)
	}
}

// TestTheControlGroupFilesAreOptional. A sandbox on a machine whose kernel keeps none of this still
// gets the two numbers that matter.
func TestTheControlGroupFilesAreOptional(t *testing.T) {
	bare := machine{total: "8024876", available: "1539300"}
	reading, err := room.Read(bare.fs())
	if err != nil {
		t.Fatalf("reading a machine with no control group files: %v", err)
	}
	if reading.Limited() || reading.Held != 0 {
		t.Fatalf("a machine with no control group files reports a limit: %+v", reading)
	}
	if said := room.Say(reading); strings.Contains(said, "the most it has held") {
		t.Fatalf("a peak nobody measured is reported:\n%s", said)
	}
}

func mustRead(t *testing.T, files fstest.MapFS) room.Reading {
	t.Helper()
	reading, err := room.Read(files)
	if err != nil {
		t.Fatalf("reading the machine: %v", err)
	}
	return reading
}
