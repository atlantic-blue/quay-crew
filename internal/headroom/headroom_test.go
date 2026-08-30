package headroom_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/headroom"
)

const mib = int64(1 << 20)

// Rule five of issue 405: no number the system cannot measure. A figure nothing read says the word,
// and it must never read as a zero, because zero megabytes held is a machine that is empty and an
// operator acts differently on the two.
func TestAFigureNothingMeasuredSaysUnknownRatherThanZero(t *testing.T) {
	unknown := headroom.Unknown()
	if unknown.Known() {
		t.Fatal("a figure nothing measured says it was measured")
	}
	if unknown.Bytes() != 0 {
		t.Fatalf("an unmeasured figure carries %d bytes", unknown.Bytes())
	}
	if got := unknown.String(); got != "unknown" {
		t.Fatalf("an unmeasured figure says %q, want unknown", got)
	}
	if got := headroom.Measured(0).String(); got != "0 MiB" {
		t.Fatalf("a measured zero says %q, and zero held is a reading", got)
	}
}

func TestAMeasuredFigureIsStatedInMebibytes(t *testing.T) {
	if got := headroom.Measured(1201 * mib).String(); got != "1201 MiB" {
		t.Fatalf("the figure says %q", got)
	}
}

func TestAnUnmeasuredShareSaysUnknown(t *testing.T) {
	if got := headroom.UnknownShare().String(); got != "unknown" {
		t.Fatalf("an unmeasured share says %q, want unknown", got)
	}
	if got := headroom.MeasuredShare(12.42).String(); got != "12.4%" {
		t.Fatalf("a measured share says %q", got)
	}
}

// The three words, taken as fractions of the limit that binds. The fractions are provisional and
// `docs/OBSERVABILITY.md` says so; what this pins is that they are read off the binding limit and
// never off a byte count somebody chose.
func TestTheStateIsReadOffTheBindingLimit(t *testing.T) {
	tests := []struct {
		name  string
		used  int64
		limit int64
		want  string
	}{
		{"an empty machine has room", 0, 1000 * mib, headroom.StateRoom},
		{"just under three quarters is still room", 749 * mib, 1000 * mib, headroom.StateRoom},
		{"three quarters is tight", 750 * mib, 1000 * mib, headroom.StateTight},
		{"just under nine tenths is still tight", 899 * mib, 1000 * mib, headroom.StateTight},
		{"nine tenths is full", 900 * mib, 1000 * mib, headroom.StateFull},
		{"over the limit is full", 1200 * mib, 1000 * mib, headroom.StateFull},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sample := headroom.Sample{Used: headroom.Measured(test.used), Limit: headroom.Measured(test.limit)}
			if got := sample.State(); got != test.want {
				t.Fatalf("%d of %d reads %q, want %q", test.used, test.limit, got, test.want)
			}
		})
	}
}

// A machine with 36 gigabytes and a 7.8 gigabyte cap on its Docker virtual machine is full at 7.8.
// Rule two: the limit that binds is the daemon's, never the machine's own memory.
func TestTheLimitThatBindsIsTheDaemonsAndNotTheMachines(t *testing.T) {
	// 7200 of 7837 is over nine tenths and full. The same 7200 against the machine's own 36 gigabytes
	// would be under a fifth, which is room, so this fails if the wrong limit is used.
	sample := headroom.Sample{
		Used:  headroom.Measured(7200 * mib),
		Limit: headroom.Measured(7837 * mib),
		Machine: headroom.Machine{
			Total:     headroom.Measured(36864 * mib),
			Available: headroom.Measured(29000 * mib),
		},
	}
	if got := sample.State(); got != headroom.StateFull {
		t.Fatalf("a daemon at 7200 of 7837 reads %q, and the machine's own 36 gigabytes is not the limit", got)
	}
}

// Rule five again, at the sample. A system that could not read the machine must not report room: the
// header that drew a healthy system through eighteen kills is the fault this closes.
func TestASampleNothingCouldReadIsUnknownAndNeverRoom(t *testing.T) {
	tests := []struct {
		name   string
		sample headroom.Sample
	}{
		{"nothing at all", headroom.Sample{Used: headroom.Unknown(), Limit: headroom.Unknown()}},
		{"a limit and no usage", headroom.Sample{Used: headroom.Unknown(), Limit: headroom.Measured(100 * mib)}},
		{"usage and no limit", headroom.Sample{Used: headroom.Measured(10 * mib), Limit: headroom.Unknown()}},
		{"a limit of zero", headroom.Sample{Used: headroom.Measured(10 * mib), Limit: headroom.Measured(0)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.sample.State(); got != headroom.StateUnknown {
				t.Fatalf("the state reads %q, want unknown", got)
			}
			// Free is unknown wherever either side is. A limit of zero is a reading, so free is a
			// reading too, and it is nothing rather than unknown.
			if !test.sample.Used.Known() || !test.sample.Limit.Known() {
				if got := test.sample.Free().String(); got != "unknown" {
					t.Fatalf("free reads %q on a sample that could not be read", got)
				}
			}
		})
	}
}

func TestFreeIsWhatTheDaemonMayStillHandOut(t *testing.T) {
	sample := headroom.Sample{Used: headroom.Measured(3628 * mib), Limit: headroom.Measured(7837 * mib)}
	if got := sample.Free().String(); got != "4209 MiB" {
		t.Fatalf("free reads %q", got)
	}
}

// A daemon holding more than its own cap says free is nothing, and never a negative figure. A
// negative number here would read as room.
func TestADaemonOverItsCapHasNoRoomRatherThanNegativeRoom(t *testing.T) {
	sample := headroom.Sample{Used: headroom.Measured(9000 * mib), Limit: headroom.Measured(7837 * mib)}
	if got := sample.Free().String(); got != "0 MiB" {
		t.Fatalf("free reads %q, want no room at all", got)
	}
}

// Rule three: the machine underneath the daemon is a different question. The kill on 27 August 2026
// came from a machine at 94 per cent of its swap while the daemon held less than half its cap.
func TestTheMachinesSwapIsReportedApartFromTheDaemonsCap(t *testing.T) {
	sample := headroom.Sample{
		Used:  headroom.Measured(3628 * mib),
		Limit: headroom.Measured(7837 * mib),
		Machine: headroom.Machine{
			SwapTotal: headroom.Measured(17408 * mib),
			SwapUsed:  headroom.Measured(16402 * mib),
		},
	}
	if got := sample.State(); got != headroom.StateRoom {
		t.Fatalf("the daemon at 3628 of 7837 reads %q, and that is the figure this word is about", got)
	}
	fraction, known := sample.Machine.SwapFraction()
	if !known {
		t.Fatal("the machine's swap is not reported, so the pressure that killed eighteen sandboxes is invisible")
	}
	if fraction < 0.93 || fraction > 0.95 {
		t.Fatalf("the swap fraction reads %.3f, want about 0.94", fraction)
	}
}

func TestSwapWithNothingBehindItIsNotAFraction(t *testing.T) {
	tests := []struct {
		name    string
		machine headroom.Machine
	}{
		{"nothing read", headroom.Machine{SwapTotal: headroom.Unknown(), SwapUsed: headroom.Unknown()}},
		{"no swap at all", headroom.Machine{SwapTotal: headroom.Measured(0), SwapUsed: headroom.Measured(0)}},
		{"a total and no usage", headroom.Machine{SwapTotal: headroom.Measured(mib), SwapUsed: headroom.Unknown()}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, known := test.machine.SwapFraction(); known {
				t.Fatal("a fraction came back from figures that cannot make one")
			}
		})
	}
}

// The header's whole contribution: one figure and one word.
func TestTheHeaderLineCarriesTheFigureAndTheWord(t *testing.T) {
	sample := headroom.Sample{Used: headroom.Measured(7500 * mib), Limit: headroom.Measured(7837 * mib)}
	line := sample.Line()
	for _, want := range []string{"7500 MiB", "7837 MiB", headroom.StateFull} {
		if !strings.Contains(line, want) {
			t.Fatalf("the header line %q does not carry %q", line, want)
		}
	}
}

// The view answers which session to stop, so the largest is first. A sandbox nothing measured sorts
// last: an unknown figure is not a large one, and putting it first would send the operator to stop
// the one container the system knows nothing about.
func TestSandboxesAreOrderedLargestFirstAndUnknownLast(t *testing.T) {
	ordered := headroom.Sorted([]headroom.Sandbox{
		{Session: "small", Held: headroom.Measured(2 * mib)},
		{Session: "unread", Held: headroom.Unknown()},
		{Session: "large", Held: headroom.Measured(1201 * mib)},
		{Session: "middle", Held: headroom.Measured(400 * mib)},
	})
	want := []string{"large", "middle", "small", "unread"}
	for index, session := range want {
		if ordered[index].Session != session {
			t.Fatalf("the order is %v, want %v", sessions(ordered), want)
		}
	}
}

func TestSortingLeavesTheCallersOwnListAlone(t *testing.T) {
	original := []headroom.Sandbox{
		{Session: "small", Held: headroom.Measured(2 * mib)},
		{Session: "large", Held: headroom.Measured(1201 * mib)},
	}
	headroom.Sorted(original)
	if original[0].Session != "small" {
		t.Fatal("sorting reordered the caller's own list")
	}
}

func sessions(boxes []headroom.Sandbox) []string {
	names := make([]string, 0, len(boxes))
	for _, box := range boxes {
		names = append(names, box.Session)
	}
	return names
}

// A system that has never read its machine says so, rather than describing a machine of zeros.
func TestASampleNobodyTookSaysTheSystemHasNotRead(t *testing.T) {
	said := headroom.Describe(headroom.Sample{Used: headroom.Unknown(), Limit: headroom.Unknown()})
	if !strings.Contains(said, "has not read the machine yet") {
		t.Fatalf("the system says:\n%s", said)
	}
}
