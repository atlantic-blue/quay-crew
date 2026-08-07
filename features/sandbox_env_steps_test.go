package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// The crew's own address, handed to a session so `quay` inside a sandbox needs no arguments. It is an
// address rather than a credential, and reaching it also needs a network that can, which is the same
// decision made once in configuration.
func initializeReachableSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a crew that sessions can reach at "([^"]*)"$`, func(ctx context.Context, at string) error {
		w := worldFrom(ctx)
		w.reachable = at
		return w.restart()
	})

	sc.Step(`^the sandbox carries the address of the crew$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		env, err := onlySandboxEnv(w)
		if err != nil {
			return err
		}
		if got := env["QC_GRPC_ADDR"]; got != w.reachable {
			return fmt.Errorf("the sandbox was told the crew is at %q, want %q", got, w.reachable)
		}
		return nil
	})

	sc.Step(`^the sandbox carries no address at all$`, func(ctx context.Context) error {
		env, err := onlySandboxEnv(worldFrom(ctx))
		if err != nil {
			return err
		}
		if got, set := env["QC_GRPC_ADDR"]; set {
			return fmt.Errorf("the sandbox was told the crew is at %q, and it was not meant to be reachable", got)
		}
		return nil
	})

	// Saying nothing would pass the check above too, so the session still has to have been given the
	// things it is meant to have.
	sc.Step(`^the sandbox carries no address it was not given$`, func(ctx context.Context) error {
		env, err := onlySandboxEnv(worldFrom(ctx))
		if err != nil {
			return err
		}
		for key, value := range env {
			if key == "QC_GRPC_ADDR" {
				continue
			}
			if strings.Contains(value, "://") || strings.Contains(value, ":50051") {
				return fmt.Errorf("the sandbox carries %s=%q, which is an address nobody asked for", key, value)
			}
		}
		return nil
	})
}

// onlySandboxEnv is the environment of the one sandbox the scenario made, as a map.
func onlySandboxEnv(w *world) (map[string]string, error) {
	if len(w.provider.Created) != 1 {
		return nil, fmt.Errorf("%d sandboxes were made, want exactly 1", len(w.provider.Created))
	}
	env := make(map[string]string, len(w.provider.Created[0].Env))
	for _, entry := range w.provider.Created[0].Env {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		env[key] = value
	}
	return env, nil
}

// The driver: the one session that drives the crew rather than doing work inside it.
func initializeDriverSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the operator opens the driver$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		opened, err := w.client.OpenDriver(ctx, &quaycrewv1.OpenDriverRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		w.drivers = append(w.drivers, opened.GetSession())
		return nil
	})

	sc.Step(`^the operator opens the driver again$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		opened, err := w.client.OpenDriver(ctx, &quaycrewv1.OpenDriverRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		w.drivers = append(w.drivers, opened.GetSession())
		return nil
	})

	sc.Step(`^the driver is sent "([^"]*)"$`, func(ctx context.Context, text string) error {
		w := worldFrom(ctx)
		if len(w.drivers) == 0 {
			return fmt.Errorf("no driver was opened")
		}
		return w.dispatch(ctx, w.projectID, w.drivers[0].GetThreadId(), text)
	})

	sc.Step(`^it is the same driver both times$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.drivers) != 2 {
			return fmt.Errorf("the driver was opened %d times, want 2", len(w.drivers))
		}
		if w.drivers[0].GetId() != w.drivers[1].GetId() {
			return fmt.Errorf("opening the crew twice gave two drivers, %s and %s",
				w.drivers[0].GetId(), w.drivers[1].GetId())
		}
		if !w.drivers[0].GetDriver() {
			return fmt.Errorf("the session opened is not marked as the driver")
		}
		return nil
	})

	// One per project. Two would each think they were the one, and the second would be reached by
	// nobody.
	sc.Step(`^the crew has one driver$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		drivers := 0
		for _, session := range listed.GetSessions() {
			if session.GetDriver() {
				drivers++
			}
		}
		if drivers != 1 {
			return fmt.Errorf("the project has %d drivers, want 1", drivers)
		}
		return nil
	})
}
