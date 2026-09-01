package features_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/atlantic-blue/quay-krewe/internal/flow"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/cucumber/godog"
)

// The trigger scenarios drive the system the way the system drives itself. Nothing here starts a run:
// something writes a row, the poller reads it on its tick, and the run appears. That is the whole
// point, so a scenario that started the run itself would prove nothing.

func initializeTriggerSteps(sc *godog.ScenarioContext) {
	// The in process source, which is all there is today. A caller inside this process writes the
	// row. There is no ingress, so nothing outside the system can do this yet.
	sc.Step(`^something happens and raises a trigger of "([^"]*)" carrying "([^"]*)" as "([^"]*)"$`,
		func(ctx context.Context, graph, key, value string) error {
			w := worldFrom(ctx)
			engine := flow.NewEngine(w.store, planeClient{client: w.client}, nil, w.server)
			raised, err := engine.Raise(ctx, flow.Trigger{
				GraphName: graph, Workspace: w.workspaceID, Project: w.projectID,
				Payload: map[string]string{key: value},
				Source:  "a caller inside the system",
			})
			if err != nil {
				return err
			}
			w.trigger = raised
			return nil
		})

	// One pass of both loops, then the run the trigger started becomes the run this scenario is
	// about, which is what a person watching would do: read the row, follow it to the run.
	sc.Step(`^the system ticks$`, func(ctx context.Context) error {
		if err := driveTheSystem(ctx); err != nil {
			return err
		}
		return followTheTrigger(ctx)
	})

	// Two pollers over one store, ticking together, which is the system running two control planes.
	sc.Step(`^two pollers tick at once$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
		var together sync.WaitGroup
		for _, owner := range []string{"poller-one", "poller-two"} {
			together.Add(1)
			go func() {
				defer together.Done()
				engine := flow.NewEngine(w.store, planeClient{client: w.client}, nil, w.server)
				flow.NewPoller(engine, 0, quiet).Owned(owner).Tick(ctx)
			}()
		}
		together.Wait()
		if err := driveTheSystem(ctx); err != nil {
			return err
		}
		return followTheTrigger(ctx)
	})

	sc.Step(`^the trigger reads back as started, naming the run$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		acted, err := w.store.GetTrigger(ctx, w.trigger.ID)
		if err != nil {
			return err
		}
		if acted.Status != flow.TriggerStarted {
			return fmt.Errorf("the trigger reads %q saying %q, want it started", acted.Status, acted.Reason)
		}
		if acted.Run != w.flowRun.ID {
			return fmt.Errorf("the trigger names run %q and the run is %q", acted.Run, w.flowRun.ID)
		}
		return nil
	})

	sc.Step(`^the trigger reads back as failed, saying "([^"]*)"$`, func(ctx context.Context, carries string) error {
		w := worldFrom(ctx)
		acted, err := w.store.GetTrigger(ctx, w.trigger.ID)
		if err != nil {
			return err
		}
		if acted.Status != flow.TriggerFailed {
			return fmt.Errorf("the trigger reads %q, want it failed rather than left as though nobody had got to it", acted.Status)
		}
		if !strings.Contains(acted.Reason, carries) {
			return fmt.Errorf("the row says %q, want it to carry %q", acted.Reason, carries)
		}
		return nil
	})

	sc.Step(`^the run's own job is labelled with the trigger that caused it$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		carrying, err := runCarrier(ctx, w)
		if err != nil {
			return err
		}
		if carrying.Labels["flow.trigger"] != w.trigger.ID {
			return fmt.Errorf("the run's own job is labelled %v, want it to name trigger %s",
				carrying.Labels, w.trigger.ID)
		}
		return nil
	})

	sc.Step(`^the run's steps hang under the run's own job$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		carrying, err := runCarrier(ctx, w)
		if err != nil {
			return err
		}
		steps, err := w.store.ListJobs(ctx, job.Filter{LabelKey: "flow.run", LabelValue: w.flowRun.ID})
		if err != nil {
			return err
		}
		found := 0
		for _, step := range steps {
			if step.ID == carrying.ID {
				continue
			}
			found++
			if step.Parent != carrying.ID {
				return fmt.Errorf("step %q hangs under %q, want the run's own job %q",
					step.Title, step.Parent, carrying.ID)
			}
			if step.Depth != carrying.Depth+1 {
				return fmt.Errorf("step %q is at depth %d and the run at %d, want one deeper",
					step.Title, step.Depth, carrying.Depth)
			}
		}
		if found == 0 {
			return fmt.Errorf("the run has no steps, so nothing hangs under it")
		}
		return nil
	})

	sc.Step(`^the run's steps were asked "([^"]*)"$`, func(ctx context.Context, prompt string) error {
		tasks, err := flowRunTasks(ctx, worldFrom(ctx))
		if err != nil {
			return err
		}
		if len(tasks) != 1 {
			return fmt.Errorf("the run's steps hold %d tasks, want 1", len(tasks))
		}
		if !strings.Contains(tasks[0].GetPrompt(), prompt) {
			return fmt.Errorf("the step was asked %q, want %q", tasks[0].GetPrompt(), prompt)
		}
		return nil
	})

	sc.Step(`^exactly (\d+) run of "([^"]*)" has started$`, func(ctx context.Context, want int, graph string) error {
		runs, err := flowRunsOf(ctx, graph)
		if err != nil {
			return err
		}
		if len(runs) != want {
			return fmt.Errorf("%d runs of %s exist, want %d", len(runs), graph, want)
		}
		return nil
	})
}

// followTheTrigger makes the run the last trigger started the run this scenario reads, so the steps
// every other flow scenario uses read the run nobody started. A trigger that started nothing leaves
// the scenario with no run, which is what the scenarios about refusals then assert on.
func followTheTrigger(ctx context.Context) error {
	w := worldFrom(ctx)
	if w.trigger.ID == "" {
		return nil
	}
	acted, err := w.store.GetTrigger(ctx, w.trigger.ID)
	if err != nil {
		return err
	}
	if acted.Run == "" {
		return nil
	}
	run, err := w.store.GetFlowRun(ctx, acted.Run)
	if err != nil {
		return err
	}
	w.flowRun = *run
	return nil
}
