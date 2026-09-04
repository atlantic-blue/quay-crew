package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"github.com/cucumber/godog"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// scenarioWait is what a scenario about a budget running out gives the system. The measured budget is
// a minute, and a suite that waits a minute to watch one is a suite nobody runs.
const scenarioWait = 200 * time.Millisecond

// stallingStore reads the way the real one does and never takes a write, which is the shape of the
// system this is all about: every listing answered and nothing started.
type stallingStore struct {
	store.Store
}

func (s stallingStore) Probe(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

// Steps for the scenarios about a system that cannot start job: what it says instead of waiting, and
// what it answers when something asks whether it is well.
func initializeWaitsSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a sandbox that never starts$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		w.provider.Hold = make(chan struct{})
		w.startWait = scenarioWait
		return w.restart()
	})

	sc.Step(`^a store that never takes a write$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		w.storeStalls = true
		w.startWait = scenarioWait
		return w.restart()
	})

	sc.Step(`^the system says it waited for "([^"]*)"$`, func(ctx context.Context, what string) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("the dispatch came back with no error, so the system said nothing about waiting")
		}
		if !strings.Contains(w.lastErr.Error(), what) {
			return fmt.Errorf("the system said %q, and it does not name %q", w.lastErr.Error(), what)
		}
		return nil
	})

	sc.Step(`^the session left behind is not sitting idle$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
		if err != nil {
			return err
		}
		if len(listed.GetSessions()) != 1 {
			return fmt.Errorf("the system has %d sessions, want the one the dispatch made",
				len(listed.GetSessions()))
		}
		session := listed.GetSessions()[0]
		if session.GetStatus() != "failed" {
			return fmt.Errorf("the session reads %q, and a session that never started must not read idle",
				session.GetStatus())
		}
		execs, err := listExecs(ctx, w, session.GetId())
		if err != nil {
			return err
		}
		if len(execs) != 1 || execs[0].GetFailure() == "" {
			return fmt.Errorf("%d execs came back, and the operator has nothing to read about why", len(execs))
		}
		return nil
	})

	sc.Step(`^the system is asked whether it is serving$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		answer, err := w.health.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
		if err != nil {
			return err
		}
		w.lastHealth = answer.GetStatus()
		return nil
	})

	sc.Step(`^the system answers that it is serving$`, func(ctx context.Context) error {
		if got := worldFrom(ctx).lastHealth; got != grpc_health_v1.HealthCheckResponse_SERVING {
			return fmt.Errorf("the system answered %s, want SERVING", got)
		}
		return nil
	})

	sc.Step(`^the system answers that it is not serving$`, func(ctx context.Context) error {
		if got := worldFrom(ctx).lastHealth; got != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
			return fmt.Errorf("the system answered %s, want NOT_SERVING", got)
		}
		return nil
	})
}
