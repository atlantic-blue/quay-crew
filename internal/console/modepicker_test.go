package console

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The console could reach two of the three modes, by flipping between them, so planning was set
// deliberately everywhere except the surface an operator lives in. These cases are the picker that
// replaced the flip.

// sessionIn is the listing showing one session in a mode, which is what the picker reads to know
// whether a pick widens what the session may do or narrows it.
func sessionIn(t *testing.T, client *fakeClient, mode string) Model {
	t.Helper()
	model := newTestModel(t, Sessions(client))
	model, _ = update(t, model, rowsFor(model,
		Row{ID: "s1", Label: "d754610f", Cells: []string{"5d013d07", "acme", "bills", "d754610f", "idle", mode, "1m"}}))
	return model
}

// pick opens the picker and moves the cursor onto a mode by name, so a case reads as the operator
// choosing rather than as a number of key presses.
func pick(t *testing.T, model Model, want string) Model {
	t.Helper()
	model, _ = update(t, model, runes("m"))
	if model.mode != modeChoose {
		t.Fatalf("m did not open the picker, mode = %v", model.mode)
	}
	for range len(offeredModes()) {
		if offeredModes()[model.choosing.at] == want {
			return model
		}
		model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyDown})
	}
	t.Fatalf("the picker never offers %q, it offers %v", want, offeredModes())
	return model
}

func TestThePickerOffersAllThreeModesNarrowestFirst(t *testing.T) {
	model, _ := update(t, sessionIn(t, &fakeClient{}, "edits"), runes("m"))

	view := model.View()
	for _, want := range []string{"plan", "edits", "dangerous"} {
		if !strings.Contains(view, want) {
			t.Errorf("the picker does not offer %q:\n%s", want, view)
		}
	}
	if first := offeredModes()[0]; first != "plan" {
		t.Fatalf("the picker opens on %q, want plan: the most permissive must never be under the cursor", first)
	}
}

// Planning was the mode the console could not reach at all, which is the whole reason this exists.
func TestASessionCanBePutIntoPlanning(t *testing.T) {
	client := &fakeClient{}

	model := pick(t, sessionIn(t, client, "dangerous"), "plan")
	_, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("choosing plan produced no command")
	}
	cmd()
	if len(client.modesSet) != 1 || client.modesSet[0] != "plan" {
		t.Fatalf("modes set = %v, want [plan]", client.modesSet)
	}
}

// Narrowing takes away what a session may do, so there is nothing to be careful about and asking
// would only be in the way.
func TestNarrowingWhatASessionMayDoDoesNotAsk(t *testing.T) {
	for _, tc := range []struct{ from, to string }{
		{from: "dangerous", to: "edits"},
		{from: "dangerous", to: "plan"},
		{from: "edits", to: "plan"},
	} {
		t.Run(tc.from+" to "+tc.to, func(t *testing.T) {
			client := &fakeClient{}

			model := pick(t, sessionIn(t, client, tc.from), tc.to)
			after, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

			if after.mode == modeConfirm {
				t.Fatalf("going from %s to %s asked first, and it takes capability away", tc.from, tc.to)
			}
			if cmd == nil {
				t.Fatalf("going from %s to %s produced no command", tc.from, tc.to)
			}
			cmd()
			if len(client.modesSet) != 1 || client.modesSet[0] != wantedMode(tc.to) {
				t.Fatalf("modes set = %v, want [%s]", client.modesSet, wantedMode(tc.to))
			}
		})
	}
}

// Widening asks, like every other key that gives a session more room, and it names the session so a
// yes is a yes to something in particular.
func TestWideningWhatASessionMayDoAsksFirst(t *testing.T) {
	for _, tc := range []struct{ from, to string }{
		{from: "plan", to: "edits"},
		{from: "plan", to: "dangerous"},
		{from: "edits", to: "dangerous"},
	} {
		t.Run(tc.from+" to "+tc.to, func(t *testing.T) {
			client := &fakeClient{}

			model := pick(t, sessionIn(t, client, tc.from), tc.to)
			asked, _ := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

			if asked.mode != modeConfirm {
				t.Fatalf("going from %s to %s did not ask, and it gives the session more room", tc.from, tc.to)
			}
			if len(client.modesSet) != 0 {
				t.Fatalf("modes set = %v, want nothing changed before the yes", client.modesSet)
			}
			if view := asked.View(); !strings.Contains(view, "d754610f") {
				t.Fatalf("the question does not name the session:\n%s", view)
			}

			_, cmd := update(t, asked, runes("y"))
			if cmd == nil {
				t.Fatal("yes produced no command")
			}
			cmd()
			if len(client.modesSet) != 1 || client.modesSet[0] != wantedMode(tc.to) {
				t.Fatalf("modes set = %v, want [%s]", client.modesSet, wantedMode(tc.to))
			}
		})
	}
}

// Escape leaves the session as it was. A picker that acted on the way out would be a picker nobody
// could open to see what the modes are.
func TestLeavingThePickerChangesNothing(t *testing.T) {
	client := &fakeClient{}

	model := pick(t, sessionIn(t, client, "edits"), "dangerous")
	after, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEsc})

	if after.mode != modeBrowse {
		t.Fatalf("escape left the console in %v, want it back in the listing", after.mode)
	}
	// The command itself, not only its effect. Reading modesSet after throwing the command away
	// passes against a picker that acts on the way out, because nothing ever ran it.
	if cmd != nil {
		cmd()
		t.Fatalf("escape produced a command, and it ran: modes set = %v", client.modesSet)
	}
	if len(client.modesSet) != 0 {
		t.Fatalf("modes set = %v, want nothing changed", client.modesSet)
	}
	// And the next key does what it always did, rather than being swallowed by a picker still holding
	// the keyboard.
	listing, _ := update(t, after, runes("/"))
	if listing.mode != modeFilter {
		t.Fatalf("after leaving the picker, / opened %v rather than the filter", listing.mode)
	}
}

// The way off the old key. D opened the picker, and before that it flipped between two modes, so it
// is in somebody's fingers. In vim D deletes to the end of the line, and a destructive shaped key on
// an action that takes nothing away teaches the operator that the shapes mean nothing, so it is gone
// and says where the picker went.
func TestTheOldDangerousKeySaysWhereTheModePickerWent(t *testing.T) {
	client := &fakeClient{}

	model, _ := update(t, sessionIn(t, client, "edits"), runes("D"))

	if model.mode != modeBrowse {
		t.Fatalf("D left the console in %v, want it browsing", model.mode)
	}
	if model.err == nil {
		t.Fatal("D did nothing and said nothing, which is the key that quietly stopped working")
	}
	if !strings.Contains(model.err.Error(), "m") || !strings.Contains(model.err.Error(), "mode") {
		t.Fatalf("D says %q, want it to name the mode picker and the key it is on", model.err)
	}
	if len(client.modesSet) != 0 {
		t.Fatalf("D armed the session on its own: modes set = %v", client.modesSet)
	}
}

// wantedMode is the protocol's spelling of a mode the listing prints, for asserting on what the system
// was actually told.
func wantedMode(spoken string) string {
	switch spoken {
	case "plan":
		return "plan"
	case "edits":
		return "acceptEdits"
	default:
		return "bypassPermissions"
	}
}
