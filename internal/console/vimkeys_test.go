package console

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The console is shaped like k9s and k9s is shaped like vim. Two thirds of the keys already agreed
// with vim and the missing third sat on the keys a vim user presses hardest, so half the keyboard
// rewarded the reflex and half of it punished it, and none of it stuck. These cases are the third
// that moved, and the moves are only half of it: what an old key does now is the other half.

// listed is a view of numbered rows, for asserting where a move lands.
func listed(t *testing.T, count int) Model {
	t.Helper()
	model := newTestModel(t, staticResource("sessions"))
	model.height = 14
	rows := make([]Row, 0, count)
	for at := 0; at < count; at++ {
		rows = append(rows, Row{ID: rowName(at), Cells: []string{rowName(at), rowName(at)}})
	}
	model, _ = update(t, model, rowsFor(model, rows...))
	return model
}

// rowName is a row identifier that keeps the rows in the order they were made, so a jump can be
// asserted by number. It is not called name, because this package imports a package by that name.
func rowName(at int) string {
	return string(rune('a'+at/10)) + string(rune('0'+at%10))
}

func press(t *testing.T, model Model, keys ...string) Model {
	t.Helper()
	for _, key := range keys {
		var msg tea.KeyMsg
		switch key {
		case "ctrl+d", "ctrl+u", "ctrl+f", "ctrl+b":
			msg = tea.KeyMsg{Type: map[string]tea.KeyType{
				"ctrl+d": tea.KeyCtrlD, "ctrl+u": tea.KeyCtrlU,
				"ctrl+f": tea.KeyCtrlF, "ctrl+b": tea.KeyCtrlB,
			}[key]}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		default:
			msg = runes(key)
		}
		model, _ = update(t, model, msg)
	}
	return model
}

// ---------- the keys that moved ----------

// TestAMovedKeySaysWhatToPressNow is the way off each one. A key that quietly does nothing is worse
// than one that changed shape: this repository has already had that regression once, and the tests
// for the new spelling all pass while the old one dead ends.
func TestAMovedKeySaysWhatToPressNow(t *testing.T) {
	for _, moved := range []struct {
		key   string
		names []string
	}{
		{"l", []string{"history", "t"}},
		{"h", []string{"history", "t"}},
		{"D", []string{"mode", "m"}},
	} {
		t.Run(moved.key, func(t *testing.T) {
			client := &fakeClient{}
			model := press(t, sessionsAt(t, client), moved.key)

			if model.mode != modeBrowse {
				t.Fatalf("%s left the console in %v, want it browsing", moved.key, model.mode)
			}
			if model.active.Name != "sessions" {
				t.Fatalf("%s moved the console to %s", moved.key, model.active.Name)
			}
			if model.err == nil {
				t.Fatalf("%s did nothing and said nothing", moved.key)
			}
			for _, want := range moved.names {
				if !strings.Contains(model.err.Error(), want) {
					t.Fatalf("%s says %q, want it to name %q", moved.key, model.err, want)
				}
			}
		})
	}
}

// The archived view carries the same history key, so it carries the same way off it.
func TestTheArchivedViewAlsoSaysWhereItsHistoryKeyWent(t *testing.T) {
	for _, key := range []string{"l", "h"} {
		t.Run(key, func(t *testing.T) {
			model := press(t, newTestModel(t, Archived(&fakeClient{})), key)
			if model.err == nil || !strings.Contains(model.err.Error(), "t") {
				t.Fatalf("%s on the archived view says %v, want it to name t", key, model.err)
			}
		})
	}
}

// A refusal that needs a row under the cursor is no refusal at all: an empty listing is exactly where
// somebody presses the old key and concludes the console is broken.
func TestAMovedKeySaysSoWithNothingListed(t *testing.T) {
	model := press(t, newTestModel(t, Sessions(&fakeClient{})), "l")
	if model.err == nil || !strings.Contains(model.err.Error(), "t") {
		t.Fatalf("l on an empty listing says %v, want it to name t", model.err)
	}
}

// TestGNoLongerRefreshes. It refreshed for as long as the console has had a refresh, and holding it
// there is what kept gg and G off the console entirely.
func TestGNoLongerRefreshes(t *testing.T) {
	client := &fakeClient{}
	model, cmd := update(t, sessionsAt(t, client), runes("g"))
	if cmd != nil {
		t.Fatalf("g asked for %#v, want it holding the first half of gg", cmd())
	}

	// The second key is what says so, because the first is a sequence waiting for its other half.
	after, _ := update(t, model, runes("j"))
	if after.err == nil {
		t.Fatal("g then a key that is not g said nothing about where refreshing went")
	}
	if !strings.Contains(after.err.Error(), "r") {
		t.Fatalf("g says %q, want it to name r", after.err)
	}
}

// The refusal must not eat the key it came with. An operator pressing g and then r is asking for a
// refresh, and answering with a message and no refresh is the same dead end in a different place.
func TestTheKeyAfterAStrandedGStillDoesItsJob(t *testing.T) {
	model, _ := update(t, sessionsAt(t, &fakeClient{}), runes("g"))
	after, cmd := update(t, model, runes("r"))
	if cmd == nil {
		t.Fatal("r after a stranded g produced no listing")
	}
	if _, isRows := cmd().(rowsMsg); !isRows {
		t.Fatalf("r after a stranded g returned %#v, want a listing", cmd())
	}
	if after.err == nil {
		t.Fatal("the stranded g said nothing about where refreshing went")
	}
}

// TestNNoLongerMakesSomething and TestBigNNoLongerStartsAConversation are the two halves of the same
// move: n and N are next and previous match now, and each says where its old job went.
func TestNNoLongerMakesSomething(t *testing.T) {
	client := &wizardClient{}
	model := press(t, newTestModel(t, Sessions(client)).WithClient(client), "n")

	if model.mode == modeWizard {
		t.Fatal("n opened the wizard, and making one thing is on o now")
	}
	if model.err == nil || !strings.Contains(model.err.Error(), "o makes something") {
		t.Fatalf("n says %v, want it to name o", model.err)
	}
}

func TestBigNNoLongerStartsAConversation(t *testing.T) {
	t.Setenv("TMUX_PANE", "%3")
	started := 0
	model := newTestModel(t, Sessions(&fakeClient{})).
		Beside(func(string) ([]string, error) { started++; return []string{"quay", "attach", "s1"}, nil }).
		Freshen(func(string) error { return nil })

	next, cmd := update(t, model, runes("N"))
	if cmd != nil {
		t.Fatalf("N asked for %#v, want it jumping between matches now", cmd())
	}
	if started != 0 {
		t.Fatal("N started a conversation, and a fresh one is on P now")
	}
	if next.err == nil || !strings.Contains(next.err.Error(), "P starts a fresh conversation") {
		t.Fatalf("N says %v, want it to name P", next.err)
	}
}

// TestOMakesOneThing: the wizard's new key, and the other half of the move off n.
func TestOMakesOneThing(t *testing.T) {
	client := &wizardClient{}
	model := press(t, newTestModel(t, Sessions(client)).WithClient(client), "o")
	if model.mode != modeWizard {
		t.Fatalf("o left the console in %v, want the wizard open", model.mode)
	}
}

// ---------- the keys vim has that the console did not ----------

func TestGGGoesToTheFirstRowAndBigGToTheLast(t *testing.T) {
	model := press(t, listed(t, 20), "j", "j", "j")
	if model.selected != 3 {
		t.Fatalf("three presses of j landed on row %d, want 3", model.selected)
	}
	model = press(t, model, "G")
	if model.selected != 19 {
		t.Fatalf("G landed on row %d, want the last of 20", model.selected)
	}
	model = press(t, model, "g", "g")
	if model.selected != 0 {
		t.Fatalf("gg landed on row %d, want the first", model.selected)
	}
}

func TestHalfAPageIsCtrlDAndCtrlU(t *testing.T) {
	model := listed(t, 60)
	half := halfOf(model.bodyHeight())
	if half < 2 {
		t.Fatalf("the test window is too short to tell half a page from one row: body = %d", model.bodyHeight())
	}

	model = press(t, model, "ctrl+d")
	if model.selected != half {
		t.Fatalf("ctrl+d landed on row %d, want %d", model.selected, half)
	}
	model = press(t, model, "ctrl+d", "ctrl+u")
	if model.selected != 2*half-half {
		t.Fatalf("ctrl+d twice and ctrl+u once landed on row %d, want %d", model.selected, half)
	}
}

// A count in front of a move is the largest piece of this and the one an operator notices least until
// it is there: 5j is five rows, and 5G is the fifth row.
func TestACountRepeatsAMove(t *testing.T) {
	model := press(t, listed(t, 40), "5", "j")
	if model.selected != 5 {
		t.Fatalf("5j landed on row %d, want 5", model.selected)
	}
	model = press(t, model, "2", "k")
	if model.selected != 3 {
		t.Fatalf("2k landed on row %d, want 3", model.selected)
	}
}

func TestACountLandsOnARowByNumber(t *testing.T) {
	model := press(t, listed(t, 40), "1", "2", "G")
	if model.selected != 11 {
		t.Fatalf("12G landed on row %d, want the twelfth, counting from one", model.selected)
	}
	model = press(t, model, "3", "g", "g")
	if model.selected != 2 {
		t.Fatalf("3gg landed on row %d, want the third", model.selected)
	}
}

// A count that is typed and then abandoned must not attach itself to the next move, or a j pressed a
// minute later jumps twelve rows.
func TestAnAbandonedCountDoesNotAttachToTheNextMove(t *testing.T) {
	model := press(t, listed(t, 40), "1", "2", "esc", "j")
	if model.selected != 1 {
		t.Fatalf("j after an abandoned count landed on row %d, want 1", model.selected)
	}
}

// A console holding half a sequence and drawing nothing looks exactly like one that dropped the key.
func TestWhatIsHeldWaitingForTheRestOfASequenceIsOnScreen(t *testing.T) {
	if drawn := press(t, listed(t, 40), "g").View(); !strings.Contains(drawn, "g") {
		t.Fatalf("a held g is nowhere on the screen:\n%s", drawn)
	}
	if drawn := press(t, listed(t, 40), "1", "2").View(); !strings.Contains(drawn, "12") {
		t.Fatalf("a half typed count is nowhere on the screen:\n%s", drawn)
	}
}

// ---------- next and previous match ----------

// filterable is three rows where the middle one does not match, so a jump has somewhere to jump over.
func filterable(t *testing.T) Model {
	t.Helper()
	model := newTestModel(t, staticResource("sessions"))
	model, _ = update(t, model, rowsFor(model,
		Row{ID: "s1", Cells: []string{"s1", "bills"}},
		Row{ID: "s2", Cells: []string{"s2", "gardening"}},
		Row{ID: "s3", Cells: []string{"s3", "bills"}}))
	return model
}

// The console filters rather than searching, so what n jumps through is what the filter last matched.
// Escape puts every row back on screen and keeps the word, which is the moment jumping is worth
// anything: until now escape threw the word away and there was nothing to jump between.
func TestNextAndPreviousMatchJumpThroughWhatWasFilteredFor(t *testing.T) {
	model := press(t, filterable(t), "/", "b", "i", "l", "l", "s")
	model = press(t, model, "esc")

	if len(model.visibleRows()) != 3 {
		t.Fatalf("escape left %d rows on screen, want all 3 back", len(model.visibleRows()))
	}
	model = press(t, model, "n")
	if model.selected != 2 {
		t.Fatalf("n landed on row %d, want it over the row that does not match", model.selected)
	}
	model = press(t, model, "N")
	if model.selected != 0 {
		t.Fatalf("N landed on row %d, want it back at the first match", model.selected)
	}
}

// Wrapping is what vim does at the end of a file, and a console that stopped instead would leave the
// last match with nowhere to go.
func TestTheMatchKeysWrapAtTheEnds(t *testing.T) {
	model := press(t, filterable(t), "/", "b", "i", "l", "l", "s", "esc", "n")
	model = press(t, model, "n")
	if model.selected != 0 {
		t.Fatalf("n from the last match landed on row %d, want it wrapped to the first", model.selected)
	}
}

// A jump with nowhere left to land says so. The first n here lands on the one row that matches,
// because escape leaves the cursor on a row that does not; the second is the one with nothing to do,
// and a key that quietly does nothing is what this whole change is about.
func TestAMatchKeySaysWhenNothingElseMatches(t *testing.T) {
	model := press(t, filterable(t), "/", "g", "a", "r", "d", "e", "n", "esc", "n")
	if model.selected != 1 {
		t.Fatalf("n landed on row %d, want the one row that matches", model.selected)
	}

	model = press(t, model, "n")
	if model.err == nil || !strings.Contains(model.err.Error(), "garden") {
		t.Fatalf("n with one match says %v, want it to say nothing else matches", model.err)
	}
	if model.selected != 1 {
		t.Fatalf("n moved to row %d with nothing else matching, want it to stay put", model.selected)
	}
}

// ---------- what already agreed with vim, and did not move ----------

// The two thirds that were already right. A change that fixed the missing third by moving these
// would have cost more than it fixed.
func TestTheKeysThatAlreadyAgreedWithVimStayed(t *testing.T) {
	model := listed(t, 40)

	if press(t, model, "j").selected != 1 {
		t.Fatal("j no longer moves down")
	}
	if press(t, press(t, model, "j"), "k").selected != 0 {
		t.Fatal("k no longer moves up")
	}
	if press(t, model, "ctrl+f").selected != model.bodyHeight() {
		t.Fatal("ctrl+f no longer pages down")
	}
	if press(t, model, "/").mode != modeFilter {
		t.Fatal("/ no longer filters")
	}
	if press(t, model, ":").mode != modeCommand {
		t.Fatal(": no longer opens the command bar")
	}
	if !press(t, model, "q").quitting {
		t.Fatal("q no longer quits")
	}
}

// Restore is undo, and it is on undo's key. It is the binding the rest of this change was written to
// match, so it is also the one a sweep would most easily take with it.
func TestRestoreIsStillOnU(t *testing.T) {
	client := &fakeClient{}
	model := newTestModel(t, Archived(client))
	model, _ = update(t, model, rowsFor(model, Row{ID: "s1", Cells: []string{"s1", "acme"}}))

	_, cmd := update(t, model, runes("u"))
	if cmd == nil {
		t.Fatal("u produced no command, want the session restored")
	}
	cmd()
	if len(client.restored) != 1 || client.restored[0] != "s1" {
		t.Fatalf("restored = %v, want [s1]", client.restored)
	}
}

// The history is reachable, on the key that names the view it opens. Testing the way off a key and
// never the way on is how a move quietly lands nowhere.
func TestTheHistoryIsOnT(t *testing.T) {
	for _, view := range []struct {
		name  string
		build func() Resource
	}{
		{"sessions", func() Resource { return Sessions(&fakeClient{}) }},
		{"archived", func() Resource { return Archived(&fakeClient{}) }},
	} {
		t.Run(view.name, func(t *testing.T) {
			registry, err := NewRegistry(view.build(), staticResource("tasks"))
			if err != nil {
				t.Fatalf("NewRegistry: %v", err)
			}
			model, err := New(registry, view.name, nil)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			model.width, model.height = 120, 20
			model, _ = update(t, model, rowsFor(model, Row{ID: "s1", Cells: []string{"s1", "acme"}}))

			model = press(t, model, "t")
			if model.active.Name != "tasks" {
				t.Fatalf("t on %s opened %s, want the tasks", view.name, model.active.Name)
			}
		})
	}
}

// The keys view and the help behind the question mark are read by whoever is trying to remember one
// of these. A binding that changes and a help that does not is worse than either.
func TestTheHelpNamesTheKeysAsTheyAreNow(t *testing.T) {
	model := tallTestModel(t, Sessions(&fakeClient{}))
	model.mode = modeHelp
	drawn := model.View()

	for _, want := range []string{"gg G", "ctrl-d ctrl-u", "n N", "Make one thing"} {
		if !strings.Contains(drawn, want) {
			t.Fatalf("the help does not name %q:\n%s", want, drawn)
		}
	}
	if strings.Contains(drawn, "r g") {
		t.Fatalf("the help still says g refreshes:\n%s", drawn)
	}
}

// The keys view lists what every view answers to, and a key that moved must be listed by its new
// spelling and not by its old one.
func TestTheKeysViewListsTheNewSpellings(t *testing.T) {
	registry, err := NewRegistry(Sessions(&fakeClient{}))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	rows, err := Keys(registry).List(context.Background(), "")
	if err != nil {
		t.Fatalf("listing the keys: %v", err)
	}
	history, mode := "", ""
	for _, row := range rows {
		switch row.Cells[2] {
		case "History":
			history = row.Cells[1]
		case "Mode":
			mode = row.Cells[1]
		}
	}
	if history != "t" {
		t.Fatalf("the keys view lists the history on %q, want t", history)
	}
	if mode != "m" {
		t.Fatalf("the keys view lists the mode on %q, want m", mode)
	}
}
