package features_test

import (
	"context"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// Steps for the scenarios about an exec running while the operator carries on. They hold an exec open
// rather than timing one, because the thing being specified is what is true *during* an exec, and a
// scenario that waits a duration for that is a scenario that passes on a slow machine by accident.
func initializeDetachSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the model takes longer over an exec than anybody will wait$`, func(ctx context.Context) error {
		worldFrom(ctx).release = worldFrom(ctx).runner.hold()
		return nil
	})

	sc.Step(`^an exec is under way$`, func(ctx context.Context) error {
		return worldFrom(ctx).runner.waitForExec()
	})

	// Dispatched the way the console dispatches: the caller gets the session back straight away and the
	// exec runs behind it. A scenario about what the operator does *while* an exec runs needs this,
	// because the waited dispatch does not return until the exec has landed.
	sc.Step(`^an exec dispatched without waiting for it$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		resp, err := w.client.Dispatch(ctx, &quaycrewv1.DispatchRequest{
			Project: w.projectID, Text: "read the repository", Detach: true,
		})
		w.lastErr = err
		if err != nil {
			return err
		}
		w.execs = append(w.execs, dispatched{sessionID: resp.GetId(), handle: resp.GetHandle()})
		return nil
	})

	sc.Step(`^the model finishes the exec$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.release == nil {
			return fmt.Errorf("no exec was being held, so there is nothing to finish")
		}
		w.release()
		w.release = nil
		return w.settled(ctx)
	})

	// Read off the system rather than off the console, because what matters is what the session holds.
	sc.Step(`^the system's one session is reported as (\w+)$`, func(ctx context.Context, want string) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
		if err != nil {
			return err
		}
		if len(listed.GetSessions()) != 1 {
			return fmt.Errorf("the system has %d sessions, so there is no single one to ask about",
				len(listed.GetSessions()))
		}
		if got := listed.GetSessions()[0].GetStatus(); got != want {
			return fmt.Errorf("the session is reported as %q, want %q", got, want)
		}
		return nil
	})

	sc.Step(`^the session carries what the model said$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
		if err != nil {
			return err
		}
		if len(listed.GetSessions()) != 1 {
			return fmt.Errorf("the system has %d sessions, want exactly 1", len(listed.GetSessions()))
		}
		execs, err := w.client.ListExecs(ctx, &quaycrewv1.ListExecsRequest{
			Session: listed.GetSessions()[0].GetId(),
		})
		if err != nil {
			return err
		}
		if len(execs.GetExecs()) == 0 {
			return fmt.Errorf("the session has no execs, so the detached one was lost")
		}
		last := execs.GetExecs()[len(execs.GetExecs())-1]
		if last.GetReply() == "" {
			return fmt.Errorf("the recorded exec carries no reply: %+v", last)
		}
		return nil
	})
}
