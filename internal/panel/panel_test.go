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

// TestAConversationOpensBesideTheConsoleAndNowhereElse. The console keeps the keyboard and half the
// width, and the conversation takes the other half of the same window.
func TestAConversationOpensBesideTheConsoleAndNowhereElse(t *testing.T) {
	commands, err := Beside("%3", []string{"krewe", "attach", "5ae35d77"})
	if err != nil {
		t.Fatalf("Beside: %v", err)
	}
	got := lines(commands)
	if strings.Count(got, "split-window") != 1 {
		t.Fatalf("opening a conversation runs %q, want one split", got)
	}
	if !strings.Contains(got, "split-window -h") {
		t.Fatalf("the split is %q, want it side by side rather than stacked", got)
	}
	if !strings.Contains(got, "-l 50%") {
		t.Fatalf("the halves are %q, want them equal", got)
	}
	// -d leaves the keyboard in the console. The operator asked to see a conversation, not to start
	// typing into it.
	if !strings.Contains(got, "split-window -h -d") {
		t.Fatalf("opening a conversation runs %q, and it takes the keyboard with it", got)
	}
	if !strings.Contains(got, "krewe attach 5ae35d77") {
		t.Fatalf("the pane runs %q, want the conversation that was asked for", got)
	}
}

// TestNothingIsSplitWithNowhereToSplitIt. A pane identifier is tmux's, so an empty one would split
// whatever pane tmux thinks is current, which is somebody else's screen.
func TestNothingIsSplitWithNowhereToSplitIt(t *testing.T) {
	for _, one := range []struct {
		name   string
		target string
		right  []string
	}{
		{name: "no pane to open beside", target: " ", right: []string{"krewe", "attach", "5ae35d77"}},
		{name: "nothing to put in it", target: "%3"},
	} {
		t.Run(one.name, func(t *testing.T) {
			if _, err := Beside(one.target, one.right); err == nil {
				t.Fatal("a conversation opened with nothing to open")
			}
		})
	}
}

// TestTheConversationIsTakenAwayAgain, which is the same key pressed a second time.
func TestTheConversationIsTakenAwayAgain(t *testing.T) {
	commands, err := Away("%4")
	if err != nil {
		t.Fatalf("Away: %v", err)
	}
	if got := lines(commands); got != "tmux kill-pane -t %4" {
		t.Fatalf("closing the conversation runs %q, want the pane killed", got)
	}
	if _, err := Away("  "); err == nil {
		t.Fatal("a pane with no identifier was killed, which is whichever pane tmux thinks is current")
	}
}

// TestThePanesAreAskedOfTmuxRatherThanRemembered. The operator can split their own window, so the
// console finds the pane beside it by asking where every pane starts.
func TestThePanesAreAskedOfTmuxRatherThanRemembered(t *testing.T) {
	if got := line(Opened("%3")); got != "tmux list-panes -F #{pane_id} -t %3" {
		t.Fatalf("the panes are listed with %q", got)
	}
	got := line(Rightward("%3"))
	for _, field := range []string{"#{pane_id}", "#{pane_left}", "#{pane_top}"} {
		if !strings.Contains(got, field) {
			t.Fatalf("asking where the panes are runs %q, and it never asks for %s", got, field)
		}
	}
}

// TestNothingHereBuildsAWindowOfItsOwn is rule 46 in this package: `krewe` opened a tmux session
// called krewe-panel with the console in one half, and it does not any more. A conversation is a pane
// beside a console that is already running, so nothing here may start a session, name a window, or
// attach a client.
func TestNothingHereBuildsAWindowOfItsOwn(t *testing.T) {
	built, err := Beside("%3", []string{"krewe", "attach", "5ae35d77"})
	if err != nil {
		t.Fatalf("Beside: %v", err)
	}
	away, err := Away("%4")
	if err != nil {
		t.Fatalf("Away: %v", err)
	}
	everything := strings.Join([]string{
		lines(built), lines(away), line(Opened("%3")), line(Rightward("%3")),
	}, "\n")
	for _, gone := range []string{"new-session", "attach-session", "switch-client", "krewe-panel", "krewe console"} {
		if strings.Contains(everything, gone) {
			t.Fatalf("this package still runs %q:\n%s", gone, everything)
		}
	}
}
