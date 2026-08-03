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
	registry *console.Registry
	active   console.Resource
	rows     []console.Row
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

	sc.Step(`^the operator dispatches "([^"]*)" to the second project$`, func(ctx context.Context, text string) error {
		w := worldFrom(ctx)
		if w.secondProjectID == "" {
			return fmt.Errorf("no second project was created")
		}
		return w.dispatch(ctx, w.secondProjectID, "", text)
	})

	sc.Step(`^the operator opens the console$`, func(ctx context.Context) error {
		return consoleFrom(ctx).open(ctx, worldFrom(ctx).client, console.Default)
	})

	sc.Step(`^the operator opens the console on projects$`, func(ctx context.Context) error {
		return consoleFrom(ctx).open(ctx, worldFrom(ctx).client, "projects")
	})

	sc.Step(`^the operator drills into project "([^"]*)"$`, func(ctx context.Context, name string) error {
		c := consoleFrom(ctx)
		if err := c.open(ctx, worldFrom(ctx).client, "projects"); err != nil {
			return err
		}
		target, found := rowNamed(c.rows, name)
		if !found {
			return fmt.Errorf("the console does not list a project called %q", name)
		}
		child, known := c.registry.Get(c.active.DrillTo)
		if !known {
			return fmt.Errorf("projects drills into %q, which is not registered", c.active.DrillTo)
		}
		c.active = child
		return c.list(ctx, target.ID)
	})

	sc.Step(`^the console lists (\d+) sessions?$`, func(ctx context.Context, want int) error {
		return expectRows(consoleFrom(ctx), "sessions", want)
	})

	sc.Step(`^the console lists (\d+) projects?$`, func(ctx context.Context, want int) error {
		return expectRows(consoleFrom(ctx), "projects", want)
	})

	sc.Step(`^the console can drill from projects into sessions$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if c.active.DrillTo != "sessions" {
			return fmt.Errorf("projects drills into %q, want sessions", c.active.DrillTo)
		}
		if _, found := c.registry.Get("sessions"); !found {
			return fmt.Errorf("sessions is not a registered resource, so the drill would dead end")
		}
		return nil
	})
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
