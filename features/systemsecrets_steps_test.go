package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/name"
	"github.com/cucumber/godog"
)

// A secret held by the system rather than by one workspace, which every workspace then reads.
func initializeSystemSecretSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the system has the secret "([^"]*)" set to "([^"]*)"$`,
		func(ctx context.Context, secret, value string) error {
			w := worldFrom(ctx)
			_, err := w.client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
				Scope: name.System, Key: secret, Value: value,
			})
			return err
		})

	sc.Step(`^the system mounts the secret "([^"]*)" holding "([^"]*)"$`,
		func(ctx context.Context, secret, contents string) error {
			w := worldFrom(ctx)
			_, err := w.client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
				Scope:      name.System,
				Key:        secret,
				Value:      contents,
				Projection: quaycrewv1.SecretProjection_SECRET_PROJECTION_FILE,
			})
			return err
		})

	sc.Step(`^the listing says the system holds "([^"]*)"$`, func(ctx context.Context, want string) error {
		return heldBy(worldFrom(ctx), want, true)
	})

	sc.Step(`^the listing says the workspace holds "([^"]*)"$`, func(ctx context.Context, want string) error {
		return heldBy(worldFrom(ctx), want, false)
	})

	sc.Step(`^the system refuses it, saying that word means the whole system$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("the system accepted it, and that workspace would take what every workspace reads")
		}
		// The reason, not only the refusal. An operator told no and not why types it again.
		if !strings.Contains(w.lastErr.Error(), "whole system") {
			return fmt.Errorf("the system refused it saying %q, which does not say why", w.lastErr)
		}
		return nil
	})
}

// heldBy checks the last listing says who holds a secret, which is the one thing about it a listing
// can say beyond its existence.
func heldBy(w *world, want string, system bool) error {
	for _, secret := range w.lastSecrets.GetSecrets() {
		if secret.GetName() != want {
			continue
		}
		if secret.GetSystem() != system {
			return fmt.Errorf("the listing says %s is held by the system: %t, want %t", want, secret.GetSystem(), system)
		}
		return nil
	}
	return fmt.Errorf("the listing does not name %q: %v", want, w.lastSecrets.GetSecrets())
}
