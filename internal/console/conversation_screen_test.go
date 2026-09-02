package console

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The console asks tmux to put a conversation beside it and then asks tmux which pane appeared. When
// the command in that pane exits at once, there is no pane to find, and the console used to answer
// "the conversation opened and tmux does not say where", which is the one thing that had not happened.
//
// This drives a real multiplexer on a directory of its own, so it never touches the operator's own.
func TestTheConsoleSaysAConversationClosedRatherThanClaimingItOpened(t *testing.T) {
	here := aWindowOfOurOwn(t)

	msg := openConversationCmd(tmuxPanes{}, here, "a-session", []string{"sh", "-c", "exit 1"})().(conversationMsg)

	if msg.err == nil {
		t.Fatalf("the console reported pane %q, want the reason it never opened", msg.pane)
	}
	said := msg.err.Error()
	for _, want := range []string{"closed as soon as it opened", "krewe attach"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the console says %q, want it to say %q", said, want)
		}
	}
}

// And the conversation that does open is still found, or the message above would be what the operator
// reads every time.
func TestTheConsoleFindsTheConversationItOpened(t *testing.T) {
	here := aWindowOfOurOwn(t)

	msg := openConversationCmd(tmuxPanes{}, here, "a-session", []string{"sleep", "30"})().(conversationMsg)

	if msg.err != nil {
		t.Fatalf("opening a conversation that stays: %v", msg.err)
	}
	if msg.pane == "" || msg.pane == here {
		t.Fatalf("the console landed on pane %q, want the one it opened beside %q", msg.pane, here)
	}
}

// aWindowOfOurOwn starts a multiplexer under a temporary directory, so the server these tests drive
// is not the one the operator is sitting in, and answers with the pane the console would be in.
func aWindowOfOurOwn(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		// A missing dependency in continuous integration is not a pass: a check that runs nothing
		// reports exactly what a check that passed reports.
		if os.Getenv("CI") != "" {
			t.Fatalf("tmux is not installed, so this proves nothing: %v", err)
		}
		t.Skip("tmux is not installed on this machine")
	}
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", "screen", "-n", "panel",
		"-x", "120", "-y", "20", "sleep 300").CombinedOutput(); err != nil {
		t.Fatalf("starting a terminal: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-server").Run() })
	out, err := exec.Command("tmux", "list-panes", "-F", "#{pane_id}", "-t", "screen:panel").Output()
	if err != nil {
		t.Fatalf("asking which pane the console is in: %v", err)
	}
	return strings.TrimSpace(string(out))
}

var _ tea.Msg = conversationMsg{}
