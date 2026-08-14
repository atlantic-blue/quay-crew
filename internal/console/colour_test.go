package console

import (
	"strings"
	"testing"
)

// A listing of thirty threads was one colour, so reading it meant reading every character of every
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
	// are the names actually on this crew, and they have to be told apart.
	seen := map[string]string{}
	for _, name := range []string{"atlantic-blue", "itv", "fanalysis", "juliantellez"} {
		colour := colourOfName(name)
		if was, taken := seen[colour]; taken {
			t.Errorf("%q and %q share a colour, and both are workspaces on this crew", was, name)
		}
		seen[colour] = name
	}
}

// The mode is the cell it costs most to misread: a thread that skips every permission must not look
// like one that asks.
func TestTheModeIsColouredByWhatItLetsAThreadDo(t *testing.T) {
	plan, edits, dangerous := colourOfMode("plan"), colourOfMode("edits"), colourOfMode("dangerous")

	if plan == dangerous {
		t.Fatal("planning and dangerous are the same colour, and they are opposite ends of what a thread may do")
	}
	if edits == dangerous {
		t.Fatal("edits and dangerous are the same colour")
	}
}

// A number nobody acts on should not compete with one they do.
func TestALargeTokenCountStandsOutAndASmallOneDoesNot(t *testing.T) {
	if colourOfTokens("1.2k") == colourOfTokens("42.3M") {
		t.Fatal("a thread that has spent 42.3M reads the same as one that has spent 1.2k")
	}
	if colourOfTokens("") != colourOfTokens("1.2k") {
		t.Fatal("an empty cell is coloured differently from a small one, which draws the eye to nothing")
	}
}

// The cursor is a bar across the row, so a cell keeping its own colour inside the bar is unreadable:
// coloured text on a coloured background. The selected row is the one place colour comes off.
func TestTheSelectedRowIsNotColouredCellByCell(t *testing.T) {
	model := newTestModel(t, Sessions(&fakeClient{}))
	model, _ = update(t, model, rowsFor(model,
		Row{ID: "s1", Cells: []string{"5d013d07", "acme", "bills", "d754610f", "idle", "dangerous", "", "", "", "1m"}},
		Row{ID: "s2", Cells: []string{"6e124e18", "itv", "player", "e865721a", "idle", "plan", "", "", "", "2m"}}))

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
