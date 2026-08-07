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
	commands, err := Layout{Left: []string{"quay"}, Right: []string{"quay", "attach", "s1"}}.Commands(false)
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	got := lines(commands)

	for _, want := range []string{
		"tmux new-session -d -s quay-panel -n panel quay",
		"tmux split-window -h -l 50% -t quay-panel:panel quay attach s1",
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
	commands, err := Layout{Left: []string{"quay"}, Right: []string{"quay", "attach", "s1"}}.Commands(false)
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	for _, argv := range commands {
		if argv[1] != "split-window" {
			continue
		}
		if !strings.Contains(line(argv), " -h ") {
			t.Fatalf("the split is stacked, not side by side: %s", line(argv))
		}
		if strings.Contains(line(argv), " -v ") {
			t.Fatalf("the split is vertical: %s", line(argv))
		}
		return
	}
	t.Fatal("the panel never splits the window, so it is not a panel")
}

// TestTheConsoleHasTheKeyboard: the operator opened the panel to use the console, so the cursor lands
// there rather than in the conversation, which is one pane away.
func TestTheConsoleHasTheKeyboard(t *testing.T) {
	commands, err := Layout{Left: []string{"quay"}, Right: []string{"quay", "attach", "s1"}}.Commands(false)
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
	commands, err := Layout{Left: []string{"quay"}, Right: []string{"quay", "attach", "s1"}}.Commands(true)
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
	layout := Layout{Left: []string{"quay"}, Right: []string{"quay", "attach", "s1"}}
	asked := line(layout.HasSession())

	commands, err := layout.Commands(false)
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
		{"no console", Layout{Right: []string{"quay", "attach", "s1"}}},
		{"no conversation", Layout{Left: []string{"quay"}}},
		{"neither", Layout{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.layout.Commands(false); err == nil {
				t.Fatal("a half empty panel was built")
			}
		})
	}
}
