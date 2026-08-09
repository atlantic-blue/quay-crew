package features_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/flow"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/cucumber/godog"
)

// The flow scenarios drive the engine the way the running crew will: against the store and against
// the control plane's real authenticated interface, so a dispatch here is a turn through the same
// road every other caller takes.

// planeClient adapts the gRPC client to the two calls the engine is allowed.
type planeClient struct {
	client quaycrewv1.ControlPlaneServiceClient
}

func (p planeClient) Dispatch(ctx context.Context, req *quaycrewv1.DispatchRequest) (*quaycrewv1.DispatchResponse, error) {
	return p.client.Dispatch(ctx, req)
}

func (p planeClient) ArchiveThread(ctx context.Context, req *quaycrewv1.ArchiveThreadRequest) (*quaycrewv1.ArchiveThreadResponse, error) {
	return p.client.ArchiveThread(ctx, req)
}

func initializeFlowSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the crew holds this flow graph:$`, func(ctx context.Context, definition *godog.DocString) error {
		w := worldFrom(ctx)
		graph, err := flow.Parse([]byte(definition.Content))
		if err != nil {
			return err
		}
		return w.store.ImportFlowGraph(ctx, graph.Name, graph.Version, definition.Content)
	})

	sc.Step(`^the operator imports this flow graph, which is refused:$`, func(ctx context.Context, definition *godog.DocString) error {
		w := worldFrom(ctx)
		_, err := flow.Parse([]byte(definition.Content))
		if err == nil {
			return fmt.Errorf("the graph parsed, and nothing was refused")
		}
		w.lastErr = err
		return nil
	})

	sc.Step(`^the refusal names the node nobody declared$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("nothing was refused")
		}
		if !strings.Contains(w.lastErr.Error(), "nowhere") {
			return fmt.Errorf("the refusal says %q, want it to name the undeclared node", w.lastErr)
		}
		return nil
	})

	sc.Step(`^the operator starts the flow "([^"]*)" in the project$`, func(ctx context.Context, name string) error {
		w := worldFrom(ctx)
		engine := flow.NewEngine(w.store, planeClient{client: w.client}, nil)
		run, err := engine.Start(ctx, name, w.workspaceID, w.projectID, nil)
		w.flowRun, w.lastErr = run, err
		return nil
	})

	sc.Step(`^the flow run is waiting$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr != nil {
			return fmt.Errorf("the run did not start: %v", w.lastErr)
		}
		kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
		if err != nil {
			return err
		}
		if kept.Status != flow.StatusWaiting {
			return fmt.Errorf("the run reads back as %q on node %q, want it waiting", kept.Status, kept.Node)
		}
		if kept.DueAt == nil {
			return fmt.Errorf("the run is waiting with no due time, so nothing would ever wake it")
		}
		return nil
	})

	// The clock is moved rather than slept through: a scenario that waited out ten real minutes
	// would be a scenario nobody runs.
	sc.Step(`^ten minutes pass and the crew looks for waits that are due$`, func(ctx context.Context) error {
		return tickFlowPoller(ctx, 11*time.Minute)
	})

	sc.Step(`^the crew looks for waits that are due$`, func(ctx context.Context) error {
		return tickFlowPoller(ctx, 0)
	})

	sc.Step(`^the flow run is asking "([^"]*)"$`, func(ctx context.Context, question string) error {
		w := worldFrom(ctx)
		if w.lastErr != nil {
			return fmt.Errorf("the run did not start: %v", w.lastErr)
		}
		kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
		if err != nil {
			return err
		}
		if kept.Status != flow.StatusAsking {
			return fmt.Errorf("the run reads back as %q on node %q, want it asking", kept.Status, kept.Node)
		}
		if kept.Question != question {
			return fmt.Errorf("the run asks %q, want %q", kept.Question, question)
		}
		return nil
	})

	sc.Step(`^the operator answers the run with "([^"]*)"$`, func(ctx context.Context, answer string) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.AnswerFlowRun(ctx, &quaycrewv1.AnswerFlowRunRequest{
			Id: w.flowRun.ID, Answer: answer,
		})
		return w.lastErr
	})

	sc.Step(`^the operator schedules "([^"]*)" in the project$`, func(ctx context.Context, graph string) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.ScheduleFlow(ctx, &quaycrewv1.ScheduleFlowRequest{
			Graph: graph, Project: w.projectID,
		})
		return nil
	})

	sc.Step(`^the operator unschedules "([^"]*)" in the project$`, func(ctx context.Context, graph string) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.UnscheduleFlow(ctx, &quaycrewv1.UnscheduleFlowRequest{
			Graph: graph, Project: w.projectID,
		})
		return w.lastErr
	})

	sc.Step(`^a day passes and the crew looks for waits that are due$`, func(ctx context.Context) error {
		return tickFlowPoller(ctx, 25*time.Hour)
	})

	sc.Step(`^no run of "([^"]*)" has started$`, func(ctx context.Context, graph string) error {
		runs, err := flowRunsOf(ctx, graph)
		if err != nil {
			return err
		}
		if len(runs) != 0 {
			return fmt.Errorf("%d runs of %s exist, want none", len(runs), graph)
		}
		return nil
	})

	// The schedule starts the run and the run advances behind that, the same way a person starting
	// one gets its identifier immediately, so this waits for it rather than reading a race.
	sc.Step(`^a run of "([^"]*)" has started and finished$`, func(ctx context.Context, graph string) error {
		deadline := time.Now().Add(10 * time.Second)
		var last string
		for time.Now().Before(deadline) {
			runs, err := flowRunsOf(ctx, graph)
			if err != nil {
				return err
			}
			if len(runs) > 1 {
				return fmt.Errorf("%d runs of %s exist, want the one the schedule started", len(runs), graph)
			}
			if len(runs) == 1 {
				last = runs[0].GetStatus()
				if last == flow.StatusDone {
					return nil
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		return fmt.Errorf("the scheduled run of %s is %q after ten seconds, want done", graph, last)
	})

	sc.Step(`^the flow run is done$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr != nil {
			return fmt.Errorf("the run did not finish: %v", w.lastErr)
		}
		kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
		if err != nil {
			return err
		}
		if kept.Status != flow.StatusDone {
			return fmt.Errorf("the run reads back as %q on node %q, want done", kept.Status, kept.Node)
		}
		return nil
	})

	sc.Step(`^the flow run is pinned to version (\d+)$`, func(ctx context.Context, version int) error {
		w := worldFrom(ctx)
		if w.lastErr != nil {
			return fmt.Errorf("the run did not finish: %v", w.lastErr)
		}
		kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
		if err != nil {
			return err
		}
		if kept.GraphVersion != version {
			return fmt.Errorf("the run is pinned to version %d, want %d", kept.GraphVersion, version)
		}
		return nil
	})

	sc.Step(`^the run's thread was asked "([^"]*)" and then "([^"]*)"$`, func(ctx context.Context, first, second string) error {
		turns, err := flowRunTurns(ctx, worldFrom(ctx))
		if err != nil {
			return err
		}
		if len(turns) != 2 {
			return fmt.Errorf("the run's thread holds %d turns, want 2", len(turns))
		}
		if turns[0].GetPrompt() != first || turns[1].GetPrompt() != second {
			return fmt.Errorf("the thread was asked %q then %q, want %q then %q",
				turns[0].GetPrompt(), turns[1].GetPrompt(), first, second)
		}
		return nil
	})

	sc.Step(`^the run's thread was asked (\d+) turns?$`, func(ctx context.Context, want int) error {
		turns, err := flowRunTurns(ctx, worldFrom(ctx))
		if err != nil {
			return err
		}
		if len(turns) != want {
			return fmt.Errorf("the run's thread holds %d turns, want %d", len(turns), want)
		}
		return nil
	})

	sc.Step(`^the run's thread is archived$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		thread, err := flowRunThread(ctx, w, true)
		if err != nil {
			return err
		}
		if thread.GetArchivedAt() == nil {
			return fmt.Errorf("the run's thread is not archived, so a finished run left a container behind")
		}
		return nil
	})

	sc.Step(`^the run's transitions read back as "([^"]*)", "([^"]*)", "([^"]*)"$`, func(ctx context.Context, first, second, third string) error {
		w := worldFrom(ctx)
		transitions, err := w.store.ListFlowTransitions(ctx, w.flowRun.ID)
		if err != nil {
			return err
		}
		nodes := make([]string, 0, len(transitions))
		for _, one := range transitions {
			nodes = append(nodes, one.Node)
		}
		want := []string{first, second, third}
		if len(nodes) != len(want) {
			return fmt.Errorf("the run recorded %d movements (%s), want %d", len(nodes), strings.Join(nodes, ", "), len(want))
		}
		for at := range want {
			if nodes[at] != want[at] {
				return fmt.Errorf("movement %d landed on %q, want %q", at+1, nodes[at], want[at])
			}
		}
		return nil
	})

	sc.Step(`^starting it is refused as not found$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("starting the flow was accepted")
		}
		if !errors.Is(w.lastErr, store.ErrNotFound) {
			return fmt.Errorf("the refusal is %v, want not found", w.lastErr)
		}
		return nil
	})
}

// tickFlowPoller moves the clock forward and runs one tick, which is what the crew's own poller
// does every few seconds. Driven directly rather than waited for: a scenario that slept would be
// slow when it passed and flaky when it did not.
func tickFlowPoller(ctx context.Context, forward time.Duration) error {
	w := worldFrom(ctx)
	at := time.Now().UTC().Add(forward)
	engine := flow.NewEngine(w.store, planeClient{client: w.client}, nil).
		WithClock(func() time.Time { return at })
	flow.NewPoller(engine, 0, slog.New(slog.NewTextHandler(io.Discard, nil))).Tick(ctx)
	return nil
}

// flowRunsOf is every run of one graph, whoever or whatever started it.
func flowRunsOf(ctx context.Context, graph string) ([]*quaycrewv1.FlowRun, error) {
	listed, err := worldFrom(ctx).client.ListFlowRuns(ctx, &quaycrewv1.ListFlowRunsRequest{})
	if err != nil {
		return nil, err
	}
	out := make([]*quaycrewv1.FlowRun, 0)
	for _, run := range listed.GetRuns() {
		if run.GetGraphName() == graph {
			out = append(out, run)
		}
	}
	return out, nil
}

// flowRunThread finds the run's own thread by its handle, among the live or the archived.
func flowRunThread(ctx context.Context, w *world, archived bool) (*quaycrewv1.Thread, error) {
	listed, err := w.client.ListThreads(ctx, &quaycrewv1.ListThreadsRequest{Archived: archived})
	if err != nil {
		return nil, err
	}
	for _, thread := range listed.GetThreads() {
		if thread.GetHandle() == w.flowRun.ThreadHandle() {
			return thread, nil
		}
	}
	return nil, fmt.Errorf("no thread carries the run's handle %q (archived=%t)", w.flowRun.ThreadHandle(), archived)
}

// flowRunTurns is the history of the run's own thread, wherever the thread now lives.
func flowRunTurns(ctx context.Context, w *world) ([]*quaycrewv1.Turn, error) {
	thread, err := flowRunThread(ctx, w, true)
	if err != nil {
		thread, err = flowRunThread(ctx, w, false)
		if err != nil {
			return nil, err
		}
	}
	resp, err := w.client.ListTurns(ctx, &quaycrewv1.ListTurnsRequest{Thread: thread.GetId()})
	if err != nil {
		return nil, err
	}
	return resp.GetTurns(), nil
}
