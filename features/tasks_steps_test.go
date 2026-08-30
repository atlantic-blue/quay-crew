package features_test

import (
	"context"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// initializeTasksSteps registers the steps for reading a session's history back.
func initializeTasksSteps(sc *godog.ScenarioContext) {

	sc.Step(`^the session has (\d+) tasks?$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		if len(w.tasks) == 0 {
			return fmt.Errorf("no task has been dispatched, so there is no session to ask about")
		}
		return sessionHasTasks(ctx, w, w.tasks[0].sessionID, want)
	})

	sc.Step(`^each session has (\d+) tasks?$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		seen := map[string]bool{}
		for _, task := range w.tasks {
			if seen[task.sessionID] {
				continue
			}
			seen[task.sessionID] = true
			if err := sessionHasTasks(ctx, w, task.sessionID, want); err != nil {
				return err
			}
		}
		if len(seen) < 2 {
			return fmt.Errorf("%d sessions were dispatched to, so this proves nothing about separation", len(seen))
		}
		return nil
	})

	sc.Step(`^the first task says "([^"]*)" and the second says "([^"]*)"$`,
		func(ctx context.Context, first, second string) error {
			w := worldFrom(ctx)
			tasks, err := listTasks(ctx, w, w.tasks[0].sessionID)
			if err != nil {
				return err
			}
			if len(tasks) < 2 {
				return fmt.Errorf("%d tasks came back, want at least 2", len(tasks))
			}
			if tasks[0].GetPrompt() != first {
				return fmt.Errorf("the first task says %q, want %q", tasks[0].GetPrompt(), first)
			}
			if tasks[1].GetPrompt() != second {
				return fmt.Errorf("the second task says %q, want %q", tasks[1].GetPrompt(), second)
			}
			if tasks[0].GetReply() != w.tasks[0].reply {
				return fmt.Errorf("the first task's reply is %q, want the one the operator got, %q",
					tasks[0].GetReply(), w.tasks[0].reply)
			}
			return nil
		})

	sc.Step(`^the one task on that session is recorded as failed$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		// A failed dispatch returns no session id, so the session is found through the listing.
		sessions, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Workspace: w.workspaceID})
		if err != nil {
			return err
		}
		if len(sessions.GetSessions()) != 1 {
			return fmt.Errorf("%d sessions exist, want exactly one", len(sessions.GetSessions()))
		}
		tasks, err := listTasks(ctx, w, sessions.GetSessions()[0].GetId())
		if err != nil {
			return err
		}
		if len(tasks) != 1 {
			return fmt.Errorf("%d tasks came back, want 1", len(tasks))
		}
		if tasks[0].GetStatus() != "failed" {
			return fmt.Errorf("the task is recorded as %q, want failed", tasks[0].GetStatus())
		}
		if tasks[0].GetFailure() == "" {
			return fmt.Errorf("the task does not say what went wrong")
		}
		return nil
	})

	sc.Step(`^the first task of the session says "([^"]*)" was asked and "([^"]*)" came back$`, func(ctx context.Context, prompt, reply string) error {
		w := worldFrom(ctx)
		if len(w.tasks) == 0 {
			return fmt.Errorf("no task has been dispatched")
		}
		tasks, err := listTasks(ctx, w, w.tasks[0].sessionID)
		if err != nil {
			return err
		}
		if len(tasks) == 0 {
			return fmt.Errorf("the session has no history")
		}
		if tasks[0].GetPrompt() != prompt || tasks[0].GetReply() != reply {
			return fmt.Errorf("the first task says %q and %q, want %q and %q",
				tasks[0].GetPrompt(), tasks[0].GetReply(), prompt, reply)
		}
		return nil
	})

	sc.Step(`^the operator asks for the history of a session that does not exist$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: "no-such-session"})
		return nil
	})
}

func listTasks(ctx context.Context, w *world, session string) ([]*quaycrewv1.Task, error) {
	resp, err := w.client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: session})
	if err != nil {
		return nil, err
	}
	return resp.GetTasks(), nil
}

func sessionHasTasks(ctx context.Context, w *world, session string, want int) error {
	tasks, err := listTasks(ctx, w, session)
	if err != nil {
		return err
	}
	if len(tasks) != want {
		return fmt.Errorf("session %s has %d tasks, want %d", session, len(tasks), want)
	}
	return nil
}
