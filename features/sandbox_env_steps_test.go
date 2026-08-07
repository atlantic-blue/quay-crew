package features_test

import (
	"context"
	"fmt"
	"strings"

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
