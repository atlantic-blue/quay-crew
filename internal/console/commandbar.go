package console

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// CommandRunner runs one quay command and hands back everything it printed.
//
// The console runs the tool rather than importing it: every command lives in package main under
// cmd/quay and cannot be imported, and the console already builds commands for attaching and for
// the panel, so this is the shape that was already here. It also means a command added later works
// in the bar without being plumbed through anything.
//
// The output is returned even when the command failed, because what a failed command said is worth
// more than the fact that it failed.
type CommandRunner func(ctx context.Context, args []string) (string, error)

// needTerminal are the commands that take over the screen rather than printing and exiting.
//
// Capturing one would leave the console waiting forever for output that is never coming, so instead
// the console suspends and hands over the real terminal, which is exactly what pressing enter on a
// row already does. That is what makes the bar the one way in rather than a reading tool.
var needTerminal = map[string]bool{
	"attach": true,
	"panel":  true,
	"header": true,
	"shell":  true,
}

// alreadyHere are the commands that would open a second copy of what you are already looking at.
// Refused rather than run, because two consoles is recursion rather than a feature and nobody can
// tell which one they are typing into.
var alreadyHere = map[string]string{
	"console": "you are already in the console",
	toolName:  "you are already in the crew",
}

// toolName is what the tool is called, which is also the word an operator types out of habit at the
// front of a command they are already inside.
const toolName = "quay"

// commandTimeout is how long the bar waits for a command. Long enough for anything that reads the
// crew, short enough that a command which will never answer gives the console back.
const commandTimeout = 30 * time.Second

// waitDelay is how long to keep reading a killed command's output before giving up on its pipes.
// Long enough for a program that is exiting cleanly to finish saying what it was saying, short
// enough that a child holding the pipe open does not hold the console with it.
const waitDelay = 200 * time.Millisecond

// WithCommandRunner wires the bar to whatever runs a quay command. A console with none refuses to
// run anything and says so, which is better than a key that appears to do nothing.
func (m Model) WithCommandRunner(run CommandRunner) Model {
	m.runCommand = run
	return m
}

// commandOutputMsg is what a command printed, on its way back to the console.
type commandOutputMsg struct {
	typed  string
	output string
	err    error
}

// runTyped decides what the words in the bar are: a view to switch to, or a command to run.
//
// A view name wins, because that is what the bar has always done and `:sessions` must keep working.
// Anything else is handed to the tool, so the bar does the obvious thing with what was typed rather
// than making the operator remember which kind of thing they are typing.
func (m Model) runTyped() (Model, tea.Cmd) {
	typed := strings.TrimSpace(m.input)
	m.mode, m.input = modeBrowse, ""
	if typed == "" {
		return m, nil
	}

	if _, found := m.registry.Resolve(typed); found {
		m.input = typed
		return m.openTyped()
	}

	args := strings.Fields(typed)
	// Typing the tool's own name is what anybody does out of habit, and passing it to itself gives
	// `unknown command "quay"`, which reads as the bar being broken rather than as a typo. The name
	// on its own is asking for the thing already on screen, so that is answered rather than run.
	if args[0] == toolName {
		if len(args) == 1 {
			m.err = fmt.Errorf("%s, so there is nothing to open: type a command after it, or press escape", alreadyHere[toolName])
			return m, nil
		}
		args = args[1:]
	}
	if needTerminal[args[0]] {
		command, err := m.handoverFor(args)
		if err != nil {
			m.err = err
			return m, nil
		}
		// The console suspends, the command gets the real terminal, and the console comes back when
		// it is done: the same handover enter on a row makes.
		return m, tea.ExecProcess(command, func(err error) tea.Msg {
			return actionDoneMsg{err: err}
		})
	}
	if m.runCommand == nil {
		m.err = fmt.Errorf("this console cannot run commands, so %q has nowhere to go; open the crew with quay", typed)
		return m, nil
	}

	run := m.runCommand
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		output, err := run(ctx, args)
		return commandOutputMsg{typed: typed, output: output, err: err}
	}
}

// showCommandOutput puts what a command said on the screen.
//
// A command that failed shows its output too, and the error under it: the words are the useful part,
// and "exit status 1" on its own tells nobody anything. A command that said nothing at all says so,
// because a blank panel reads as the console having broken rather than as an empty listing.
func (m Model) showCommandOutput(msg commandOutputMsg) Model {
	m.mode, m.commandTop = modeOutput, 0
	m.commandTyped = msg.typed
	m.commandOutput = strings.Split(strings.TrimRight(msg.output, "\n"), "\n")
	if strings.TrimSpace(msg.output) == "" {
		m.commandOutput = []string{"(it said nothing)"}
	}
	m.err = msg.err
	return m
}

// updateOutputKey scrolls what a command said, and closes it on anything else. The same shape the
// help overlay has, because it is the same kind of thing: something to read, over the rows.
func (m Model) updateOutputKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.commandTop--
	case "down", "j":
		m.commandTop++
	case "pgup", "ctrl+b":
		m.commandTop -= m.bodyHeight()
	case "pgdown", "ctrl+f":
		m.commandTop += m.bodyHeight()
	default:
		m.mode, m.commandTop, m.err = modeBrowse, 0, nil
		m.commandOutput, m.commandTyped = nil, ""
		return m, nil
	}
	if m.commandTop < 0 {
		m.commandTop = 0
	}
	if most := len(m.commandOutput) - 1; m.commandTop > most {
		m.commandTop = most
	}
	return m, nil
}

// commandBody is the window of the output that fits, with the command that produced it at the top
// so a screen of rows says what it is answering.
func (m Model) commandBody() []string {
	height := m.bodyHeight()
	if height < 1 {
		height = 1
	}
	// Cut and padded to exactly the panel's width: the frame puts a border either side of whatever
	// it is given, so a short line leaves the right border next to the text and a long one spills
	// past it and wraps the terminal. Either way the panel comes out ragged.
	lines := []string{m.fit(prompt.Render(":" + m.commandTyped))}
	for _, line := range m.commandOutput {
		lines = append(lines, m.fit(line))
	}
	top := m.commandTop
	if top > len(lines)-1 {
		top = len(lines) - 1
	}
	if top < 0 {
		top = 0
	}
	end := top + height
	if end > len(lines) {
		end = len(lines)
	}
	return lines[top:end]
}

// handoverFor builds the command the console suspends itself for.
//
// The words as they were typed, passed to this same binary: the bar runs the tool rather than
// reinterpreting what was asked for, so a command that grows an option tomorrow takes it here too.
func (m Model) handoverFor(args []string) (*exec.Cmd, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("nothing to run")
	}
	if because, refused := alreadyHere[args[0]]; refused {
		return nil, fmt.Errorf("%s, so there is nothing to open: press escape to get back to the rows", because)
	}
	self, err := os.Executable()
	if err != nil {
		self = "quay"
	}
	return exec.Command(self, args...), nil
}

// TheToolItself runs a quay command by running this same binary again.
//
// The tool rather than a copy of its logic: every command lives in package main under cmd/quay and
// cannot be imported, and running the real one means a command added tomorrow works in the bar with
// nothing plumbed through. It costs one short lived process per command typed, which is nothing
// against a person's typing speed.
//
// Standard error is folded in with standard output, because a refusal is what the operator most
// needs to read and it comes back on the error stream.
func TheToolItself() CommandRunner {
	return func(ctx context.Context, args []string) (string, error) {
		self, err := os.Executable()
		if err != nil {
			// The name is resolved from PATH, which is the copy the shell ran to get here anyway.
			self = "quay"
		}
		return RunNamed(ctx, self, args)
	}
}

// RunNamed runs one program and hands back everything it printed on either stream.
//
// Exported so the runner the console ships with can be driven against a real process in a test: a
// double proves the bar reacts to output, and only this proves the console can start something,
// wait for it, and read what it said.
func RunNamed(ctx context.Context, program string, args []string) (string, error) {
	command := exec.CommandContext(ctx, program, args...)
	// Killing a program does not close the pipes its own children inherited, so reading its output
	// can block long past the deadline: a command whose child outlives it would freeze the console
	// for as long as that child ran, whatever the timeout said. This gives up on the pipes shortly
	// after the kill, which is what makes the deadline mean something.
	command.WaitDelay = waitDelay
	// Both streams together, because a refusal comes back on the error one and that is what the
	// operator most needs to read.
	output, err := command.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return string(output), fmt.Errorf("%s took longer than %s and was given up on", strings.Join(args, " "), commandTimeout)
	}
	return string(output), err
}
