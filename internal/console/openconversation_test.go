package console

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// cursorOn is the session the cursor is standing on, which is what the operator is pointing at.
func cursorOn(t *testing.T, model Model) string {
	t.Helper()
	selected, found := model.Selected()
	if !found {
		t.Fatal("the console has no row under its cursor")
	}
	return selected.ID
}

// terminalRecording keeps the command the console handed its own screen to.
func terminalRecording(handed *string) Terminal {
	return func(command *exec.Cmd, done func(error) tea.Msg) tea.Cmd {
		*handed = strings.Join(command.Args, " ")
		return func() tea.Msg { return done(nil) }
	}
}

// The console in a panel: a pane of its own with a conversation beside it. Open records what each
// pane runs, so a scenario reads what the operator is looking at rather than the call the key made.
type recordedPanes struct {
	made    int
	running map[string][]string
	beside  string
	closed  []string
}

func aPanelWithAConversationBeside(what []string) *recordedPanes {
	panes := &recordedPanes{running: map[string][]string{}}
	panes.beside, panes.running["%1"], panes.made = "%1", what, 1
	return panes
}

func (p *recordedPanes) Beside(string) (string, bool) { return p.beside, p.beside != "" }

func (p *recordedPanes) Open(_ string, argv []string) (string, error) {
	p.made++
	pane := fmt.Sprintf("%%%d", p.made)
	p.running[pane] = argv
	p.beside = pane
	return pane, nil
}

func (p *recordedPanes) Close(pane string) error {
	p.closed = append(p.closed, pane)
	delete(p.running, pane)
	p.beside = ""
	return nil
}

// showing is the conversation the operator is looking at beside the console: what the pane next to it
// is running, rather than what the console asked for.
func (p *recordedPanes) showing() string {
	if p.beside == "" {
		return "no conversation is open"
	}
	return strings.Join(p.running[p.beside], " ")
}

// theConversationOf is what the command line makes of a session the console hands it, which is a
// conversation of that session's own. Empty is the system opened with no argument, which is the
// driver, and that is deliberate.
func theConversationOf(selected string) ([]string, error) {
	if selected == "" {
		return []string{"krewe", "attach", "the-driver"}, nil
	}
	if selected == "s3" {
		return nil, fmt.Errorf("attach: session s3 has no conversation behind it")
	}
	return []string{"krewe", "attach", selected}, nil
}

// Three conversations in one list, and enter pressed on each of them in turn. The assertion is on the
// conversation open beside the console after the key has been through, not on the call the key made:
// a console that asked for the right session and opened the same chat every time is the defect this
// is about.
func TestEnterOpensTheConversationUnderTheCursorEveryTime(t *testing.T) {
	t.Setenv("TMUX_PANE", "%0")
	panes := aPanelWithAConversationBeside([]string{"krewe", "attach", "the-driver"})
	model := newTestModel(t, Sessions(&fakeClient{})).
		Beside(theConversationOf).WithPanes(panes)
	model, _ = update(t, model, rowsFor(model,
		row("s1", "s1", "acme"), row("s2", "s2", "acme"), row("s4", "s4", "acme")))

	for _, want := range []string{"s1", "s2", "s4"} {
		at := model
		for cursorOn(t, at) != want {
			at = walk(t, at, runes("j"))
		}
		at = walk(t, at, enter())

		if at.Reported() != nil {
			t.Fatalf("enter on %s was refused: %v", want, at.Reported())
		}
		if showing := panes.showing(); !strings.Contains(showing, want) {
			t.Fatalf("the cursor is on %s and the conversation beside the console is %q", want, showing)
		}
		if open, found := at.OpenBeside(); !found || open != want {
			t.Fatalf("the console says it is beside %q (%v), want %s", open, found, want)
		}
	}
}

// A session with no conversation behind it cannot be opened at all. The console has to say so and
// leave the conversation that is there alone: replacing it with somebody else's is the failure this
// whole job is about, and it would be worse arriving through the refusal path.
func TestEnterOnASessionWithNoConversationSaysSoAndOpensNothingElse(t *testing.T) {
	t.Setenv("TMUX_PANE", "%0")
	panes := aPanelWithAConversationBeside([]string{"krewe", "attach", "s1"})
	model := newTestModel(t, Sessions(&fakeClient{})).
		Beside(theConversationOf).WithPanes(panes)
	model, _ = update(t, model, rowsFor(model, row("s3", "s3", "acme")))

	model = walk(t, model, enter())

	if model.Reported() == nil {
		t.Fatal("enter on a session with no conversation opened something and said nothing")
	}
	if !strings.Contains(model.Reported().Error(), "s3") {
		t.Fatalf("the console said %v, want it to name the session it could not open", model.Reported())
	}
	if showing := panes.showing(); !strings.Contains(showing, "s1") {
		t.Fatalf("the conversation beside the console is now %q, want the one that was already there", showing)
	}
}

// A console on its own has no pane beside it to open into, so enter hands over its own screen, which
// is what it has always done and the only thing it can do.
func TestEnterHandsOverTheScreenWhenThereIsNoConversationBesideTheConsole(t *testing.T) {
	t.Setenv("TMUX_PANE", "%0")
	panes := &recordedPanes{running: map[string][]string{}}
	handed := ""
	model := newTestModel(t, Sessions(&fakeClient{})).
		Beside(theConversationOf).WithPanes(panes).
		WithTerminal(terminalRecording(&handed))
	model, _ = update(t, model, rowsFor(model, row("s1", "s1", "acme"), row("s2", "s2", "acme")))
	model = walk(t, model, runes("j"))

	model = walk(t, model, enter())

	if model.Reported() != nil {
		t.Fatalf("enter was refused: %v", model.Reported())
	}
	if !strings.Contains(handed, "s2") {
		t.Fatalf("the console handed its screen to %q, want the session under the cursor", handed)
	}
}
