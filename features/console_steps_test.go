package features_test

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/console"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/cucumber/godog"
)

// consoleWorld is the console's own scenario state, kept beside the shared world rather than inside
// it so the console scenarios do not widen what every other scenario carries.
type consoleWorld struct {
	secondProject string
	registry      *console.Registry
	active        console.Resource
	rows          []console.Row
	// opened is the command the console would hand the terminal, and openErr the reason it would not.
	opened  *exec.Cmd
	openErr error
	// model is the real console, driven by keys, for the scenarios about what a key does.
	model console.Model
}

type consoleKey struct{}

func consoleFrom(ctx context.Context) *consoleWorld {
	c, _ := ctx.Value(consoleKey{}).(*consoleWorld)
	return c
}

// open builds the console's registry against the live control plane and lists one resource.
func (c *consoleWorld) open(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, name string) error {
	registry, err := console.NewDefaultRegistry(client)
	if err != nil {
		return err
	}
	resource, found := registry.Get(name)
	if !found {
		return fmt.Errorf("the console has no resource named %q", name)
	}
	c.registry, c.active = registry, resource
	return c.list(ctx, "")
}

func (c *consoleWorld) list(ctx context.Context, parent string) error {
	rows, err := c.active.List(ctx, parent)
	if err != nil {
		return fmt.Errorf("list %s: %w", c.active.Name, err)
	}
	c.rows = rows
	return nil
}

// initializeConsoleSteps registers the console scenarios' steps. It is called from
// initializeScenario so the console keeps its steps in its own file.
func initializeConsoleSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, consoleKey{}, &consoleWorld{}), nil
	})

	sc.Step(`^the operator dispatches "([^"]*)" to the second workspace$`, func(ctx context.Context, text string) error {
		w := worldFrom(ctx)
		if w.secondWorkspaceID == "" {
			return fmt.Errorf("no second workspace was created")
		}
		return w.dispatch(ctx, w.secondWorkspaceID, "", text)
	})

	sc.Step(`^a second project named "([^"]*)"$`, func(ctx context.Context, name string) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		resp, err := w.client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{Workspace: w.workspaceID, Name: name})
		if err != nil {
			return err
		}
		// The background's project is the one the other steps mean, so record this one separately.
		c.secondProject = resp.GetProject().GetId()
		return nil
	})

	sc.Step(`^the operator dispatches "([^"]*)" to the second project$`, func(ctx context.Context, text string) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		if c.secondProject == "" {
			return fmt.Errorf("no second project was created")
		}
		return w.dispatch(ctx, c.secondProject, "", text)
	})

	sc.Step(`^the operator drills into project "([^"]*)"$`, func(ctx context.Context, name string) error {
		return consoleFrom(ctx).drillInto(ctx, worldFrom(ctx).client, "projects", name)
	})

	sc.Step(`^the operator opens the console$`, func(ctx context.Context) error {
		return consoleFrom(ctx).open(ctx, worldFrom(ctx).client, console.Default)
	})

	sc.Step(`^the operator opens the console on workspaces$`, func(ctx context.Context) error {
		return consoleFrom(ctx).open(ctx, worldFrom(ctx).client, "workspaces")
	})

	sc.Step(`^the operator drills into workspace "([^"]*)"$`, func(ctx context.Context, name string) error {
		return consoleFrom(ctx).drillInto(ctx, worldFrom(ctx).client, "workspaces", name)
	})

	sc.Step(`^the console lists (\d+) sessions?$`, func(ctx context.Context, want int) error {
		return expectRows(consoleFrom(ctx), "sessions", want)
	})

	// The command bar resolves what was typed, so this drives the same path a keystroke does rather
	// than asking the registry a question the operator never asks it.
	sc.Step(`^the operator opens the console by typing "([^"]*)"$`, func(ctx context.Context, typed string) error {
		c := consoleFrom(ctx)
		registry, err := console.NewDefaultRegistry(worldFrom(ctx).client)
		if err != nil {
			return err
		}
		resource, found := registry.Resolve(typed)
		if !found {
			return fmt.Errorf("the command bar does not know %q", typed)
		}
		c.registry, c.active = registry, resource
		return c.list(ctx, "")
	})

	sc.Step(`^the console is showing sessions$`, func(ctx context.Context) error {
		if got := consoleFrom(ctx).active.Name; got != "sessions" {
			return fmt.Errorf("the console is showing %q, want sessions", got)
		}
		return nil
	})

	// A session exists only once a turn creates it, so a failing runner is how you get one with no
	// conversation behind it.
	sc.Step(`^a session whose first turn failed$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		w.runner.failNext = true
		_ = w.dispatch(ctx, w.projectID, "", "this turn fails")
		return nil
	})

	sc.Step(`^the operator presses enter on the selected session$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		row, err := onlyRow(c)
		if err != nil {
			return err
		}
		c.pressEnter(row)
		return nil
	})

	sc.Step(`^the console opens that session's conversation$`, func(ctx context.Context) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		if c.openErr != nil {
			return fmt.Errorf("enter was refused: %w", c.openErr)
		}
		if c.opened == nil {
			return fmt.Errorf("enter produced no command")
		}
		current, err := w.lastTurn()
		if err != nil {
			return err
		}
		line := strings.Join(c.opened.Args, " ")
		for _, want := range []string{sandbox.ContainerName(current.sessionID), "claude --resume conversation-1"} {
			if !strings.Contains(line, want) {
				return fmt.Errorf("the command is %q, want it to carry %q", line, want)
			}
		}
		return nil
	})

	sc.Step(`^the operator opens the console and presses backspace on the session$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if err := c.openModel(worldFrom(ctx).client); err != nil {
			return err
		}
		return c.press(tea.KeyMsg{Type: tea.KeyBackspace})
	})

	sc.Step(`^the operator answers "([^"]*)"$`, func(ctx context.Context, answer string) error {
		return consoleFrom(ctx).press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(answer)})
	})

	sc.Step(`^the console asks whether to stop that session$`, func(ctx context.Context) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		current, err := w.lastTurn()
		if err != nil {
			return err
		}
		view := c.model.View()
		want := "stop session " + display.ShortID(current.threadID) + "?"
		if !strings.Contains(view, want) {
			return fmt.Errorf("the console does not ask %q:\n%s", want, view)
		}
		return nil
	})

	sc.Step(`^the operator opens the console and archives the session$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if err := c.openModel(worldFrom(ctx).client); err != nil {
			return err
		}
		if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")}); err != nil {
			return err
		}
		if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}); err != nil {
			return err
		}
		// The console asked; whichever view a later step reads is listed fresh from the control plane.
		return c.open(ctx, worldFrom(ctx).client, console.Default)
	})

	sc.Step(`^the archived view lists (\d+) sessions?$`, func(ctx context.Context, want int) error {
		c := consoleFrom(ctx)
		if err := c.open(ctx, worldFrom(ctx).client, "archived"); err != nil {
			return err
		}
		return expectRows(c, "archived", want)
	})

	sc.Step(`^the archived session still holds its conversation$`, func(ctx context.Context) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		row, err := onlyRow(c)
		if err != nil {
			return err
		}
		session, err := w.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: row.ID})
		if err != nil {
			return err
		}
		if session.GetSession().GetModelSessionId() == "" {
			return fmt.Errorf("the archived thread has no conversation handle left")
		}
		return nil
	})

	sc.Step(`^the console says the session has no conversation yet$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if c.openErr == nil {
			return fmt.Errorf("enter opened %v, want a reason the operator can act on", c.opened)
		}
		if !strings.Contains(c.openErr.Error(), "no conversation yet") {
			return fmt.Errorf("the reason is %q, want it to say there is no conversation yet", c.openErr)
		}
		return nil
	})

	sc.Step(`^the console lists (\d+) projects?$`, func(ctx context.Context, want int) error {
		return expectRows(consoleFrom(ctx), "projects", want)
	})

	sc.Step(`^the console lists (\d+) workspaces?$`, func(ctx context.Context, want int) error {
		return expectRows(consoleFrom(ctx), "workspaces", want)
	})

	sc.Step(`^the console can drill from workspaces into projects$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if c.active.DrillTo != "projects" {
			return fmt.Errorf("workspaces drills into %q, want projects", c.active.DrillTo)
		}
		if _, found := c.registry.Get("projects"); !found {
			return fmt.Errorf("projects is not a registered resource, so the drill would dead end")
		}
		return nil
	})

	sc.Step(`^the console shows the session's workspace as "([^"]*)"$`, func(ctx context.Context, want string) error {
		c := consoleFrom(ctx)
		row, err := onlyRow(c)
		if err != nil {
			return err
		}
		// The workspace column is the second one the sessions resource declares.
		const workspaceColumn = 1
		if got := row.Cells[workspaceColumn]; got != want {
			return fmt.Errorf("the console shows the workspace as %q, want the name %q", got, want)
		}
		return nil
	})

	sc.Step(`^the console shows the session identifier shortened$`, func(ctx context.Context) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		row, err := onlyRow(c)
		if err != nil {
			return err
		}
		full, err := w.lastTurn()
		if err != nil {
			return err
		}
		shown := row.Cells[0]
		if shown == full.sessionID {
			return fmt.Errorf("the console shows the whole identifier %q, want it shortened", shown)
		}
		if !strings.HasPrefix(full.sessionID, shown) {
			return fmt.Errorf("the shortened identifier %q is not a prefix of %q", shown, full.sessionID)
		}
		// The row must still carry the whole thing, or every action on it breaks.
		if row.ID != full.sessionID {
			return fmt.Errorf("the row carries %q, want the whole identifier %q", row.ID, full.sessionID)
		}
		return nil
	})

	sc.Step(`^the operator stops the selected session from the console$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		row, err := onlyRow(c)
		if err != nil {
			return err
		}
		for _, action := range c.active.Actions {
			if action.Label != "Stop" {
				continue
			}
			if action.Run == nil {
				return fmt.Errorf("the Stop action has nothing to run")
			}
			return action.Run(ctx, row)
		}
		return fmt.Errorf("the sessions view has no Stop action")
	})
}

// drive runs a console command and feeds everything it produces back into the model, which is what
// the bubbletea runtime does with no terminal in the way. It is how a scenario can press a key
// against the real control plane and then assert on the store rather than on a double.
//
// A batch is unpacked rather than run whole, because one of the console's own commands is the three
// second refresh clock and a scenario should not wait for it.
func drive(model console.Model, cmd tea.Cmd) (console.Model, error) {
	if cmd == nil {
		return model, nil
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, inner := range msg {
			next, err := drive(model, inner)
			if err != nil {
				return model, err
			}
			model = next
		}
		return model, nil
	case nil:
		return model, nil
	default:
		updated, next := model.Update(msg)
		typed, isModel := updated.(console.Model)
		if !isModel {
			return model, fmt.Errorf("the console returned %T, want a console.Model", updated)
		}
		return drive(typed, next)
	}
}

// press sends one key through the console and runs whatever it asks for.
func (c *consoleWorld) press(key tea.KeyMsg) error {
	updated, cmd := c.model.Update(key)
	typed, isModel := updated.(console.Model)
	if !isModel {
		return fmt.Errorf("the console returned %T, want a console.Model", updated)
	}
	next, err := drive(typed, cmd)
	if err != nil {
		return err
	}
	c.model = next
	return nil
}

// openModel builds the real console over the live control plane and loads its opening view. It asks
// for a refresh rather than calling Init, because Init also starts the refresh clock.
func (c *consoleWorld) openModel(client quaycrewv1.ControlPlaneServiceClient) error {
	registry, err := console.NewDefaultRegistry(client)
	if err != nil {
		return err
	}
	model, err := console.New(registry, console.Default, nil)
	if err != nil {
		return err
	}
	c.model = model
	return c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
}

// pressEnter runs whatever the active view has bound to enter on the single listed row, which is the
// same path a keypress takes. It records the command and the refusal rather than asserting on either,
// so the two scenarios can each say what they expect.
func (c *consoleWorld) pressEnter(row console.Row) {
	for _, action := range c.active.Actions {
		if !action.Bound("enter") || action.Shell == nil {
			continue
		}
		c.opened, c.openErr = action.Shell(row)
		return
	}
	c.openErr = fmt.Errorf("the %s view has nothing bound to enter", c.active.Name)
}

// onlyRow returns the single listed row, so a scenario asserting on "the session" cannot quietly
// pass by looking at the first of several.
func onlyRow(c *consoleWorld) (console.Row, error) {
	if c.registry == nil {
		return console.Row{}, fmt.Errorf("the console was not opened")
	}
	if len(c.rows) != 1 {
		return console.Row{}, fmt.Errorf("the console lists %d rows, want exactly 1", len(c.rows))
	}
	return c.rows[0], nil
}

// drillInto opens a resource, finds the named row, and descends into whatever that resource drills
// to, which is how the operator moves from a workspace to its projects and on to their sessions.
func (c *consoleWorld) drillInto(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, resource, name string) error {
	if err := c.open(ctx, client, resource); err != nil {
		return err
	}
	target, found := rowNamed(c.rows, name)
	if !found {
		return fmt.Errorf("the console does not list a %s row called %q", resource, name)
	}
	child, known := c.registry.Get(c.active.DrillTo)
	if !known {
		return fmt.Errorf("%s drills into %q, which is not registered", resource, c.active.DrillTo)
	}
	c.active = child
	return c.list(ctx, target.ID)
}

func expectRows(c *consoleWorld, resource string, want int) error {
	if c.registry == nil {
		return fmt.Errorf("the console was not opened")
	}
	if c.active.Name != resource {
		return fmt.Errorf("the console is showing %q, not %q", c.active.Name, resource)
	}
	if len(c.rows) != want {
		return fmt.Errorf("the console lists %d %s, want %d", len(c.rows), resource, want)
	}
	return nil
}

func rowNamed(rows []console.Row, name string) (console.Row, bool) {
	for _, row := range rows {
		for _, cell := range row.Cells {
			if strings.EqualFold(cell, name) {
				return row, true
			}
		}
	}
	return console.Row{}, false
}
