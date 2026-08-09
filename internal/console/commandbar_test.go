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

// A command that takes over the screen must never reach the capturing runner: capturing one waits
// forever for output that is never coming, which is the console frozen. It is handed the terminal
// instead, so what this pins is that it does not go down the other road.
func TestAnInteractiveCommandIsNeverCaptured(t *testing.T) {
	for _, typed := range []string{"attach abc123", "panel", "header"} {
		ran := &ranCommand{output: "should not be captured"}
		model := typeInto(t, openBar(t, barModel(t, ran)), typed)

		if ran.args != nil {
			t.Errorf("%q was captured, and capturing it would freeze the console", typed)
		}
		if model.mode == modeOutput {
			t.Errorf("%q opened the output panel, so it was read rather than handed the terminal", typed)
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

// The one way in: a command that takes over the screen is handed the terminal rather than refused,
// which is exactly what pressing enter on a row already does.
func TestTheBarHandsTheTerminalOverForAnInteractiveCommand(t *testing.T) {
	model := barModel(t, &ranCommand{})

	for _, typed := range []string{"attach abc123", "panel", "header"} {
		command, err := model.handoverFor(strings.Fields(typed))
		if err != nil {
			t.Errorf("%q was refused: %v", typed, err)
			continue
		}
		if command == nil {
			t.Errorf("%q built no command to hand the terminal to", typed)
			continue
		}
		// The words typed, passed through as they were: the bar is running the tool, not
		// reinterpreting what was asked for.
		if got := strings.Join(command.Args[1:], " "); got != typed {
			t.Errorf("%q built the arguments %q", typed, got)
		}
	}
}

// A console inside the console is recursion rather than a feature, and the refusal says so instead
// of leaving somebody in two consoles wondering which one they are typing into.
func TestOpeningAConsoleInsideTheConsoleIsRefused(t *testing.T) {
	model := barModel(t, &ranCommand{})

	_, err := model.handoverFor([]string{"console"})
	if err == nil {
		t.Fatal("a console opened inside the console")
	}
	if !strings.Contains(err.Error(), "already") {
		t.Errorf("the refusal says %q, want it to say you are already here", err)
	}
}

// A command that only prints is still captured, so making the bar the way in did not turn every
// listing into a screen takeover.
func TestAPrintingCommandIsStillCaptured(t *testing.T) {
	ran := &ranCommand{output: "acme"}
	model := typeInto(t, openBar(t, barModel(t, ran)), "workspace list")

	if ran.args == nil {
		t.Fatal("a printing command was not captured")
	}
	if model.mode != modeOutput {
		t.Fatalf("the console is in mode %v, want the output panel", model.mode)
	}
}
