package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/cucumber/godog"
)

// Steps for putting the whole system down before something else takes its containers away.
func initializeDrainSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the operator drains the system$`, func(ctx context.Context) error {
		return drain(ctx, false)
	})

	sc.Step(`^the operator drains the system anyway$`, func(ctx context.Context) error {
		return drain(ctx, true)
	})

	sc.Step(`^the drain says it put down (\d+) sessions?$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		if got := len(w.lastDrain.GetStopped()); got != want {
			return fmt.Errorf("the drain put down %d sessions, want %d", got, want)
		}
		return nil
	})

	// A count is not an answer to "what did you just stop". The operator has to be able to find the
	// conversation again, and the handle is what they would type.
	sc.Step(`^the drain names the session it put down$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		for _, session := range w.lastDrain.GetStopped() {
			if session.GetId() == current.sessionID {
				if session.GetHandle() == "" {
					return fmt.Errorf("the drain named the session with nothing the operator could type")
				}
				return nil
			}
		}
		return fmt.Errorf("the drain does not name the session it stopped: %v", w.lastDrain.GetStopped())
	})

	sc.Step(`^the drain says the session was working when it went$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.lastDrain.GetWorking()) == 0 {
			return fmt.Errorf("the drain interrupted a task and says nothing about it")
		}
		return nil
	})

	sc.Step(`^the refusal names what is still working$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("the system drained over a task that was still working")
		}
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		// The handle as the operator reads it in a listing, which is what they need to go and wait
		// for the right conversation.
		if !strings.Contains(w.lastErr.Error(), display.ShortID(current.handle)) {
			return fmt.Errorf("the refusal says %q, which does not name the session that is working", w.lastErr)
		}
		return nil
	})

	sc.Step(`^no sandbox the system made is closed$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.provider.Boxes) == 0 {
			return fmt.Errorf("no sandbox was ever created, so this scenario is not testing anything")
		}
		for i, box := range w.provider.Boxes {
			if box.Closed {
				return fmt.Errorf("sandbox %d was closed by a drain that refused", i)
			}
		}
		return nil
	})
}

// drain asks the system to put every live session down, keeping both the answer and the refusal: a
// scenario about a refusal needs the error, and one about what went down needs the response.
func drain(ctx context.Context, force bool) error {
	w := worldFrom(ctx)
	resp, err := w.client.DrainSessions(ctx, &quaycrewv1.DrainSessionsRequest{Force: force})
	w.lastDrain = resp
	w.lastErr = err
	return nil
}
