package console

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/capacity"
	"github.com/atlantic-blue/quay-krewe/internal/headroom"
)

const mebibyte = int64(1) << 20

// theIncident is what the machine read on 27 August 2026, from issue 405: the daemon holding 3,628
// mebibytes of a 7,837 mebibyte cap. The room view listed eighteen sandboxes against these figures
// and carried neither of them.
func theIncident() *quaycrewv1.GetHeadroomResponse {
	return answerOf(3628*mebibyte, 7837*mebibyte)
}

// answerOf is what the system answers for a machine holding one figure of another, in the words the
// control plane writes them in.
func answerOf(used, limit int64) *quaycrewv1.GetHeadroomResponse {
	sample := headroom.Sample{Used: headroom.Measured(used), Limit: headroom.Measured(limit)}
	return &quaycrewv1.GetHeadroomResponse{
		Used: sample.Used.String(), Limit: sample.Limit.String(), Free: sample.Free().String(),
		State: sample.State(), UsedBytes: used, LimitBytes: limit,
	}
}

// The fault this closes: eighteen rows of megabytes and no total, no capacity and no headroom
// anywhere on the screen. See issue 457.
func TestTheSummarySaysWhatIsHeldWhatBindsAndWhatIsLeft(t *testing.T) {
	line, _ := roomSummary(theIncident())

	for _, want := range []string{"3628 MiB", "7837 MiB", "4209 MiB"} {
		if !strings.Contains(line, want) {
			t.Errorf("the summary does not carry %q, so the operator still cannot see how close the "+
				"machine is:\n%s", want, line)
		}
	}
}

// What the rows add up to is not what the limit binds: the figure is every container on the daemon,
// including the system's own, so an operator adding eighteen rows in their head gets a smaller number
// and has to be told which one this is.
func TestTheSummarySaysTheFigureIsEveryContainerAndNotJustTheSandboxes(t *testing.T) {
	line, _ := roomSummary(theIncident())

	if !strings.Contains(line, "containers hold") {
		t.Errorf("the summary does not say whose memory 3628 MiB is:\n%s", line)
	}
}

// The margin is stated in the unit an operator acts in. A sandbox asks for a measured 1,536
// mebibytes, so 4,209 left is two more sandboxes and not a number to divide in your head.
func TestTheSummaryPutsWhatIsLeftInSandboxes(t *testing.T) {
	line, _ := roomSummary(theIncident())

	fits := 4209 * mebibyte / capacity.RequestMemory
	if fits != 2 {
		t.Fatalf("the measured request is %s, so 4209 MiB is %d sandboxes and this case is stale",
			capacity.Memory(capacity.RequestMemory), fits)
	}
	if !strings.Contains(line, "2 more sandboxes") {
		t.Errorf("the summary does not say how many more sandboxes fit in what is left:\n%s", line)
	}
}

// The word is the system's own, so the view and the header never carry two different answers about one
// machine.
func TestTheSummaryCarriesTheSystemsWord(t *testing.T) {
	line, state := roomSummary(theIncident())
	if !strings.Contains(line, headroom.StateRoom) {
		t.Errorf("the summary does not say the machine is %q:\n%s", headroom.StateRoom, line)
	}
	if state != StateReady {
		t.Errorf("a machine with room is drawn as %v, want %v", state, StateReady)
	}
}

// Full has to be readable without reading the number beside it, and without colour: a console piped
// to a file has none.
func TestAFullMachineIsReadableWithoutTheNumberOrTheColour(t *testing.T) {
	line, state := roomSummary(answerOf(7200*mebibyte, 7837*mebibyte))

	if !strings.Contains(line, strings.ToUpper(headroom.StateFull)) {
		t.Errorf("a full machine does not say FULL:\n%s", line)
	}
	if state != StateFailed {
		t.Errorf("a full machine is drawn as %v, want %v", state, StateFailed)
	}
	if !strings.Contains(line, "not enough for another sandbox") {
		t.Errorf("a full machine does not say another sandbox will not fit:\n%s", line)
	}
}

// The threshold that turns is measured. A small daemon cap is under three quarters full, which is
// the fraction the header calls room, and still has less left than the 1,536 mebibytes a sandbox was
// measured to ask for. The fraction is a judgement; this one is a measurement, and it is the one an
// operator acts on.
func TestAMarginTooThinForASandboxTurnsEvenWhereTheFractionSaysRoom(t *testing.T) {
	answer := answerOf(2600*mebibyte, 4096*mebibyte)
	if answer.GetState() != headroom.StateRoom {
		t.Fatalf("the system calls this machine %q, and this case is about the one it calls room",
			answer.GetState())
	}

	line, state := roomSummary(answer)

	if state == StateReady {
		t.Errorf("1496 MiB left will not hold a sandbox asking for %s, and the summary reads healthy:\n%s",
			capacity.Memory(capacity.RequestMemory), line)
	}
	if !strings.Contains(line, headroom.StateTight) {
		t.Errorf("a margin too thin for another sandbox does not read tight:\n%s", line)
	}
}

// Nothing here estimates. A system that never read its machine says so and names the reason, rather
// than drawing zeroes that read as a machine holding nothing.
func TestASystemThatReadNothingSaysSoRatherThanDrawingZeroes(t *testing.T) {
	line, state := roomSummary(&quaycrewv1.GetHeadroomResponse{
		Used: "unknown", Limit: "unknown", Free: "unknown", State: headroom.StateUnknown,
		UsedBytes: -1, LimitBytes: -1, Failed: "the daemon is not answering",
	})

	if !strings.Contains(line, headroom.StateUnknown) {
		t.Errorf("a machine nobody read does not read unknown:\n%s", line)
	}
	if !strings.Contains(line, "the daemon is not answering") {
		t.Errorf("the summary does not say why the system knows nothing:\n%s", line)
	}
	if strings.Contains(line, "0 MiB") {
		t.Errorf("the summary draws a zero for a figure nobody measured:\n%s", line)
	}
	if state == StateReady {
		t.Error("a machine nobody read is drawn as healthy, which is the header that drew through eighteen kills")
	}
}

// A system too old to answer at all leaves the view its rows rather than a line about nothing.
func TestAViewWithNothingToSummariseDrawsNoLine(t *testing.T) {
	if line, _ := roomSummary(nil); line != "" {
		t.Errorf("a summary was drawn from no answer at all: %q", line)
	}
}

// ---------- what the operator is left looking at ----------

// summaryResource is a view with a line of its own, for the drawing cases.
func summaryResource(line string, state State) Resource {
	resource := staticResource("room")
	resource.Summary = func(context.Context, string) (string, State) { return line, state }
	return resource
}

// The line has to be above the rows and inside the panel, which is where the eye lands before it
// starts reading megabytes.
func TestTheSummaryIsDrawnAboveTheColumns(t *testing.T) {
	model := newTestModel(t, summaryResource("containers hold 3628 MiB of 7837 MiB", StateReady))
	model.summary = summary{line: "containers hold 3628 MiB of 7837 MiB", state: StateReady}

	lines := strings.Split(model.View(), "\n")
	at, columns := -1, -1
	for index, line := range lines {
		if strings.Contains(line, "3628 MiB") && at < 0 {
			at = index
		}
		if strings.Contains(line, "NAME") && columns < 0 {
			columns = index
		}
	}
	if at < 0 {
		t.Fatalf("the summary is not on the screen:\n%s", model.View())
	}
	if columns < 0 || at > columns {
		t.Errorf("the summary is drawn at line %d and the columns at %d, so it is not above them", at, columns)
	}
}

// The line costs a row, and a row it does not pay for is a row taken off the bottom of the listing:
// the panel would then draw one line more than it has and the footer would be pushed off the screen.
func TestTheSummaryCostsARowOfTheListing(t *testing.T) {
	plain := newTestModel(t, staticResource("room"))
	withLine := newTestModel(t, summaryResource("containers hold 3628 MiB of 7837 MiB", StateReady))
	withLine.summary = summary{line: "containers hold 3628 MiB of 7837 MiB", state: StateReady}

	if got, want := withLine.bodyHeight(), plain.bodyHeight()-1; got != want {
		t.Errorf("the listing has %d rows with a summary and %d without, want %d", got, plain.bodyHeight(), want)
	}
	if len(strings.Split(withLine.View(), "\n")) != len(strings.Split(plain.View(), "\n")) {
		t.Errorf("the panel is a different height with a summary in it, so something else went off the screen:\n%s",
			withLine.View())
	}
}

// A view with no line of its own is untouched: nine of the ten views have nothing to summarise.
func TestAViewWithNoSummaryIsDrawnAsItAlwaysWas(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	if line := model.summaryLine(); line != "" {
		t.Errorf("a view with no summary drew one: %q", line)
	}
}
