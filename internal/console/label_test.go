package console

import (
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
	tea "github.com/charmbracelet/bubbletea"
)

// A listing of fourteen sessions is a column of hexadecimal, and working out which conversation was
// the one about the electricity bill means opening them. These cases are the name that fixes that.

// aSessionNamed is the listing showing one session, named or not.
func aSessionNamed(t *testing.T, client *fakeClient, label string) Model {
	t.Helper()
	model := newTestModel(t, Sessions(client))
	shown := label
	if shown == "" {
		shown = "d754610f"
	}
	model, _ = update(t, model, rowsFor(model,
		Row{ID: "s1", Label: shown, Detail: label,
			Cells: []string{"5d013d07", "acme", "bills", shown, "idle", "edits", "", "", "", "1m"}}))
	return model
}

func TestNamingASessionSendsTheNameToTheCrew(t *testing.T) {
	client := &fakeClient{}
	model, _ := update(t, aSessionNamed(t, client, ""), runes("L"))

	if model.mode != modeType {
		t.Fatalf("L left the console in %v, want it asking for a name", model.mode)
	}
	for _, r := range "the electricity bill" {
		model, _ = update(t, model, runes(string(r)))
	}
	_, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("naming produced no command")
	}
	cmd()
	if len(client.labelsSet) != 1 || client.labelsSet[0] != "the electricity bill" {
		t.Fatalf("labels set = %v, want [the electricity bill]", client.labelsSet)
	}
}

// Clearing has to be enter on an empty line, because escape is how nothing happens. A key that
// refused to clear would leave no way back to the identifier.
func TestAnEmptyNameClearsIt(t *testing.T) {
	client := &fakeClient{}
	model, _ := update(t, aSessionNamed(t, client, "the electricity bill"), runes("L"))

	if model.input != "the electricity bill" {
		t.Fatalf("naming started from %q, want the name it already has", model.input)
	}
	for range len("the electricity bill") {
		model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	_, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	cmd()
	if len(client.labelsSet) != 1 || client.labelsSet[0] != "" {
		t.Fatalf("labels set = %v, want one empty name", client.labelsSet)
	}
}

func TestLeavingTheNameLineChangesNothing(t *testing.T) {
	client := &fakeClient{}
	model, _ := update(t, aSessionNamed(t, client, ""), runes("L"))
	model, _ = update(t, model, runes("x"))

	after, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEsc})

	if after.mode != modeBrowse {
		t.Fatalf("escape left the console in %v", after.mode)
	}
	if cmd != nil {
		cmd()
		t.Fatalf("escape produced a command, and it ran: labels set = %v", client.labelsSet)
	}
	if len(client.labelsSet) != 0 {
		t.Fatalf("labels set = %v, want nothing", client.labelsSet)
	}
}

// The whole point: the listing reads as conversations rather than as identifiers.
func TestTheListingShowsTheNameAndFallsBackToTheIdentifier(t *testing.T) {
	named := &quaycrewv1.Session{Id: "5d013d07aa", Handle: "d754610faa", Label: "the electricity bill"}
	described := &quaycrewv1.Session{Id: "6e124e18bb", Handle: "e865721abb", Description: "fixing the payout job"}
	bare := &quaycrewv1.Session{Id: "7f235f29cc", Handle: "f976832bcc"}

	if got := display.SessionName(named); got != "the electricity bill" {
		t.Errorf("a named session is listed as %q", got)
	}
	// A name somebody picked beats a name a machine wrote, so a session with both shows the label.
	both := &quaycrewv1.Session{Handle: "aa11bb22", Label: "the electricity bill", Description: "fixing the payout job"}
	if got := display.SessionName(both); got != "the electricity bill" {
		t.Errorf("a session with both is listed as %q, want the operator's name", got)
	}
	if got := display.SessionName(described); got != "fixing the payout job" {
		t.Errorf("a described session is listed as %q", got)
	}
	// The id, which is the value the session column carries, so falling back lands on something the
	// operator can type. It used to fall back to the handle, which no column names.
	if got := display.SessionName(bare); got != "7f235f29" {
		t.Errorf("a session with no name at all is listed as %q, want the id the session column prints", got)
	}
	// The name cell itself does not fall back at all: it is empty until somebody names the session.
	if got := display.SessionLabel(bare); got != "" {
		t.Errorf("the name cell of an unnamed session says %q, want nothing", got)
	}
}
