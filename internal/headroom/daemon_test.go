package headroom

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

// These read the daemon's own output, so they are in the package rather than beside it: the parsers
// are where a wrong figure would come from, and a wrong figure here is an operator stopping the
// wrong session.

func TestTheLimitIsReadFromTheDaemonRatherThanTheMachine(t *testing.T) {
	limit, processors, name := parseInfo([]byte("8217579520\t14\tDocker Desktop\n"))
	if !processors.Known() || processors.Percent() != 1400 {
		t.Errorf("processors = %v, want 1400 per cent, which is fourteen of them", processors)
	}
	if !limit.Known() || limit.Bytes() != 8217579520 {
		t.Fatalf("the limit reads %s", limit)
	}
	if name != "Docker Desktop" {
		t.Fatalf("the machine is named %q", name)
	}
}

func TestADaemonThatWillNotSayItsMemoryIsUnknown(t *testing.T) {
	tests := []struct {
		name string
		out  string
	}{
		{"nothing at all", ""},
		{"a word where the number goes", "lots\tDocker Desktop"},
		{"zero, which no daemon has", "0\tDocker Desktop"},
		{"a negative count", "-1\tDocker Desktop"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limit, _, _ := parseInfo([]byte(test.out))
			if limit.Known() {
				t.Fatalf("the limit reads %s, and nothing measured it", limit)
			}
		})
	}
}

func TestEveryContainerCountsTowardsWhatTheDaemonHolds(t *testing.T) {
	// One sandbox, one of the crew's own services, and one container belonging to something else.
	// All three take memory from the same cap, so all three count.
	out := strings.Join([]string{
		"quaycrew-a00d36d6454a3de66d02c6a3\t1.201GiB / 7.653GiB\t42.50%",
		"quaycrew-postgres-1\t34MiB / 7.653GiB\t0.10%",
		"somebody-elses-build\t512MiB / 7.653GiB\t7.00%",
	}, "\n")
	used, _, boxes := parseStats([]byte(out))
	if !used.Known() {
		t.Fatal("the total is unknown and every line was readable")
	}
	want := gibibytes(1.201) + 34*(1<<20) + 512*(1<<20)
	if used.Bytes() != want {
		t.Fatalf("the total is %d bytes, want %d", used.Bytes(), want)
	}
	if len(boxes) != 1 {
		t.Fatalf("%d sandboxes came back, want the one container named after a session", len(boxes))
	}
	if boxes[0].Session != "a00d36d6454a3de66d02c6a3" {
		t.Fatalf("the sandbox belongs to %q", boxes[0].Session)
	}
	if got := boxes[0].Processor.String(); got != "42.5%" {
		t.Fatalf("its processor share reads %q", got)
	}
}

// Rule five. A container the daemon could not read leaves the total unknown rather than silently
// making the machine look emptier than it is.
func TestAContainerTheDaemonCouldNotReadLeavesTheTotalUnknown(t *testing.T) {
	out := strings.Join([]string{
		"quaycrew-a00d36d6454a3de66d02c6a3\t1.201GiB / 7.653GiB\t42.50%",
		"quaycrew-b11e47e7565b4ef77e13d7b4\t-- / --\t--",
	}, "\n")
	used, _, boxes := parseStats([]byte(out))
	if used.Known() {
		t.Fatalf("the total reads %s while one container was not readable", used)
	}
	if len(boxes) != 2 {
		t.Fatalf("%d sandboxes came back, want both", len(boxes))
	}
	if boxes[1].Held.Known() {
		t.Fatalf("the unreadable sandbox reads %s", boxes[1].Held)
	}
	if boxes[1].Processor.Known() {
		t.Fatalf("the unreadable sandbox has a processor share of %s", boxes[1].Processor)
	}
}

// A daemon holding nothing is a reading. It is not the same answer as a daemon that would not say,
// and the two must not collapse into one.
func TestADaemonHoldingNothingIsAMeasuredZero(t *testing.T) {
	used, _, boxes := parseStats([]byte("  \n"))
	if !used.Known() || used.Bytes() != 0 {
		t.Fatalf("an empty daemon reads %s, want a measured zero", used)
	}
	if len(boxes) != 0 {
		t.Fatalf("%d sandboxes came back from an empty daemon", len(boxes))
	}
}

func TestALineTheCrewCannotReadLeavesTheTotalUnknown(t *testing.T) {
	used, _, _ := parseStats([]byte("quaycrew-postgres-1\t34MiB"))
	if used.Known() {
		t.Fatalf("a line with a field missing still gave a total of %s", used)
	}
}

func TestTheUnitsTheDaemonPrints(t *testing.T) {
	tests := []struct {
		text  string
		bytes int64
		ok    bool
	}{
		{"512B / 7.653GiB", 512, true},
		{"1.5KiB / 7.653GiB", 1536, true},
		{"34MiB / 7.653GiB", 34 * (1 << 20), true},
		{"1.201GiB / 7.653GiB", gibibytes(1.201), true},
		{"2TiB / 7.653GiB", 2 * (1 << 40), true},
		{"1.6MB / 7.653GB", 1_600_000, true},
		{"", 0, false},
		{"-- / --", 0, false},
		{"lots / 7.653GiB", 0, false},
		{"12Zib / 7.653GiB", 0, false},
		{"-4MiB / 7.653GiB", 0, false},
	}
	for _, test := range tests {
		t.Run(test.text, func(t *testing.T) {
			held, ok := parseSize(test.text)
			if ok != test.ok {
				t.Fatalf("%q parsed %v, want %v", test.text, ok, test.ok)
			}
			if ok && held.Bytes() != test.bytes {
				t.Fatalf("%q is %d bytes, want %d", test.text, held.Bytes(), test.bytes)
			}
			if !ok && held.Known() {
				t.Fatalf("%q came back measured", test.text)
			}
		})
	}
}

// A share nothing measured is unknown and never zero. A session reported at no processor share is a
// session an operator reads as idle and stops.
func TestAProcessorShareNothingMeasuredIsUnknown(t *testing.T) {
	for _, text := range []string{"", "--", "  ", "lots%", "-3.00%"} {
		if share := parsePercent(text); share.Known() {
			t.Fatalf("%q came back as %s", text, share)
		}
	}
	if share := parsePercent(" 0.00% "); !share.Known() || share.Percent() != 0 {
		t.Fatalf("a measured nothing reads %s", share)
	}
}

// Only a container named after a session is a sandbox. The compose project is called quaycrew too,
// so its own services carry the same prefix, and stopping one of those is stopping the crew.
func TestOnlyAContainerNamedAfterASessionIsASandbox(t *testing.T) {
	tests := []struct {
		container string
		session   string
		sandbox   bool
	}{
		{"quaycrew-a00d36d6454a3de66d02c6a3", "a00d36d6454a3de66d02c6a3", true},
		{"quaycrew-postgres-1", "", false},
		{"quaycrew-controlplane-1", "", false},
		{"quaycrew-a00d36d6454a3de66d02c6", "", false},
		{"quaycrew-A00D36D6454A3DE66D02C6A3", "", false},
		{"quaycrew-zzzz36d6454a3de66d02c6a3", "", false},
		{"something-else", "", false},
	}
	for _, test := range tests {
		t.Run(test.container, func(t *testing.T) {
			session, isSandbox := sessionOf(test.container)
			if isSandbox != test.sandbox || session != test.session {
				t.Fatalf("%q read as (%q, %v)", test.container, session, isSandbox)
			}
		})
	}
}

// Rule four. One read that failed must not take the others with it, and it must never fail a
// command: the sample comes back with what it could read and says what it could not.
func TestOneReadFailingLeavesTheRestOfTheSampleStanding(t *testing.T) {
	daemon := Daemon{
		Root: machineWith("MemTotal:       8024876 kB\nMemAvailable:   1539300 kB\n"),
		Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if args[0] == "info" {
				return []byte("8217579520\t14\tDocker Desktop"), nil
			}
			return nil, fmt.Errorf("the daemon is not answering")
		},
	}
	sample, err := daemon.Sample(context.Background())
	if err != nil {
		t.Fatalf("the whole sample failed over one read: %v", err)
	}
	if !sample.Limit.Known() {
		t.Fatal("the limit was read and did not survive the other read failing")
	}
	if sample.Used.Known() {
		t.Fatalf("what the containers hold reads %s, and that read failed", sample.Used)
	}
	if sample.State() != StateUnknown {
		t.Fatalf("the state reads %q on half a sample", sample.State())
	}
	if !strings.Contains(sample.Failed, "not answering") {
		t.Fatalf("the sample does not say what failed: %q", sample.Failed)
	}
}

// A machine that keeps no memory accounting is every Mac. The daemon's own figures still stand, and
// the machine's read unknown rather than zero.
func TestAMachineWithNoAccountingLeavesTheDaemonsFiguresStanding(t *testing.T) {
	daemon := Daemon{
		Root: fstest.MapFS{},
		Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if args[0] == "info" {
				return []byte("8217579520\t14\tDocker Desktop"), nil
			}
			return []byte("quaycrew-postgres-1\t34MiB / 7.653GiB\t0.10%"), nil
		},
	}
	sample, err := daemon.Sample(context.Background())
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if !sample.Limit.Known() || !sample.Used.Known() {
		t.Fatal("the daemon's own figures did not survive a machine with no accounting")
	}
	if sample.Machine.Total.Known() || sample.Machine.SwapUsed.Known() {
		t.Fatalf("the machine reads %s and %s, and there is no accounting to read",
			sample.Machine.Total, sample.Machine.SwapUsed)
	}
	if sample.Machine.Name != "Docker Desktop" {
		t.Fatalf("the machine is named %q", sample.Machine.Name)
	}
}

// Rule three, read from the machine rather than from the daemon.
func TestTheMachinesSwapIsReadWhereTheAccountingCarriesIt(t *testing.T) {
	daemon := Daemon{
		Root: machineWith("MemTotal:       8024876 kB\nMemAvailable:   1539300 kB\n" +
			"SwapTotal:      17825792 kB\nSwapFree:       1029120 kB\n"),
		Run: func(context.Context, string, ...string) ([]byte, error) { return []byte(""), nil },
	}
	sample, err := daemon.Sample(context.Background())
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	fraction, known := sample.Machine.SwapFraction()
	if !known {
		t.Fatal("the machine's swap was not read")
	}
	if fraction < 0.93 || fraction > 0.95 {
		t.Fatalf("swap reads %.3f of the total, want about 0.94", fraction)
	}
}

// A machine whose accounting does not mention swap is not a machine with no swap. One is a reading
// and the other is silence, and only one of them means there is nothing left to fall back on.
func TestAMachineThatSaysNothingAboutSwapIsNotAMachineWithNone(t *testing.T) {
	silent := Daemon{
		Root: machineWith("MemTotal:       8024876 kB\nMemAvailable:   1539300 kB\n"),
		Run:  func(context.Context, string, ...string) ([]byte, error) { return []byte(""), nil },
	}
	sample, err := silent.Sample(context.Background())
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if sample.Machine.SwapTotal.Known() {
		t.Fatalf("a machine that said nothing about swap reads %s", sample.Machine.SwapTotal)
	}

	none := Daemon{
		Root: machineWith("MemTotal:       8024876 kB\nMemAvailable:   1539300 kB\n" +
			"SwapTotal:             0 kB\nSwapFree:              0 kB\n"),
		Run: func(context.Context, string, ...string) ([]byte, error) { return []byte(""), nil },
	}
	sample, err = none.Sample(context.Background())
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if !sample.Machine.SwapTotal.Known() || sample.Machine.SwapTotal.Bytes() != 0 {
		t.Fatalf("a machine with no swap reads %s, want a measured zero", sample.Machine.SwapTotal)
	}
}

// gibibytes is what a fractional count of gibibytes is in bytes. A constant conversion cannot carry
// the fraction, and the daemon prints fractions.
func gibibytes(count float64) int64 {
	return int64(count * float64(int64(1)<<30))
}

func machineWith(meminfo string) fstest.MapFS {
	return fstest.MapFS{"proc/meminfo": &fstest.MapFile{Data: []byte(meminfo)}}
}

// The processor axis exists because memory was not the axis that broke. On 29 August 2026 eight
// sandboxes held 7,488 megabytes of a 7,653 megabyte runtime and 913 per cent of a processor at the
// same moment, and what stopped answering was the daemon.
func TestEveryContainerCountsTowardsTheProcessorsToo(t *testing.T) {
	out := strings.Join([]string{
		"quaycrew-a00d36d6454a3de66d02c6a3\t1.201GiB / 7.653GiB\t420.50%",
		"quaycrew-postgres-1\t34MiB / 7.653GiB\t12.00%",
	}, "\n")
	_, busy, _ := parseStats([]byte(out))
	if !busy.Known() {
		t.Fatal("the processor total is unknown and every line was readable")
	}
	if busy.Percent() != 432.5 {
		t.Errorf("the containers hold %s of the processors, want 432.5%%", busy)
	}
}

// A figure the daemon would not give up leaves that axis unknown and takes nothing else down with
// it: a reading missing one container is a reserve that is too small on the machine that needs it
// most, and a memory total that was fine is still worth having.
func TestAProcessorFigureTheDaemonWithheldLeavesThatAxisUnknown(t *testing.T) {
	out := "quaycrew-a00d36d6454a3de66d02c6a3\t1.201GiB / 7.653GiB\t--"
	used, busy, _ := parseStats([]byte(out))
	if busy.Known() {
		t.Errorf("the processor total reads %s, and nothing measured it", busy)
	}
	if !used.Known() {
		t.Error("the memory total went unknown with the processor figure, and it was readable")
	}
}
