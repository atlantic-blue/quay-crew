package console

import (
	"context"
	"errors"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
)

// probed is a reading of one part, the way the system answers one.
func probed(name, state string) *quaycrewv1.HealthComponent {
	return &quaycrewv1.HealthComponent{Name: name, State: state}
}

// statsRows lists the stats view against a system that last found this.
func statsRows(t *testing.T, client *fakeClient) map[string]Row {
	t.Helper()
	rows, err := Stats(client).List(context.Background(), "")
	if err != nil {
		t.Fatalf("listing the stats: %v", err)
	}
	byName := make(map[string]Row, len(rows))
	for _, row := range rows {
		byName[row.Cells[0]] = row
	}
	return byName
}

// TestADeadComponentReadsAsDeadInTheStatsView is the finding this column exists for. A component was
// gone for sixteen hours while this screen was open, and its row was drawn in the colour of the working
// ones, because every row was ready and the view could say nothing else.
func TestADeadComponentReadsAsDeadInTheStatsView(t *testing.T) {
	rows := statsRows(t, &fakeClient{health: []*quaycrewv1.HealthComponent{
		probed(display.HealthStore, display.HealthDown),
	}})

	store := rows["Store engine"]
	if got := store.Cells[1]; got != display.HealthDown {
		t.Fatalf("the store row reads %q, and a store nothing can write to is down", got)
	}
	if store.State != StateFailed {
		t.Fatalf("the store row is in state %v, so it is not drawn as wanting attention", store.State)
	}
	// And the rows beside it are not dragged down with it, or the view says everything is broken.
	if got := rows["Model"].Cells[1]; got != display.HealthNotChecked {
		t.Fatalf("the model row reads %q, and nothing probes it", got)
	}
	if rows["Model"].State != StateUnknown {
		t.Fatalf("the model row is in state %v, want unknown", rows["Model"].State)
	}
}

// TestTheStatsViewNeverCallsAPartHealthyThatNothingProbed. Four of the five rows have no probe behind
// them. Green on those is the same lie in a different row.
func TestTheStatsViewNeverCallsAPartHealthyThatNothingProbed(t *testing.T) {
	rows := statsRows(t, &fakeClient{health: []*quaycrewv1.HealthComponent{
		probed(display.HealthStore, display.HealthServing),
	}})

	for _, unprobed := range []string{"Model", "Sandbox engine", "Secrets", "State"} {
		row := rows[unprobed]
		if got := row.Cells[1]; got != display.HealthNotChecked {
			t.Fatalf("the %s row reads %q, and nothing probes it", unprobed, got)
		}
		if row.State != StateUnknown {
			t.Fatalf("the %s row is in state %v, and no colour is the answer for a part nobody read",
				unprobed, row.State)
		}
	}
}

// TestEveryStatsRowSaysHowItIs, so a row cannot be added later that carries no state at all.
func TestEveryStatsRowSaysHowItIs(t *testing.T) {
	rows := statsRows(t, &fakeClient{health: []*quaycrewv1.HealthComponent{
		probed(display.HealthStore, display.HealthServing),
	}})
	if len(rows) != 5 {
		t.Fatalf("the stats view listed %d rows, want the five it carries", len(rows))
	}
	said := map[string]bool{
		display.HealthServing: true, display.HealthDown: true,
		display.HealthNotConfigured: true, display.HealthNotChecked: true,
	}
	for what, row := range rows {
		if len(row.Cells) != 3 {
			t.Fatalf("the %s row has %d cells, want what, state and running", what, len(row.Cells))
		}
		if !said[row.Cells[1]] {
			t.Fatalf("the %s row says %q, which is not one of the words a state is said in", what, row.Cells[1])
		}
	}
}

// TestTheStatsViewStillDrawsWhenTheSystemWillNotSayHowItIs. An older control plane does not answer the
// health call at all, and the five lines an operator opened this view for are still worth drawing.
func TestTheStatsViewStillDrawsWhenTheSystemWillNotSayHowItIs(t *testing.T) {
	rows := statsRows(t, &fakeClient{healthErr: errors.New("unknown method GetHealth")})
	if len(rows) != 5 {
		t.Fatalf("the stats view listed %d rows when the system would not say how it is", len(rows))
	}
	for what, row := range rows {
		if got := row.Cells[1]; got != display.HealthNotChecked {
			t.Fatalf("the %s row reads %q from a system that answered nothing", what, got)
		}
	}
}

// TestAHealthStateIsWrittenInTheColourOfTheState, because the word and the colour have to agree: the
// row that is down is the one somebody opened this view to find.
func TestAHealthStateIsWrittenInTheColourOfTheState(t *testing.T) {
	for state, want := range map[string]string{
		display.HealthServing:       ansiGreenCode,
		display.HealthDown:          ansiRedCode,
		display.HealthNotConfigured: dimCode,
		display.HealthNotChecked:    dimCode,
	} {
		if got := colourOfHealth(state); got != want {
			t.Fatalf("%q is written in %q, want %q", state, got, want)
		}
	}
}
