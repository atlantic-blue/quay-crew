package console

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ranCommand records what the console asked the tool to run, and answers with canned output, so
// these tests are about the bar rather than about starting processes.
type ranCommand struct {
	args   []string
	output string
	err    error
}

func (r *ranCommand) run(_ context.Context, args []string) (string, error) {
	r.args = args
	return r.output, r.err
}

// barModel is a console with the command runner wired to a double.
func barModel(t *testing.T, ran *ranCommand) Model {
	t.Helper()
	return newTestModel(t, staticResource("sessions", "s"), staticResource("workspaces", "p")).
		WithCommandRunner(ran.run)
}

// typeInto presses each character and then enter, then runs whatever the reducer handed back the
// way the runtime would, so the command's answer is fed in rather than assumed.
func typeInto(t *testing.T, model Model, typed string) Model {
	t.Helper()
	model = typeAll(t, model, typed)
	next, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		next, cmd = update(t, next, msg)
	}
	return next
}

func openBar(t *testing.T, model Model) Model {
	t.Helper()
	next, _ := update(t, model, runes(":"))
	if next.mode != modeCommand {
		t.Fatalf("the colon left the console in mode %v, want the command bar", next.mode)
	}
	return next
}

// The whole point: typing a quay command runs it and shows what it said, so reading the crew does
// not mean leaving the console for a shell.
func TestTheBarRunsAQuayCommandAndShowsItsOutput(t *testing.T) {
	ran := &ranCommand{output: "acme\nother\n"}
	model := typeInto(t, openBar(t, barModel(t, ran)), "workspace list")

	if len(ran.args) != 2 || ran.args[0] != "workspace" || ran.args[1] != "list" {
		t.Fatalf("the console ran %v, want the words that were typed", ran.args)
	}
	view := model.View()
	if !strings.Contains(view, "acme") || !strings.Contains(view, "other") {
		t.Fatalf("the console does not show what the command said:\n%s", view)
	}
}

// A view name still switches views, so everything the bar did before it could run commands keeps
// working: the bar does the obvious thing with what was typed.
func TestAViewNameStillSwitchesViews(t *testing.T) {
	ran := &ranCommand{output: "should not run"}
	model := typeInto(t, openBar(t, barModel(t, ran)), "workspaces")

	if ran.args != nil {
		t.Fatalf("typing a view name ran %v as a command", ran.args)
	}
	if model.active.Name != "workspaces" {
		t.Fatalf("the console is on %q, want it switched to workspaces", model.active.Name)
	}
}

// What a command said is worth reading, and a listing is taller than one row, so it scrolls the way
// the help overlay does rather than being cut off at the bottom of the screen.
func TestLongOutputScrolls(t *testing.T) {
	var many []string
	for i := range 200 {
		many = append(many, fmt.Sprintf("row %d", i))
	}
	ran := &ranCommand{output: strings.Join(many, "\n")}
	model := typeInto(t, openBar(t, barModel(t, ran)), "sessions list")

	first := model.View()
	if !strings.Contains(first, "row 0") {
		t.Fatalf("the output does not start at the top:\n%s", first)
	}
	for range 30 {
		model, _ = update(t, model, runes("j"))
	}
	scrolled := model.View()
	if scrolled == first {
		t.Fatal("moving down did not scroll the output")
	}
}

// A command that failed is worth more than one that worked, so what it said is shown rather than
// swallowed into a one line error.
func TestAFailedCommandShowsWhatItSaid(t *testing.T) {
	ran := &ranCommand{output: "there is no workspace called ghost", err: fmt.Errorf("exit status 1")}
	model := typeInto(t, openBar(t, barModel(t, ran)), "workspace show ghost")

	view := model.View()
	if !strings.Contains(view, "no workspace called ghost") {
		t.Fatalf("a failed command does not show what it said:\n%s", view)
	}
}

// Capturing a command that wants a terminal of its own would hang the console waiting for output
// that never comes, so those are refused by name before anything is started.
func TestACommandThatNeedsATerminalIsRefused(t *testing.T) {
	for _, typed := range []string{"attach abc123", "panel", "console", "header"} {
		ran := &ranCommand{output: "should not run"}
		model := typeInto(t, openBar(t, barModel(t, ran)), typed)

		if ran.args != nil {
			t.Errorf("%q was run, and capturing it would hang the console", typed)
		}
		if model.err == nil {
			t.Errorf("%q was accepted with no refusal", typed)
			continue
		}
		if !strings.Contains(model.err.Error(), "terminal of its own") {
			t.Errorf("%q is refused with %q, want it to say why", typed, model.err)
		}
		// And it names the command, so a refusal in a list of four reads as being about the one
		// that was typed.
		if !strings.Contains(model.err.Error(), strings.Fields(typed)[0]) {
			t.Errorf("%q is refused with %q, which does not name it", typed, model.err)
		}
	}
}

// Escape leaves the output the way it leaves every other overlay, back to the rows.
func TestEscapeClosesTheOutput(t *testing.T) {
	ran := &ranCommand{output: "acme"}
	model := typeInto(t, openBar(t, barModel(t, ran)), "workspace list")

	next, _ := update(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if next.mode != modeBrowse {
		t.Fatalf("escape left the console in mode %v, want it back on the rows", next.mode)
	}
	if strings.Contains(next.View(), "acme") {
		t.Fatalf("the output is still on screen after escape:\n%s", next.View())
	}
}

// A console with no runner wired is every console that is not the panel's. It says so rather than
// doing nothing, which reads as the key being broken.
func TestWithNoRunnerTheBarSaysSo(t *testing.T) {
	model := newTestModel(t, staticResource("sessions", "s"))
	next := typeInto(t, openBar(t, model), "workspace list")
	if next.err == nil {
		t.Fatal("a console with no runner accepted a command silently")
	}
}

// What the bar says under the words it is holding has to match what enter will do with them, or the
// hint is a lie about the very next keystroke.
func TestTheBarSaysWhatEnterWillDo(t *testing.T) {
	ran := &ranCommand{output: "acme"}
	model := typeAll(t, openBar(t, barModel(t, ran)), "workspace list")
	if view := model.View(); !strings.Contains(view, "runs this as a quay command") {
		t.Fatalf("the bar does not say it will run this:\n%s", view)
	}

	viewName := typeAll(t, openBar(t, barModel(t, ran)), "workspaces")
	if view := viewName.View(); strings.Contains(view, "runs this as a quay command") {
		t.Fatalf("the bar offers to run a view name as a command:\n%s", view)
	}
}
