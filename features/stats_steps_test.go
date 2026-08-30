package features_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/cucumber/godog"
)

// initializeStatsSteps registers the steps for what the stats view says about each part of the system.
// Its own file, so the health scenarios' reach into the console does not widen the console's steps.
func initializeStatsSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the system probes itself$`, func(ctx context.Context) error {
		worldFrom(ctx).server.ProbeHealth(ctx)
		return nil
	})

	sc.Step(`^the operator opens the console on stats$`, func(ctx context.Context) error {
		return consoleFrom(ctx).open(ctx, worldFrom(ctx).client, "stats")
	})

	sc.Step(`^the stats view says the "([^"]*)" is "([^"]*)"$`, func(ctx context.Context, what, state string) error {
		for _, row := range consoleFrom(ctx).rows {
			if row.Cells[0] != what {
				continue
			}
			if row.Cells[1] != state {
				return fmt.Errorf("the %s row reads %q, want %q", what, row.Cells[1], state)
			}
			return nil
		}
		return fmt.Errorf("the stats view has no row for %q", what)
	})

	// And on the screen, because a row carrying the right word is only half of it: what settles
	// whether an operator sees a dead component is what the console draws.
	sc.Step(`^the operator reads "([^"]*)" as "([^"]*)" on the screen$`,
		func(ctx context.Context, what, state string) error {
			c, w := consoleFrom(ctx), worldFrom(ctx)
			if err := c.openModelOn(w, "stats"); err != nil {
				return err
			}
			for _, line := range strings.Split(c.model.View(), "\n") {
				if !strings.Contains(line, what) {
					continue
				}
				if !strings.Contains(stripColour(line), state) {
					return fmt.Errorf("the console draws %q, and it does not say %q", stripColour(line), state)
				}
				return nil
			}
			return fmt.Errorf("the console draws nothing about %q:\n%s", what, stripColour(c.model.View()))
		})
}

// stripColour takes the escape sequences off a drawn line, so a step reads the words rather than the
// bytes that colour them.
func stripColour(drawn string) string {
	var plain strings.Builder
	for at := 0; at < len(drawn); at++ {
		if drawn[at] != 0x1b {
			plain.WriteByte(drawn[at])
			continue
		}
		for at < len(drawn) && drawn[at] != 'm' {
			at++
		}
	}
	return plain.String()
}
