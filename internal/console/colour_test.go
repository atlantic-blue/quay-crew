package console

import (
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
)

// A listing of thirty sessions was one colour, so reading it meant reading every character of every
// row. These cases are the colouring that replaced that: the same rules the sessions tool uses, which
// is the listing the operator already reads all day.

// TestAWorkspaceKeepsItsColourAndTwoWorkspacesDiffer is the whole point of colouring a name rather
// than a column: the eye finds the rows belonging to one place without reading any of them.
func TestAWorkspaceKeepsItsColourAndTwoWorkspacesDiffer(t *testing.T) {
	first, again := colourOfName("atlantic-blue"), colourOfName("atlantic-blue")
	if first != again {
		t.Fatalf("the same name took two colours, %q then %q, so a listing would shimmer on every refresh", first, again)
	}

	// Not a guarantee for every possible pair, since a hash into a palette collides eventually. These
	// are the names actually on this system, and they have to be told apart.
	seen := map[string]string{}
	for _, name := range []string{"atlantic-blue", "itv", "fanalysis", "juliantellez"} {
		colour := colourOfName(name)
		if was, taken := seen[colour]; taken {
			t.Errorf("%q and %q share a colour, and both are workspaces on this system", was, name)
		}
		seen[colour] = name
	}
}

// The mode is the cell it costs most to misread: a session that skips every permission must not look
// like one that asks.
func TestTheModeIsColouredByWhatItLetsASessionDo(t *testing.T) {
	plan, edits, dangerous := colourOfMode("plan"), colourOfMode("edits"), colourOfMode("dangerous")

	if plan == dangerous {
		t.Fatal("planning and dangerous are the same colour, and they are opposite ends of what a session may do")
	}
	if edits == dangerous {
		t.Fatal("edits and dangerous are the same colour")
	}
}

// A number nobody acts on should not compete with one they do.
func TestALargeTokenCountStandsOutAndASmallOneDoesNot(t *testing.T) {
	if colourOfTokens("1.2k") == colourOfTokens("42.3M") {
		t.Fatal("a session that has spent 42.3M reads the same as one that has spent 1.2k")
	}
	if colourOfTokens("") != colourOfTokens("1.2k") {
		t.Fatal("an empty cell is coloured differently from a small one, which draws the eye to nothing")
	}
}

// The cursor is a bar across the row, so a cell keeping its own colour inside the bar is unreadable:
// coloured text on a coloured background. The selected row is the one place colour comes off.
//
// The rows carry a state, because every row in every listing does and a case built from rows without
// one asserts against a path nothing reaches. That is how a whole row being drawn in its state went
// unnoticed while this case passed.
func TestTheSelectedRowIsNotColouredCellByCell(t *testing.T) {
	model := newTestModel(t, Sessions(&fakeClient{}))
	model, _ = update(t, model, rowsFor(model,
		Row{ID: "s1", State: StateReady, Cells: []string{"5d013d07", "acme", "bills", "d754610f", "idle", "dangerous", "", "", "", "1m"}},
		Row{ID: "s2", State: StateReady, Cells: []string{"6e124e18", "itv", "player", "e865721a", "idle", "plan", "", "", "", "2m"}}))

	view := model.View()
	selected, other := lineWith(t, view, "5d013d07"), lineWith(t, view, "6e124e18")

	if strings.Count(selected, "\x1b[38;5;") != 0 {
		t.Errorf("the selected row carries cell colours inside the cursor bar:\n%q", selected)
	}
	if strings.Count(other, "\x1b[38;5;") == 0 {
		t.Errorf("an unselected row carries no cell colour at all:\n%q", other)
	}
}

// Colour is added after the cells are cut to their columns, so it cannot change where anything sits.
// A row that lines up in a test and not on screen is the failure this prevents.
func TestColourDoesNotMoveTheColumns(t *testing.T) {
	model := newTestModel(t, Sessions(&fakeClient{}))
	model, _ = update(t, model, rowsFor(model,
		Row{ID: "s1", Cells: []string{"5d013d07", "acme", "bills", "d754610f", "idle", "dangerous", "", "", "", "1m"}},
		Row{ID: "s2", Cells: []string{"6e124e18", "itv", "player", "e865721a", "idle", "plan", "", "", "", "2m"}}))

	view := model.View()
	// Where a column starts, not how wide the line is. The whole line is padded to the panel after
	// the cells are joined, so its width is the same however badly the cells inside it are laid out:
	// measuring that passes on exactly the mistake this is here to catch. Two rows whose names are
	// different lengths put the status cell in the same place only if every cell before it kept its
	// padding.
	first, second := visibleText(lineWith(t, view, "5d013d07")), visibleText(lineWith(t, view, "6e124e18"))
	at, also := strings.Index(first, "idle"), strings.Index(second, "idle")

	if at < 0 || also < 0 {
		t.Fatalf("the status cell is missing from a row:\n%q\n%q", first, second)
	}
	if at != also {
		t.Fatalf("status starts at %d in one row and %d in the other, so the columns do not line up:\n%q\n%q",
			at, also, first, second)
	}
}

// A row that is doing fine is still a row with a workspace, a project, a name and a mode in it, and
// every one of those was drawn in a single green because the row had a state. This is the case that
// says a state no longer costs a row its cells.
func TestARowWithAStateStillCarriesTheColoursOfItsCells(t *testing.T) {
	for _, state := range []struct {
		named string
		is    State
	}{
		{"ready", StateReady},
		{"busy", StateBusy},
		{"stopped", StateStopped},
	} {
		t.Run(state.named, func(t *testing.T) {
			model := newTestModel(t, Sessions(&fakeClient{}))
			model, _ = update(t, model, rowsFor(model,
				Row{ID: "s1", State: StateReady, Cells: []string{"5d013d07", "acme", "bills", "d754610f", "idle", "dangerous", "", "", "", "1m"}},
				Row{ID: "s2", State: state.is, Cells: []string{"6e124e18", "itv", "player", "e865721a", "idle", "plan", "", "", "", "2m"}}))

			// The second row, so the cursor is not on it: the selected row has no cell colours by
			// design and would pass this for the wrong reason.
			line := lineWith(t, model.View(), "6e124e18")
			if !strings.Contains(line, colourOfName("itv")) {
				t.Errorf("a %s row does not carry its workspace's colour:\n%q", state.named, line)
			}
			if !strings.Contains(line, colourOfName("player")) {
				t.Errorf("a %s row does not carry its project's colour:\n%q", state.named, line)
			}
		})
	}
}

// The one state loud enough to keep the whole line. A session that ended badly has to read as ended
// badly from across the room, and that is worth more than knowing which workspace it was in.
func TestAFailedRowIsStillDrawnInOneColour(t *testing.T) {
	model := newTestModel(t, Sessions(&fakeClient{}))
	model, _ = update(t, model, rowsFor(model,
		Row{ID: "s1", State: StateReady, Cells: []string{"5d013d07", "acme", "bills", "d754610f", "idle", "dangerous", "", "", "", "1m"}},
		Row{ID: "s2", State: StateFailed, Cells: []string{"6e124e18", "itv", "player", "e865721a", "failed", "plan", "", "", "", "2m"}}))

	line := lineWith(t, model.View(), "6e124e18")
	if strings.Contains(line, colourOfName("itv")) {
		t.Errorf("a failed row is broken up by its cells' colours instead of reading as failed:\n%q", line)
	}
}

// The status cell is where the state went. Four statuses that mean four different things must not
// arrive on screen looking the same.
func TestTheStatusCellSaysHowARowIsDoing(t *testing.T) {
	idle, running := colourOfStatus("idle"), colourOfStatus("running")
	stopped, failed := colourOfStatus("stopped"), colourOfStatus("failed")

	seen := map[string]string{}
	for named, colour := range map[string]string{"idle": idle, "running": running, "stopped": stopped, "failed": failed} {
		if was, taken := seen[colour]; taken {
			t.Errorf("%q and %q are the same colour, and they mean different things", was, named)
		}
		seen[colour] = named
	}

	// The stale mark rides on the same cell, and a stale session is still running or still idle.
	if colourOfStatus("idle stale") != idle {
		t.Error("a stale session loses its status colour, so the cell says nothing about how it is doing")
	}
	if colourOfStatus("something new") != "" {
		t.Error("a status nobody has taught this is being coloured, which is a guess presented as a fact")
	}
}

// Age is the sessions tool's idle column under another name, and it carries that column's three
// bands: touched a moment ago, touched today, touched some other week.
func TestAgeIsColouredByHowLongAgoItWas(t *testing.T) {
	fresh, recent := colourOfAge("12s"), colourOfAge("2m")
	waiting, old, ancient := colourOfAge("40m"), colourOfAge("3h"), colourOfAge("6d")

	if fresh != recent {
		t.Error("a session touched twelve seconds ago and one touched two minutes ago read differently")
	}
	if fresh == waiting {
		t.Error("a session touched two minutes ago reads the same as one left forty minutes")
	}
	if waiting != old {
		t.Error("forty minutes and three hours read differently, and both are today")
	}
	if old == ancient {
		t.Error("a session from this morning reads the same as one from last week")
	}
	if colourOfAge("-") != dimCode {
		t.Error("a session with no age is being coloured as though it had one")
	}
}

// The sweep, so the next view added cannot be the flat one. Nine of the ten views shipped with no
// cell colour at all, and nothing said so: each of them looked deliberate on its own.
func TestEveryViewColoursTheCellsInItsRows(t *testing.T) {
	registry, err := NewDefaultRegistry(&fakeClient{})
	if err != nil {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}

	for _, name := range registry.Names() {
		resource, found := registry.Get(name)
		if !found {
			t.Fatalf("the registry lists %q and cannot produce it", name)
		}
		coloured := 0
		for _, column := range resource.Columns {
			if column.Colour != nil {
				coloured++
			}
		}
		if coloured == 0 {
			t.Errorf("the %q view draws every cell in one colour, so its rows can only be read one at a time", name)
		}
	}
}

// visibleText is what a terminal shows, with the escape sequences that colour it removed, so an index
// into it is a column on screen.
func visibleText(line string) string {
	var out strings.Builder
	inEscape := false
	for _, r := range line {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape && r == 'm':
			inEscape = false
		case !inEscape:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// lineWith is the drawn line containing text, so a case can look at one row rather than the screen.
func lineWith(t *testing.T, view, text string) string {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, text) {
			return line
		}
	}
	t.Fatalf("no line contains %q in:\n%s", text, view)
	return ""
}

// The context cell warns at the same share the line under the prompt warns at. An operator who reads
// one and then the other should not have to work out which of them means something.
func TestTheContextCellWarnsWhereTheLineUnderThePromptWarns(t *testing.T) {
	for _, tc := range []struct {
		cell string
		warn bool
	}{
		{cell: "12%", warn: false},
		{cell: "29%", warn: false},
		{cell: "30%", warn: true},
		{cell: "97%", warn: true},
		// A window nothing has measured, so there is no share yet to act on.
		{cell: "258k", warn: false},
		{cell: "", warn: false},
	} {
		t.Run(tc.cell, func(t *testing.T) {
			if warned := colourOfContext(tc.cell) == ansiYellowCode; warned != tc.warn {
				t.Errorf("%q is coloured %q, warning=%v, want warning=%v",
					tc.cell, colourOfContext(tc.cell), warned, tc.warn)
			}
		})
	}
}

// TestASessionHoldingAConversationIsNotColouredReady. Green is the console's word for "nothing is
// happening here, act on it if you like", and it is exactly the wrong thing to say over a container
// running somebody's conversation. The word and the colour have to agree.
func TestASessionHoldingAConversationIsNotColouredReady(t *testing.T) {
	ready := colourOfStatus(display.StatusIdle)
	for _, holding := range []string{display.StatusAwake, display.StatusAttached} {
		if colourOfStatus(holding) == ready {
			t.Errorf("%q is drawn the same as idle, so a listing invites an operator to take a "+
				"container somebody is using", holding)
		}
		if stateFromStatus(holding) != StateBusy {
			t.Errorf("a %q session does not read as busy, and something is running in it", holding)
		}
	}
	// The stale mark rides on the same cell here too.
	if colourOfStatus(display.StatusAwake+" stale") != colourOfStatus(display.StatusAwake) {
		t.Error("a stale awake session loses its colour")
	}
	// Unknown is the system saying it could not tell, so it is left uncoloured rather than dressed as
	// ready or as busy.
	if colourOfStatus(display.StatusUnknown) != "" {
		t.Error("a session the system could not read is being coloured, which is a guess shown as a fact")
	}
}

// TestTheRowsColourFollowsTheWordTheRowPrints. The cell and the row are coloured by two different
// paths, and a row that reads awake in green would say the opposite of its own word.
func TestTheRowsColourFollowsTheWordTheRowPrints(t *testing.T) {
	session := &quaycrewv1.Session{
		Id:       "5d013d07b9bcc8c05a1f437a",
		Status:   display.StatusIdle,
		Presence: quaycrewv1.SessionPresence_SESSION_PRESENCE_AWAKE,
	}
	if got := sessionRow(session, "acme", "house-bills").State; got != StateBusy {
		t.Fatalf("a session running a conversation is drawn as state %v, want busy", got)
	}
}
