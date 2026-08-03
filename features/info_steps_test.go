package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/cucumber/godog"
	"google.golang.org/protobuf/encoding/prototext"
)

// infoWorld is what the control plane last said about itself.
type infoWorld struct {
	reported *quaycrewv1.GetInfoResponse
	err      error
}

type infoKey struct{}

func infoFrom(ctx context.Context) *infoWorld {
	i, _ := ctx.Value(infoKey{}).(*infoWorld)
	return i
}

// initializeInfoSteps registers the steps for what the control plane says it is running.
func initializeInfoSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, infoKey{}, &infoWorld{}), nil
	})

	sc.Step(`^a control plane that keeps session state outside the container$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		w.info.State = "host directory /tmp/quaycrew"
		// The value is read when the server is built, so stand a new one up over the same store.
		return w.restart()
	})

	sc.Step(`^the operator asks what the control plane is running$`, func(ctx context.Context) error {
		w, i := worldFrom(ctx), infoFrom(ctx)
		i.reported, i.err = w.client.GetInfo(ctx, &quaycrewv1.GetInfoRequest{})
		return nil
	})

	sc.Step(`^it reports the model "([^"]*)", the sandbox "([^"]*)" and the store "([^"]*)"$`,
		func(ctx context.Context, model, sandbox, store string) error {
			i := infoFrom(ctx)
			if i.err != nil {
				return i.err
			}
			if got := i.reported.GetModel(); got != model {
				return fmt.Errorf("it reports the model %q, want %q", got, model)
			}
			if got := i.reported.GetSandbox(); got != sandbox {
				return fmt.Errorf("it reports the sandbox %q, want %q", got, sandbox)
			}
			if got := i.reported.GetStore(); got != store {
				return fmt.Errorf("it reports the store %q, want %q", got, store)
			}
			return nil
		})

	sc.Step(`^it says a session's state is (not )?kept outside its container$`, func(ctx context.Context, negation string) error {
		i := infoFrom(ctx)
		if i.err != nil {
			return i.err
		}
		kept := i.reported.GetState() != ""
		if want := negation == ""; kept != want {
			return fmt.Errorf("it reports the state %q, want it %skept outside the container",
				i.reported.GetState(), negation)
		}
		return nil
	})

	sc.Step(`^the answer carries nothing from the secrets backend$`, func(ctx context.Context) error {
		i := infoFrom(ctx)
		if i.err != nil {
			return i.err
		}
		// Read the whole message rather than the fields this test happens to know about, so a field
		// added later cannot quietly start carrying a value.
		rendered := prototext.Format(i.reported)
		if strings.Contains(rendered, "tok-xyz") {
			return fmt.Errorf("the answer carries the token: %s", rendered)
		}
		return nil
	})
}
