package features_test

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
		engine := flow.NewEngine(w.store, planeClient{client: w.client})
		run, err := engine.Start(ctx, name, w.workspaceID, w.projectID, nil)
		w.flowRun, w.lastErr = run, err
		return nil
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
