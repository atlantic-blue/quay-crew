package features_test

import (
	"context"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// Steps for the scenarios about a task running while the operator carries on. They hold a task open
// rather than timing one, because the thing being specified is what is true *during* a task, and a
// scenario that waits a duration for that is a scenario that passes on a slow machine by accident.
func initializeDetachSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the model takes longer over a task than anybody will wait$`, func(ctx context.Context) error {
		worldFrom(ctx).release = worldFrom(ctx).runner.hold()
		return nil
	})

	sc.Step(`^a task is under way$`, func(ctx context.Context) error {
		return worldFrom(ctx).runner.waitForTask()
	})

	// Dispatched the way the console dispatches: the caller gets the session back straight away and the
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
		w.tasks = append(w.tasks, task{sessionID: resp.GetId(), handle: resp.GetHandle()})
		return nil
	})

	sc.Step(`^the model finishes the task$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.release == nil {
			return fmt.Errorf("no task was being held, so there is nothing to finish")
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
		tasks, err := w.client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{
			Session: listed.GetSessions()[0].GetId(),
		})
		if err != nil {
			return err
		}
		if len(tasks.GetTasks()) == 0 {
			return fmt.Errorf("the session has no tasks, so the detached one was lost")
		}
		last := tasks.GetTasks()[len(tasks.GetTasks())-1]
		if last.GetReply() == "" {
			return fmt.Errorf("the recorded task carries no reply: %+v", last)
		}
		return nil
	})
}
