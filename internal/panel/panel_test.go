package panel

import (
	"strings"
	"testing"
)

func line(argv []string) string { return strings.Join(argv, " ") }

func lines(commands [][]string) string {
	out := make([]string, 0, len(commands))
	for _, argv := range commands {
		out = append(out, line(argv))
	}
	return strings.Join(out, "\n")
}

// TestThePanelIsTwoHalvesSideBySide: the whole point of it. Half the screen each, split left and
// right, the console in one and the conversation in the other.
func TestThePanelIsTwoHalvesSideBySide(t *testing.T) {
	commands, err := Layout{Header: []string{"krewe", "header"}, HeaderRows: 10, Left: []string{"krewe", "console"}, Right: []string{"krewe", "attach", "s1"}}.Commands(Terminal{})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	got := lines(commands)

	for _, want := range []string{
		"tmux new-session -d -s krewe-panel -n panel krewe header",
		"tmux split-window -v -t krewe-panel:panel.0 krewe console",
		"tmux split-window -h -l 50% -t krewe-panel:panel.1 krewe attach s1",
		"tmux resize-pane -t krewe-panel:panel.0 -y 10",
		"tmux attach-session -t krewe-panel",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the panel does not run %q:\n%s", want, got)
		}
	}
}

// TestThePanelSplitsSideBySideRatherThanStacked: -h is the difference. Without it tmux stacks the
// panes and the console loses half its rows instead of half its width.
func TestThePanelSplitsSideBySideRatherThanStacked(t *testing.T) {
	commands, err := Layout{Header: []string{"krewe", "header"}, HeaderRows: 10, Left: []string{"krewe", "console"}, Right: []string{"krewe", "attach", "s1"}}.Commands(Terminal{})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	// The split that divides the two halves is the one inside the lower region; the other is the full
	// width cut that puts the header above them, and it is vertical on purpose.
	for _, argv := range commands {
		if argv[1] != "split-window" || !strings.Contains(line(argv), ".1") {
			continue
		}
		if !strings.Contains(line(argv), " -h ") {
			t.Fatalf("the two halves are stacked, not side by side: %s", line(argv))
		}
		if strings.Contains(line(argv), " -v ") {
			t.Fatalf("the two halves are divided vertically: %s", line(argv))
		}
		return
	}
	t.Fatal("the panel never divides the lower half, so there are no two halves")
}

// TestTheConsoleHasTheKeyboard: the operator opened the panel to use the console, so the cursor lands
// there rather than in the conversation, which is one pane away.
func TestTheConsoleHasTheKeyboard(t *testing.T) {
	commands, err := Layout{Header: []string{"krewe", "header"}, HeaderRows: 10, Left: []string{"krewe", "console"}, Right: []string{"krewe", "attach", "s1"}}.Commands(Terminal{})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	selected := ""
	for _, argv := range commands {
		if argv[1] == "select-pane" {
			selected = line(argv)
		}
	}
	if selected == "" {
		t.Fatal("the panel never says which pane has the keyboard, so it is whichever was made last")
	}
	// Pane 0 is the header, which nothing is typed into. Pane 1 is the console.
	if !strings.HasSuffix(selected, ".1") {
		t.Fatalf("the keyboard starts in %q, want the console", selected)
	}
}

// TestOpeningAPanelThatIsAlreadyOpenDoesNotSplitItAgain: the layout is built once. Splitting on every
// open is how a two pane panel becomes eleven panes by lunchtime.
func TestOpeningAPanelThatIsAlreadyOpenDoesNotSplitItAgain(t *testing.T) {
	commands, err := Layout{Header: []string{"krewe", "header"}, HeaderRows: 10, Left: []string{"krewe", "console"}, Right: []string{"krewe", "attach", "s1"}}.Commands(Terminal{AlreadyOpen: true})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	got := lines(commands)

	if strings.Contains(got, "split-window") {
		t.Fatalf("opening an open panel split it again:\n%s", got)
	}
	if strings.Contains(got, "new-session") {
		t.Fatalf("opening an open panel built a second one:\n%s", got)
	}
	if len(commands) != 1 || commands[0][1] != "attach-session" {
		t.Fatalf("opening an open panel should only attach, and it runs:\n%s", got)
	}
}

// TestTheSessionNameIsTheSameEveryTime, because that is what makes opening it twice come back to the
// one already open rather than starting a second.
func TestTheSessionNameIsTheSameEveryTime(t *testing.T) {
	layout := Layout{Header: []string{"krewe", "header"}, HeaderRows: 10, Left: []string{"krewe", "console"}, Right: []string{"krewe", "attach", "s1"}}
	asked := line(layout.HasSession())

	commands, err := layout.Commands(Terminal{})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	if !strings.Contains(asked, SessionName) {
		t.Fatalf("it asks about %q, which does not name the panel", asked)
	}
	if !strings.Contains(lines(commands), SessionName) {
		t.Fatalf("it asks about one session and builds another:\n%s", lines(commands))
	}
}

// TestAHalfEmptyPanelIsRefused: a pane with nothing in it exits immediately and tmux closes it, so
// the layout collapses back to one pane and reads as the panel being broken.
func TestAHalfEmptyPanelIsRefused(t *testing.T) {
	for _, test := range []struct {
		name   string
		layout Layout
	}{
		{"no console", Layout{Header: []string{"krewe", "header"}, HeaderRows: 10, Right: []string{"krewe", "attach", "s1"}}},
		{"no conversation", Layout{Header: []string{"krewe", "header"}, HeaderRows: 10, Left: []string{"krewe", "console"}}},
		{"none of it", Layout{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.layout.Commands(Terminal{}); err == nil {
				t.Fatal("a half empty panel was built")
			}
		})
	}
}

// TestThePanelOpensFromInsideTmux. Building the layout and then failing to show it is the same as not
// building it: tmux refuses to attach a client that is already inside one, so the panel was made,
// left running, and never appeared.
//
// The terminal said `sessions should be nested with care, unset $TMUX to force`, and the two panes
// were sitting there the whole time.
func TestThePanelOpensFromInsideTmux(t *testing.T) {
	layout := Layout{Header: []string{"krewe", "header"}, HeaderRows: 10, Left: []string{"krewe", "console"}, Right: []string{"krewe", "attach", "s1"}}

	commands, err := layout.Commands(Terminal{InsideTmux: true})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	got := lines(commands)

	if strings.Contains(got, "attach-session") {
		t.Fatalf("it attaches from inside tmux, which tmux refuses:\n%s", got)
	}
	if !strings.Contains(got, "switch-client -t "+SessionName) {
		t.Fatalf("it never moves the client to the panel:\n%s", got)
	}
	// It still has to be built, or there is nothing to switch to.
	if !strings.Contains(got, "new-session") || !strings.Contains(got, "split-window") {
		t.Fatalf("it switches to a panel it never built:\n%s", got)
	}
}

// TestThePanelSwitchesToOneAlreadyOpenFromInsideTmux: same refusal, on the path that only reattaches.
func TestThePanelSwitchesToOneAlreadyOpenFromInsideTmux(t *testing.T) {
	commands, err := Layout{Header: []string{"krewe", "header"}, HeaderRows: 10, Left: []string{"krewe", "console"}, Right: []string{"krewe", "attach", "s1"}}.
		Commands(Terminal{AlreadyOpen: true, InsideTmux: true})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	got := lines(commands)

	if strings.Contains(got, "attach-session") {
		t.Fatalf("it attaches from inside tmux, which tmux refuses:\n%s", got)
	}
	if !strings.Contains(got, "switch-client") {
		t.Fatalf("it does not move the client to the open panel:\n%s", got)
	}
}

// TestThePanelStillAttachesFromAPlainTerminal is the way off the other path (rule 46 in this
// repository): switching a client that does not exist is not a thing, so a plain terminal must keep
// attaching rather than being swept up in the fix for the nested one.
func TestThePanelStillAttachesFromAPlainTerminal(t *testing.T) {
	for _, open := range []bool{false, true} {
		commands, err := Layout{Header: []string{"krewe", "header"}, HeaderRows: 10, Left: []string{"krewe", "console"}, Right: []string{"krewe", "attach", "s1"}}.
			Commands(Terminal{AlreadyOpen: open})
		if err != nil {
			t.Fatalf("Commands: %v", err)
		}
		got := lines(commands)
		if !strings.Contains(got, "attach-session") {
			t.Fatalf("a plain terminal no longer attaches (already open: %v):\n%s", open, got)
		}
		if strings.Contains(got, "switch-client") {
			t.Fatalf("a plain terminal switches a client it does not have (already open: %v):\n%s", open, got)
		}
	}
}

// TestThePanelIsBuiltAtTheTerminalsSize. tmux scales the panes a session already has when a client of
// a different size attaches, so a header given ten rows in tmux's default eighty by twenty four came
// back as twenty one rows in a taller terminal. Building it at the real size is what makes the rows
// the header asked for the rows it gets.
func TestThePanelIsBuiltAtTheTerminalsSize(t *testing.T) {
	commands, err := Layout{
		Header: []string{"krewe", "header"}, HeaderRows: 10,
		Left: []string{"krewe", "console"}, Right: []string{"krewe", "attach", "s1"},
		Width: 180, Height: 46,
	}.Commands(Terminal{})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	if !strings.Contains(lines(commands), "-x 180 -y 46") {
		t.Fatalf("the panel is not built at the terminal's size:\n%s", lines(commands))
	}
}

// TestAPanelWithNoSizeStillOpens: a terminal that cannot be measured is not a reason to refuse, and
// tmux has a default for exactly this.
func TestAPanelWithNoSizeStillOpens(t *testing.T) {
	commands, err := Layout{
		Header: []string{"krewe", "header"}, HeaderRows: 10,
		Left: []string{"krewe", "console"}, Right: []string{"krewe", "attach", "s1"},
	}.Commands(Terminal{})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	if strings.Contains(lines(commands), " -x ") {
		t.Fatalf("it passed a size it does not have:\n%s", lines(commands))
	}
}

// TestTheHeaderSpansBothHalves. A tmux pane is a rectangle, so the window is cut top and bottom first
// and only the lower part is split left and right. Do it the other way round and there is no full
// width row left to put a header in.
func TestTheHeaderSpansBothHalves(t *testing.T) {
	commands, err := Layout{
		Header: []string{"krewe", "header"}, HeaderRows: 10,
		Left: []string{"krewe", "console"}, Right: []string{"krewe", "attach", "s1"},
	}.Commands(Terminal{})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	splits := make([]string, 0, 2)
	for _, argv := range commands {
		if argv[1] == "split-window" {
			splits = append(splits, line(argv))
		}
	}
	if len(splits) != 2 {
		t.Fatalf("want a full width cut then a side by side one, got:\n%s", strings.Join(splits, "\n"))
	}
	if !strings.Contains(splits[0], " -v ") {
		t.Fatalf("the first cut is not full width, so the header cannot span both halves:\n%s", splits[0])
	}
	if !strings.Contains(splits[1], "-t krewe-panel:panel.1") {
		t.Fatalf("the side by side cut is not inside the lower half:\n%s", splits[1])
	}
	if !strings.Contains(lines(commands), "resize-pane -t krewe-panel:panel.0 -y 10") {
		t.Fatalf("the header is not given exactly its rows:\n%s", lines(commands))
	}
	// And it keeps them: tmux scales panes when a client of a different size attaches.
	for _, when := range []string{"client-resized", "client-attached"} {
		if !strings.Contains(lines(commands), when) {
			t.Fatalf("the header is not put back on %s:\n%s", when, lines(commands))
		}
	}
}

// TestAPanelBuiltByAnOlderToolIsMadeAgain. Reattaching to an open panel is right until the tool has
// been upgraded: the panes are still running the old binary, so the fix that shipped this morning is
// not in what you are looking at, however many times you upgrade.
//
// It is what "still cant see the bloody logo man" turned out to be. The logo was drawn correctly by
// the new build, and the panel on screen was the old one.
func TestAPanelBuiltByAnOlderToolIsMadeAgain(t *testing.T) {
	layout := Layout{
		Version: "038fdd6",
		Header:  []string{"krewe", "header"}, HeaderRows: 1,
		Left: []string{"krewe", "console"}, Right: []string{"krewe", "attach", "s1"},
	}
	commands, err := layout.Commands(Terminal{AlreadyOpen: true, Stale: true})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	got := lines(commands)

	if !strings.HasPrefix(got, "tmux kill-session -t krewe-panel") {
		t.Fatalf("the old panel is not taken down first, so new-session reattaches to it:\n%s", got)
	}
	if !strings.Contains(got, "new-session") || strings.Count(got, "split-window") != 2 {
		t.Fatalf("the panel is not built again:\n%s", got)
	}
}

// TestAPanelBuiltByThisToolIsJustReattachedTo, or every open would tear down the conversation you were
// reading and start it again.
func TestAPanelBuiltByThisToolIsJustReattachedTo(t *testing.T) {
	commands, err := Layout{
		Version: "038fdd6",
		Header:  []string{"krewe", "header"}, HeaderRows: 1,
		Left: []string{"krewe", "console"}, Right: []string{"krewe", "attach", "s1"},
	}.Commands(Terminal{AlreadyOpen: true})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	got := lines(commands)

	for _, never := range []string{"kill-session", "new-session", "split-window"} {
		if strings.Contains(got, never) {
			t.Fatalf("opening a current panel runs %s:\n%s", never, got)
		}
	}
}

// TestThePanelRecordsWhatBuiltIt, or nothing can tell that it is stale.
func TestThePanelRecordsWhatBuiltIt(t *testing.T) {
	commands, err := Layout{
		Version: "038fdd6",
		Header:  []string{"krewe", "header"}, HeaderRows: 1,
		Left: []string{"krewe", "console"}, Right: []string{"krewe", "attach", "s1"},
	}.Commands(Terminal{})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	if !strings.Contains(lines(commands), VersionOption+" 038fdd6") {
		t.Fatalf("the panel does not record the build that made it:\n%s", lines(commands))
	}
	// And the same option is what gets read back.
	if !strings.Contains(strings.Join(Built(""), " "), VersionOption) {
		t.Fatalf("what is read back is not what was written: %v", Built(""))
	}
}

// TestCyclingPanesSkipsTheHeader. The header is one row of text with nothing to type into, so landing
// on it costs a keypress to arrive and another to leave, and the panel's two useful halves are three
// presses apart instead of one.
func TestCyclingPanesSkipsTheHeader(t *testing.T) {
	commands, err := Layout{
		Header: []string{"krewe", "header"}, HeaderRows: 1,
		Left: []string{"krewe", "console"}, Right: []string{"krewe", "attach", "s1"},
	}.Commands(Terminal{})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}

	bounce := ""
	for _, argv := range commands {
		if argv[1] == "set-hook" && contains(argv, "after-select-pane") {
			bounce = line(argv)
		}
	}
	if bounce == "" {
		t.Fatal("nothing stops the pane keys landing on the header, which has nothing to interact with")
	}
	// Scoped to this window. A hook on the server would change every tmux session the operator has.
	if !contains(strings.Fields(bounce), "-w") {
		t.Fatalf("the hook is not scoped to the panel's window, so it changes tmux everywhere:\n%s", bounce)
	}
	if !strings.Contains(bounce, "pane_index},0") {
		t.Fatalf("the hook does not test for the header pane:\n%s", bounce)
	}
	if !strings.Contains(bounce, "select-pane") {
		t.Fatalf("the hook notices the header and does not move off it:\n%s", bounce)
	}
}

// TestTheActivePaneIsVisiblyTheActiveOne. Three panes, one of them a single row, and an unlit border
// between them: the operator could not tell which half had the keyboard without typing something to
// find out.
func TestTheActivePaneIsVisiblyTheActiveOne(t *testing.T) {
	commands, err := Layout{
		Header: []string{"krewe", "header"}, HeaderRows: 1,
		Left: []string{"krewe", "console"}, Right: []string{"krewe", "attach", "s1"},
	}.Commands(Terminal{})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}

	// The style itself, not the command line it arrives on. Comparing whole lines compares the option
	// names too, which differ whatever the colours are, so the check below would pass with both
	// borders drawn identically.
	active, inactive := "", ""
	activeLine, inactiveLine := "", ""
	for _, argv := range commands {
		switch {
		case contains(argv, "pane-active-border-style"):
			active, activeLine = argv[len(argv)-1], line(argv)
		case contains(argv, "pane-border-style"):
			inactive, inactiveLine = argv[len(argv)-1], line(argv)
		}
	}
	if active == "" {
		t.Fatal("the pane with the keyboard is drawn the same as the ones without it")
	}
	if inactive == "" {
		t.Fatal("only the active border is styled, so it is lit against whatever the terminal defaults to")
	}
	if active == inactive {
		t.Fatalf("both borders are drawn %q, which says nothing about where the keyboard is", active)
	}
	for _, styled := range []string{activeLine, inactiveLine} {
		if !contains(strings.Fields(styled), "-w") {
			t.Fatalf("the style is not scoped to the panel's window, so it changes tmux everywhere:\n%s", styled)
		}
	}
}

// contains says whether argv holds a word, so a test can find a command by what it does rather than
// by counting its position in the list.
func contains(argv []string, want string) bool {
	for _, arg := range argv {
		if arg == want {
			return true
		}
	}
	return false
}
