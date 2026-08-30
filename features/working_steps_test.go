package features_test

import (
	"context"
	"fmt"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// Steps for the scenarios about seeing a task while it happens. The caller here waits, which is what
// a terminal does, so the dispatch runs behind the scenario: a step that called it directly would not
// come back until the task had landed, and the whole question is what is true before then.
func initializeWorkingSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a task dispatched with the caller waiting for it$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		w.waited = make(chan waitedDispatch, 1)
		go func(client quaycrewv1.ControlPlaneServiceClient, project string, answered chan<- waitedDispatch) {
			resp, err := client.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
				Project: project, Text: "read the repository",
			})
			answered <- waitedDispatch{resp: resp, err: err}
		}(w.client, w.projectID, w.waited)
		return nil
	})

	sc.Step(`^the operator's dispatch comes back$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.waited == nil {
			return fmt.Errorf("nobody dispatched anything, so there is nothing to come back")
		}
		select {
		case answered := <-w.waited:
			w.lastErr = answered.err
			if answered.err != nil {
				return answered.err
			}
			w.tasks = append(w.tasks, task{
				sessionID: answered.resp.GetId(),
				handle:    answered.resp.GetHandle(),
				reply:     answered.resp.GetReply(),
			})
			return nil
		case <-time.After(10 * time.Second):
			return fmt.Errorf("the dispatch never came back")
		}
	})

	sc.Step(`^the system's one session was asked "([^"]*)" and is still running$`,
		func(ctx context.Context, prompt string) error {
			w := worldFrom(ctx)
			session, err := theOneSession(ctx, w)
			if err != nil {
				return err
			}
			tasks, err := listTasks(ctx, w, session.GetId())
			if err != nil {
				return err
			}
			if len(tasks) != 1 {
				return fmt.Errorf("%d tasks are recorded while one runs, want 1", len(tasks))
			}
			if tasks[0].GetPrompt() != prompt {
				return fmt.Errorf("the recorded task says %q was asked, want %q", tasks[0].GetPrompt(), prompt)
			}
			if tasks[0].GetStatus() != "running" {
				return fmt.Errorf("the recorded task reads %q, want running", tasks[0].GetStatus())
			}
			return nil
		})

	sc.Step(`^the system's one session has (\d+) tasks?$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		session, err := theOneSession(ctx, w)
		if err != nil {
			return err
		}
		return sessionHasTasks(ctx, w, session.GetId(), want)
	})
}

// waitedDispatch is what a dispatch nobody is watching yet came back with.
type waitedDispatch struct {
	resp *quaycrewv1.DispatchResponse
	err  error
}

// theOneSession is the system's single session, for the scenarios that cannot name it: a waited
// dispatch has not come back yet, so nothing has told the scenario which session it made.
func theOneSession(ctx context.Context, w *world) (*quaycrewv1.Session, error) {
	listed, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
	if err != nil {
		return nil, err
	}
	if len(listed.GetSessions()) != 1 {
		return nil, fmt.Errorf("the system has %d sessions, so there is no single one to ask about",
			len(listed.GetSessions()))
	}
	return listed.GetSessions()[0], nil
}
