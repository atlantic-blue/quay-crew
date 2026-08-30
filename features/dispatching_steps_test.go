package features_test

import (
	"context"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// Steps for the scenarios about a task the system keeps after the caller has gone. The caller's own
// context is cancelled here, which is what a closed terminal does to the call it was holding.
func initializeDispatchingSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a task dispatched by a caller that then goes away$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		calling, hangUp := context.WithCancel(ctx)
		resp, err := w.client.Dispatch(calling, &quaycrewv1.DispatchRequest{
			Project: w.projectID, Text: "read the repository", Detach: true,
		})
		w.lastErr = err
		if err != nil {
			hangUp()
			return err
		}
		w.tasks = append(w.tasks, task{sessionID: resp.GetId(), handle: resp.GetHandle()})
		// The caller is gone from here on, while the task it asked for is still in the model.
		hangUp()
		if resp.GetReply() != "" {
			return fmt.Errorf("a dispatch that lets go answered %q, and there is no answer yet", resp.GetReply())
		}
		return nil
	})
}
