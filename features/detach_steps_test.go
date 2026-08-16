package features_test

import (
	"context"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// Steps for the scenarios about a turn running while the operator carries on. They hold a turn open
// rather than timing one, because the thing being specified is what is true *during* a turn, and a
// scenario that waits a duration for that is a scenario that passes on a slow machine by accident.
func initializeDetachSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the model takes longer over a turn than anybody will wait$`, func(ctx context.Context) error {
		worldFrom(ctx).release = worldFrom(ctx).runner.hold()
		return nil
	})

	sc.Step(`^a turn is under way$`, func(ctx context.Context) error {
		return worldFrom(ctx).runner.waitForTurn()
	})

	// Dispatched the way the console dispatches: the caller gets the thread back straight away and the
	// task runs behind it. A scenario about what the operator does *while* a task runs needs this,
	// because the waited dispatch does not return until the task has landed.
	sc.Step(`^a task dispatched without waiting for it$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		resp, err := w.client.Dispatch(ctx, &quaycrewv1.DispatchRequest{
			Project: w.projectID, Text: "read the repository", Detach: true,
		})
		w.lastErr = err
		if err != nil {
			return err
		}
		w.turns = append(w.turns, turn{sessionID: resp.GetId(), threadID: resp.GetHandle()})
		return nil
	})

	sc.Step(`^the model finishes the turn$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.release == nil {
			return fmt.Errorf("no turn was being held, so there is nothing to finish")
		}
		w.release()
		w.release = nil
		return w.settled(ctx)
	})

	// Read off the crew rather than off the console, because what matters is what the thread holds.
	sc.Step(`^the crew's one thread is reported as (\w+)$`, func(ctx context.Context, want string) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListThreads(ctx, &quaycrewv1.ListThreadsRequest{})
		if err != nil {
			return err
		}
		if len(listed.GetThreads()) != 1 {
			return fmt.Errorf("the crew has %d threads, so there is no single one to ask about",
				len(listed.GetThreads()))
		}
		if got := listed.GetThreads()[0].GetStatus(); got != want {
			return fmt.Errorf("the thread is reported as %q, want %q", got, want)
		}
		return nil
	})

	sc.Step(`^the thread carries what the model said$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListThreads(ctx, &quaycrewv1.ListThreadsRequest{})
		if err != nil {
			return err
		}
		if len(listed.GetThreads()) != 1 {
			return fmt.Errorf("the crew has %d threads, want exactly 1", len(listed.GetThreads()))
		}
		turns, err := w.client.ListTurns(ctx, &quaycrewv1.ListTurnsRequest{
			Thread: listed.GetThreads()[0].GetId(),
		})
		if err != nil {
			return err
		}
		if len(turns.GetTurns()) == 0 {
			return fmt.Errorf("the thread has no turns, so the detached one was lost")
		}
		last := turns.GetTurns()[len(turns.GetTurns())-1]
		if last.GetReply() == "" {
			return fmt.Errorf("the recorded turn carries no reply: %+v", last)
		}
		return nil
	})
}
