package features_test

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/cucumber/godog"
)

// identifierWorld is what the operator copied off the screen, and what the last refused surface said.
type identifierWorld struct {
	// copied is the word the scenario is about to type back at every surface.
	copied string
	// refusal is what a surface said when it would not take it.
	refusal error
}

type identifierKey struct{}

func identifiersFrom(ctx context.Context) *identifierWorld {
	held, _ := ctx.Value(identifierKey{}).(*identifierWorld)
	return held
}

// listedSession is the row the scenario is about, and the cells a listing prints for it. Both
// surfaces render a session through display, so reading it here reads what the operator reads.
func listedSession(ctx context.Context) (*quaycrewv1.Session, []string, error) {
	w := worldFrom(ctx)
	current, err := w.lastTask()
	if err != nil {
		return nil, nil, err
	}
	resp, err := w.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: current.sessionID})
	if err != nil {
		return nil, nil, err
	}
	session := resp.GetSession()
	return session, display.SessionCells(session, w.workspaceName, w.projectName), nil
}

// reached is the session a surface landed on, resolved from what was copied. Every surface that takes
// a session goes through this one lookup, which is the point: there used to be two of them and they
// answered with different identifiers.
func reached(ctx context.Context) (*quaycrewv1.Session, error) {
	w, held := worldFrom(ctx), identifiersFrom(ctx)
	if held.copied == "" {
		return nil, fmt.Errorf("nothing was copied off the screen")
	}
	return workspace.Session(ctx, w.client, held.copied)
}

func initializeIdentifierSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, identifierKey{}, &identifierWorld{}), nil
	})

	sc.Step(`^the listing heads its first column "([^"]*)"$`, func(ctx context.Context, heading string) error {
		if got := display.SessionColumns()[0]; got != heading {
			return fmt.Errorf("the first column is headed %q, want %q", got, heading)
		}
		return nil
	})

	sc.Step(`^the first cell of that row is the session id$`, func(ctx context.Context) error {
		session, cells, err := listedSession(ctx)
		if err != nil {
			return err
		}
		if cells[0] != display.ShortID(session.GetId()) {
			return fmt.Errorf("the first cell reads %q, want the id %q", cells[0], display.ShortID(session.GetId()))
		}
		if cells[0] == display.ShortID(session.GetHandle()) {
			return fmt.Errorf("the first cell reads the handle, which no command took")
		}
		return nil
	})

	sc.Step(`^the name cell of that row is empty$`, func(ctx context.Context) error {
		session, cells, err := listedSession(ctx)
		if err != nil {
			return err
		}
		if cells[nameCell] != "" {
			return fmt.Errorf("the name cell reads %q, want nothing until somebody names it", cells[nameCell])
		}
		// The failure this replaced: the handle sat under the heading "name".
		if strings.Contains(strings.Join(cells, " "), display.ShortID(session.GetHandle())) {
			return fmt.Errorf("the row still prints the handle somewhere: %v", cells)
		}
		return nil
	})

	sc.Step(`^the name cell of that row reads "([^"]*)"$`, func(ctx context.Context, want string) error {
		_, cells, err := listedSession(ctx)
		if err != nil {
			return err
		}
		if cells[nameCell] != want {
			return fmt.Errorf("the name cell reads %q, want %q", cells[nameCell], want)
		}
		return nil
	})

	sc.Step(`^the operator copies the identifier out of the listing$`, func(ctx context.Context) error {
		_, cells, err := listedSession(ctx)
		if err != nil {
			return err
		}
		identifiersFrom(ctx).copied = cells[0]
		return nil
	})

	sc.Step(`^the operator copies the handle out of the system$`, func(ctx context.Context) error {
		session, _, err := listedSession(ctx)
		if err != nil {
			return err
		}
		identifiersFrom(ctx).copied = display.ShortID(session.GetHandle())
		return nil
	})

	sc.Step(`^the operator copies the address of that session$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, cells, err := listedSession(ctx)
		if err != nil {
			return err
		}
		identifiersFrom(ctx).copied = strings.Join([]string{w.workspaceName, w.projectName, cells[0]}, workspace.Separator)
		return nil
	})

	// Each of these does what its command does: resolve what was typed, then make the system call, then
	// read the system back. What is asserted is the state the session is left in, never the call.
	sc.Step(`^dispatch on what was copied continues that session$`, func(ctx context.Context) error {
		w, held := worldFrom(ctx), identifiersFrom(ctx)
		before, err := w.lastTask()
		if err != nil {
			return err
		}
		typed, words := workspace.SplitSession([]string{held.copied, "and again"})
		if typed != held.copied {
			return fmt.Errorf("%q was read as the message %v rather than as the session", held.copied, words)
		}
		session, err := reached(ctx)
		if err != nil {
			return err
		}
		if err := w.dispatch(ctx, session.GetProject(), session.GetHandle(), strings.Join(words, " ")); err != nil {
			return err
		}
		if w.lastErr != nil {
			return fmt.Errorf("dispatch on %q was refused: %w", held.copied, w.lastErr)
		}
		after, err := w.lastTask()
		if err != nil {
			return err
		}
		if after.sessionID != before.sessionID {
			return fmt.Errorf("the task ran in session %s, want %s", after.sessionID, before.sessionID)
		}
		return nil
	})

	sc.Step(`^tasks on what was copied lists that session's history$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		session, err := reached(ctx)
		if err != nil {
			return err
		}
		resp, err := w.client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: session.GetId()})
		if err != nil {
			return err
		}
		if len(resp.GetTasks()) == 0 {
			return fmt.Errorf("the history is empty, want the session's own tasks")
		}
		if resp.GetTasks()[0].GetPrompt() != "remember this" {
			return fmt.Errorf("the history opens with %q, want the session's first task",
				resp.GetTasks()[0].GetPrompt())
		}
		return nil
	})

	sc.Step(`^attach on what was copied opens that session's conversation$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		session, err := reached(ctx)
		if err != nil {
			return err
		}
		spec, err := w.client.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: session.GetId()})
		if err != nil {
			return err
		}
		if spec.GetSandbox() != sandbox.ContainerName(session.GetId()) {
			return fmt.Errorf("attach opened %q, want the sandbox of %s", spec.GetSandbox(),
				display.ShortID(session.GetId()))
		}
		return nil
	})

	sc.Step(`^label on what was copied names that session$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		session, err := reached(ctx)
		if err != nil {
			return err
		}
		if _, err := w.client.SetSessionLabel(ctx, &quaycrewv1.SetSessionLabelRequest{
			Id: session.GetId(), Label: "the water bill",
		}); err != nil {
			return err
		}
		read, err := w.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: session.GetId()})
		if err != nil {
			return err
		}
		if read.GetSession().GetLabel() != "the water bill" {
			return fmt.Errorf("the session is called %q, want the name that was just set",
				read.GetSession().GetLabel())
		}
		return nil
	})

	sc.Step(`^mode on what was copied sets that session's mode$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		session, err := reached(ctx)
		if err != nil {
			return err
		}
		if _, err := w.client.SetSessionPermissionMode(ctx, &quaycrewv1.SetSessionPermissionModeRequest{
			Id: session.GetId(), Mode: model.PermissionPlan,
		}); err != nil {
			return err
		}
		read, err := w.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: session.GetId()})
		if err != nil {
			return err
		}
		if read.GetSession().GetPermissionMode() != model.PermissionPlan {
			return fmt.Errorf("the session runs in %q, want the mode that was just set",
				read.GetSession().GetPermissionMode())
		}
		return nil
	})

	// The words as they are typed, so the split between a session and a message is what is under test
	// rather than a scenario's own idea of which word was which.
	sc.Step(`^the operator types the identifier and then "([^"]*)"$`, func(ctx context.Context, text string) error {
		_, cells, err := listedSession(ctx)
		if err != nil {
			return err
		}
		return typeAtDispatch(ctx, cells[0], text)
	})

	sc.Step(`^the operator types "([^"]*)" and then "([^"]*)"$`,
		func(ctx context.Context, first, second string) error {
			return typeAtDispatch(ctx, first, second)
		})

	sc.Step(`^the dispatch is refused$`, func(ctx context.Context) error {
		if identifiersFrom(ctx).refusal == nil {
			return fmt.Errorf("the dispatch was accepted, want a refusal")
		}
		return nil
	})

	sc.Step(`^the refusal names the identifier the listing prints$`, func(ctx context.Context) error {
		held := identifiersFrom(ctx)
		if held.refusal == nil {
			return fmt.Errorf("nothing was refused")
		}
		session, cells, err := listedSession(ctx)
		if err != nil {
			return err
		}
		if !strings.Contains(held.refusal.Error(), cells[0]) {
			return fmt.Errorf("the refusal is %q, want it to offer %q, which is what the listing prints",
				held.refusal, cells[0])
		}
		if strings.Contains(held.refusal.Error(), display.ShortID(session.GetHandle())) {
			return fmt.Errorf("the refusal is %q, and it offers the handle, which is nowhere on the screen",
				held.refusal)
		}
		return nil
	})

	sc.Step(`^the operator opens the console over the system$`, func(ctx context.Context) error {
		return consoleFrom(ctx).openModel(worldFrom(ctx))
	})

	sc.Step(`^the refusal says how to send it as the message instead$`, func(ctx context.Context) error {
		said := identifiersFrom(ctx).refusal
		if said == nil {
			return fmt.Errorf("nothing was refused")
		}
		if !strings.Contains(said.Error(), "quote the whole message") {
			return fmt.Errorf("the refusal is %q, and it does not say what to type instead", said)
		}
		return nil
	})

	sc.Step(`^that session was left with (\d+) task$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		resp, err := w.client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: current.sessionID})
		if err != nil {
			return err
		}
		if len(resp.GetTasks()) != want {
			return fmt.Errorf("the session holds %d tasks, want %d", len(resp.GetTasks()), want)
		}
		return nil
	})

	sc.Step(`^the system holds (\d+) session$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		resp, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
		if err != nil {
			return err
		}
		if len(resp.GetSessions()) != want {
			return fmt.Errorf("the system holds %d sessions, want %d: the refused word started one",
				len(resp.GetSessions()), want)
		}
		return nil
	})

	// The console, driven by a real key. The command it produces is run, and what came back is fed in
	// the way the runtime feeds it, so the assertion is about where the operator is left.
	sc.Step(`^the terminal cannot run what the console starts$`, func(ctx context.Context) error {
		consoleFrom(ctx).terminalErr = fmt.Errorf(`exec: "docker": executable file not found in $PATH`)
		return nil
	})

	sc.Step(`^the operator presses the enter key on the session row$`, func(ctx context.Context) error {
		return consoleFrom(ctx).press(tea.KeyMsg{Type: tea.KeyEnter})
	})

	sc.Step(`^the conversation that opened belongs to that session$`, func(ctx context.Context) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		if len(c.handedOver) != 1 {
			return fmt.Errorf("the terminal was handed %d commands, want exactly 1", len(c.handedOver))
		}
		line := strings.Join(c.handedOver[0].Args, " ")
		if !strings.Contains(line, sandbox.ContainerName(current.sessionID)) {
			return fmt.Errorf("the command is %q, want the sandbox of %s", line,
				display.ShortID(current.sessionID))
		}
		// And the system agrees: opening a conversation names one, so the session now holds it.
		read, err := w.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: current.sessionID})
		if err != nil {
			return err
		}
		if read.GetSession().GetModelSessionId() == "" {
			return fmt.Errorf("the session holds no conversation, so nothing was opened")
		}
		if !strings.Contains(line, read.GetSession().GetModelSessionId()) {
			return fmt.Errorf("the command is %q, want the conversation %q the system holds",
				line, read.GetSession().GetModelSessionId())
		}
		return nil
	})

	sc.Step(`^the console is back on its list with nothing to report$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		view := c.model.View()
		if !strings.Contains(view, "sessions") {
			return fmt.Errorf("the console is not back on its list:\n%s", view)
		}
		if said := c.model.Reported(); said != nil {
			return fmt.Errorf("the console reports %q after opening the conversation", said)
		}
		return nil
	})

	sc.Step(`^the next key still works$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")}); err != nil {
			return err
		}
		if !strings.Contains(c.model.View(), "tasks") {
			return fmt.Errorf("t after opening a conversation did not reach the history:\n%s", c.model.View())
		}
		return nil
	})

	sc.Step(`^the console says why the conversation did not open$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		said := c.model.Reported()
		if said == nil {
			return fmt.Errorf("the console says nothing, so the key reads as doing nothing:\n%s", c.model.View())
		}
		if !strings.Contains(said.Error(), "docker") {
			return fmt.Errorf("the console reports %q, want the reason the command did not run", said)
		}
		return nil
	})

	// Enter asks for a listing in the same return that records the reason, and that listing has
	// already arrived by the time this runs. Before the fix it took the reason with it: the rows below
	// are the proof the refresh happened, and the reason above is the proof it survived.
	sc.Step(`^the refreshed list is under it, with the reason still on the screen$`, func(ctx context.Context) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		view := c.model.View()
		if !strings.Contains(view, display.ShortID(current.sessionID)) {
			return fmt.Errorf("the list under the reason is empty, so nothing refreshed:\n%s", view)
		}
		if c.model.Reported() == nil {
			return fmt.Errorf("the refresh blanked the reason, so the key still reads as doing nothing")
		}
		return nil
	})

	sc.Step(`^the next key clears the reason$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}); err != nil {
			return err
		}
		if err := c.refresh(); err != nil {
			return err
		}
		if said := c.model.Reported(); said != nil {
			return fmt.Errorf("the reason outlived the key that read it: %q", said)
		}
		return nil
	})
}

// nameCell is where the name sits in a session row, which is the cell the handle used to occupy.
const nameCell = 3

// typeAtDispatch runs `quay task` over two typed words the way the command does: split them, read
// the first as a session if that is what it is, and dispatch the rest.
func typeAtDispatch(ctx context.Context, first, second string) error {
	w, held := worldFrom(ctx), identifiersFrom(ctx)
	held.refusal = nil
	typed, words := workspace.SplitSession([]string{first, second})
	text := strings.Join(words, " ")

	project, handle := w.projectID, ""
	if typed != "" {
		session, err := workspace.Session(ctx, w.client, typed)
		if err != nil {
			held.refusal = fmt.Errorf("%w\n\nto send %q as the message instead, quote the whole message", err, typed)
			return nil
		}
		project, handle = session.GetProject(), session.GetHandle()
	}
	return w.dispatch(ctx, project, handle, text)
}

// recordingTerminal stands in for handing the screen over. It keeps the command and answers with
// whatever the scenario said the terminal would do, so the console's own reducer sees the answer the
// runtime would give it.
func recordingTerminal(c *consoleWorld) func(*exec.Cmd, func(error) tea.Msg) tea.Cmd {
	return func(command *exec.Cmd, done func(error) tea.Msg) tea.Cmd {
		c.handedOver = append(c.handedOver, command)
		return func() tea.Msg { return done(c.terminalErr) }
	}
}
