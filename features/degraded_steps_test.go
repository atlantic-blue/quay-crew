package features_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/cucumber/godog"
)

// Steps for what the command line tool says about a system that is reporting itself not serving.
//
// The tool runs as its own process against a system on a real address, so a scenario reads the two
// streams a caller reads. That is the whole finding: the system knew, and nothing the operator typed
// said so.

// notServing is the words the line carries. Written here as well as in the tool, so a change to
// either has to be a change to both.
const notServing = "this system is not serving"

func initializeDegradedSteps(sc *godog.ScenarioContext) {
	sc.Step(`^standard error says this system is not serving and names "([^"]*)"$`,
		func(ctx context.Context, part string) error {
			said := toolFrom(ctx).stderr
			if err := says("standard error", said, notServing); err != nil {
				return err
			}
			return says("standard error", said, part)
		})

	// A verdict with no reason is what the container health check already had, and nobody could act
	// on it.
	sc.Step(`^standard error says which write did not land$`, func(ctx context.Context) error {
		return says("standard error", toolFrom(ctx).stderr, "did not take a record")
	})

	sc.Step(`^standard output carries the answer and nothing about the system being down$`,
		func(ctx context.Context) error {
			w, t := worldFrom(ctx), toolFrom(ctx)
			if err := says("standard output", t.stdout, w.workspaceName); err != nil {
				return err
			}
			if strings.Contains(t.stdout, notServing) {
				return fmt.Errorf("standard output carries the warning, which belongs on standard error: %q",
					t.stdout)
			}
			return nil
		})

	sc.Step(`^standard error says nothing about the system being down$`, func(ctx context.Context) error {
		if got := toolFrom(ctx).stderr; strings.Contains(got, notServing) {
			return fmt.Errorf("standard error calls a system nothing has probed not serving: %q", got)
		}
		return nil
	})
}
