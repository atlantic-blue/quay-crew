package panel

import (
	"fmt"
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
	commands, err := Layout{Status: []string{" Version: dev"}, Left: []string{"quay", "console"}, Right: []string{"quay", "attach", "s1"}}.Commands(Terminal{})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	got := lines(commands)

	for _, want := range []string{
		"tmux new-session -d -s quay-panel -n panel quay console",
		"tmux split-window -h -l 50% -t quay-panel:panel.0 quay attach s1",
		"tmux attach-session -t quay-panel",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the panel does not run %q:\n%s", want, got)
		}
	}
}

// TestThePanelSplitsSideBySideRatherThanStacked. Julian chose side by side from two mockups, and -h
// is the difference: without it tmux stacks the panes and the console loses half its rows instead of
// half its width.
func TestThePanelSplitsSideBySideRatherThanStacked(t *testing.T) {
	commands, err := Layout{Status: []string{" Version: dev"}, Left: []string{"quay", "console"}, Right: []string{"quay", "attach", "s1"}}.Commands(Terminal{})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	for _, argv := range commands {
		if argv[1] != "split-window" {
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
	t.Fatal("the panel never splits the window, so there are no two halves")
}

// TestTheConsoleHasTheKeyboard: the operator opened the panel to use the console, so the cursor lands
// there rather than in the conversation, which is one pane away.
func TestTheConsoleHasTheKeyboard(t *testing.T) {
	commands, err := Layout{Status: []string{" Version: dev"}, Left: []string{"quay", "console"}, Right: []string{"quay", "attach", "s1"}}.Commands(Terminal{})
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
	// Pane 0 is the one new-session made, which is the console.
	if !strings.HasSuffix(selected, ".0") {
		t.Fatalf("the keyboard starts in %q, want the console", selected)
	}
}

// TestOpeningAPanelThatIsAlreadyOpenDoesNotSplitItAgain: the layout is built once. Splitting on every
// open is how a two pane panel becomes eleven panes by lunchtime.
func TestOpeningAPanelThatIsAlreadyOpenDoesNotSplitItAgain(t *testing.T) {
	commands, err := Layout{Status: []string{" Version: dev"}, Left: []string{"quay", "console"}, Right: []string{"quay", "attach", "s1"}}.Commands(Terminal{AlreadyOpen: true})
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
	layout := Layout{Status: []string{" Version: dev"}, Left: []string{"quay", "console"}, Right: []string{"quay", "attach", "s1"}}
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
		{"no console", Layout{Status: []string{" Version: dev"}, Right: []string{"quay", "attach", "s1"}}},
		{"no conversation", Layout{Status: []string{" Version: dev"}, Left: []string{"quay", "console"}}},
		{"no header", Layout{Left: []string{"quay", "console"}, Right: []string{"quay", "attach", "s1"}}},
		{"a header taller than tmux draws", Layout{Status: []string{"a", "b", "c", "d", "e", "f"}, Left: []string{"quay", "console"}, Right: []string{"quay", "attach", "s1"}}},
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
// Julian, having run it: "cant see the two panes". The terminal said `sessions should be nested with
// care, unset $TMUX to force`, and the two panes were sitting there the whole time.
func TestThePanelOpensFromInsideTmux(t *testing.T) {
	layout := Layout{Status: []string{" Version: dev"}, Left: []string{"quay", "console"}, Right: []string{"quay", "attach", "s1"}}

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
	commands, err := Layout{Status: []string{" Version: dev"}, Left: []string{"quay", "console"}, Right: []string{"quay", "attach", "s1"}}.
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
		commands, err := Layout{Status: []string{" Version: dev"}, Left: []string{"quay", "console"}, Right: []string{"quay", "attach", "s1"}}.
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

// TestTheHeaderIsTmuxsOwnStatusLine, not a pane. Julian: "why does header need a process?" It does
// not. A pane runs a program; tmux draws its status line itself, across the full width, at a height it
// owns, and it cannot scroll.
func TestTheHeaderIsTmuxsOwnStatusLine(t *testing.T) {
	commands, err := Layout{
		Status: []string{" Version: dev", " Address: localhost:50051", " Where: me/bills"},
		Left:   []string{"quay", "console"}, Right: []string{"quay", "attach", "s1"},
	}.Commands(Terminal{})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	got := lines(commands)

	// Two panes. A third would be a header that needs a process to draw it.
	if panes := strings.Count(got, "split-window") + 1; panes != 2 {
		t.Fatalf("the panel has %d panes, want two:\n%s", panes, got)
	}
	if !strings.Contains(got, "set-option -t quay-panel status 3") {
		t.Fatalf("the status line is not the height of the header:\n%s", got)
	}
	if !strings.Contains(got, "status-position top") {
		t.Fatalf("the header is not above the panes:\n%s", got)
	}
	for index, want := range []string{" Version: dev", " Address: localhost:50051", " Where: me/bills"} {
		if !strings.Contains(got, fmt.Sprintf("status-format[%d] %s", index, want)) {
			t.Fatalf("line %d of the header is not set to %q:\n%s", index, want, got)
		}
	}
	// Nothing has to hold it at a height, because tmux owns it.
	if strings.Contains(got, "resize-pane") || strings.Contains(got, "set-hook") {
		t.Fatalf("the header is still being held at a height by hand:\n%s", got)
	}
}

// TestAHeaderTallerThanTmuxDrawsIsRefused, rather than losing whichever lines did not fit. tmux
// answers "unknown value" to a sixth, and finding that out at runtime means a panel that opens wrong.
func TestAHeaderTallerThanTmuxDrawsIsRefused(t *testing.T) {
	tall := make([]string, MaxStatusLines+1)
	for index := range tall {
		tall[index] = " a line"
	}
	_, err := Layout{Status: tall, Left: []string{"quay"}, Right: []string{"quay", "attach", "s1"}}.
		Commands(Terminal{})
	if err == nil {
		t.Fatalf("a header of %d lines was accepted, and tmux draws %d", len(tall), MaxStatusLines)
	}
}

// TestTheHeaderIsAskedAgainWhileYouWork: where you are changes when `quay use` runs in the other pane,
// and a header naming the wrong place is worse than one naming none.
func TestTheHeaderIsAskedAgainWhileYouWork(t *testing.T) {
	commands, err := Layout{
		Status: []string{" Where: #(quay use)"},
		Left:   []string{"quay", "console"}, Right: []string{"quay", "attach", "s1"},
	}.Commands(Terminal{})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	if !strings.Contains(lines(commands), "status-interval") {
		t.Fatalf("the header is drawn once and never asked again:\n%s", lines(commands))
	}
}

// TestThePanelIsBuiltAtTheTerminalsSize. tmux scales the panes a session already has when a client of
// a different size attaches, so a header given ten rows in tmux's default eighty by twenty four came
// back as twenty one rows in a taller terminal. Building it at the real size is what makes the rows
// the header asked for the rows it gets.
func TestThePanelIsBuiltAtTheTerminalsSize(t *testing.T) {
	commands, err := Layout{
		Status: []string{" Version: dev"},
		Left:   []string{"quay", "console"}, Right: []string{"quay", "attach", "s1"},
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
		Status: []string{" Version: dev"},
		Left:   []string{"quay", "console"}, Right: []string{"quay", "attach", "s1"},
	}.Commands(Terminal{})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	if strings.Contains(lines(commands), " -x ") {
		t.Fatalf("it passed a size it does not have:\n%s", lines(commands))
	}
}

// TestTheHeaderCannotScrollOrChangeHeight. Julian: "the header should not be scrollable", and "it
// should have a fixed height". A status line is both by construction: tmux draws it at the number of
// lines it was set to, and a status line has no scrollback to scroll into.
//
// The pane version needed a resize hook on every client resize to hold its height, and an alternate
// screen so its own redraws did not become history. Neither exists any more, and that is the point.
func TestTheHeaderCannotScrollOrChangeHeight(t *testing.T) {
	commands, err := Layout{
		Status: []string{" one", " two", " three", " four"},
		Left:   []string{"quay", "console"}, Right: []string{"quay", "attach", "s1"},
		Width: 180, Height: 46,
	}.Commands(Terminal{})
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	got := lines(commands)

	if !strings.Contains(got, "status 4") {
		t.Fatalf("the header is not fixed at the lines it has:\n%s", got)
	}
	for _, byHand := range []string{"resize-pane", "set-hook"} {
		if strings.Contains(got, byHand) {
			t.Fatalf("the header is still held in place with %s, which tmux does itself:\n%s", byHand, got)
		}
	}
}
