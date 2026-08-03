package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/console"
	"github.com/cucumber/godog"
)

// consoleWorld is the console's own scenario state, kept beside the shared world rather than inside
// it so the console scenarios do not widen what every other scenario carries.
type consoleWorld struct {
	secondProject string
	registry      *console.Registry
	active        console.Resource
	rows          []console.Row
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
