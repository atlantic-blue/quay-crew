package features_test

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/flow"
	"github.com/cucumber/godog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// The operator surface for flows, driven through the same authenticated interface every other
// caller uses, so what these prove is that the engine is reachable at all: a flow engine nothing
// can reach delivers nothing.

// namedGraph is the graph the scenarios import, small enough to read and wide enough to dispatch,
// branch and end.
const namedGraph = `
name: fix-red
version: 1
mode: edits
nodes:
  fix:  { type: dispatch, prompt: "fix the build" }
  ok:   { type: choice, on: { result.failed: "false" } }
  push: { type: dispatch, prompt: "push the fix" }
edges:
  - [fix, ok]
  - [ok, push, "true"]
  - [ok, done, "false"]
  - [push, done]
`

func initializeFlowSurfaceSteps(sc *godog.ScenarioContext) {
	importGraph := func(ctx context.Context, definition string) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.ImportFlow(ctx, &quaycrewv1.ImportFlowRequest{Definition: definition})
		return nil
	}

	sc.Step(`^the operator imports the flow graph "([^"]*)"$`, func(ctx context.Context, name string) error {
		if err := importGraph(ctx, namedGraph); err != nil {
			return err
		}
		return worldFrom(ctx).lastErr
	})

	sc.Step(`^the operator imports the flow graph "([^"]*)" again$`, func(ctx context.Context, name string) error {
		return importGraph(ctx, namedGraph)
	})

	sc.Step(`^the operator imports a flow graph whose edge leads nowhere$`, func(ctx context.Context) error {
		return importGraph(ctx, `
name: broken
version: 1
mode: edits
nodes:
  a: { type: dispatch, prompt: "a" }
edges:
  - [a, nowhere]
`)
	})

	sc.Step(`^the operator starts a run of "([^"]*)" in the project$`, func(ctx context.Context, graph string) error {
		return startFlowRun(ctx, graph)
	})

	sc.Step(`^the operator imports a flow graph that cycles, capped at (\d+) transitions$`, func(ctx context.Context, cap int) error {
		return importGraph(ctx, fmt.Sprintf(`
name: loop
version: 1
mode: edits
limits:
  transitions: %d
nodes:
  begin: { type: dispatch, prompt: "begin" }
  more:  { type: choice, on: { result.failed: "false" } }
  again: { type: dispatch, prompt: "again" }
edges:
  - [begin, more]
  - [more, again, "true"]
  - [more, done, "false"]
  - [again, more]
`, cap))
	})

	sc.Step(`^the operator imports a flow graph capped at 0 transitions$`, func(ctx context.Context) error {
		return importGraph(ctx, `
name: never
version: 1
mode: edits
limits:
  transitions: 0
nodes:
  say: { type: dispatch, prompt: "hello" }
edges:
  - [say, done]
`)
	})

	sc.Step(`^the run stops$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr != nil {
			return fmt.Errorf("starting the run was refused: %v", w.lastErr)
		}
		return waitForFlowRun(ctx, w, flow.StatusStopped)
	})

	sc.Step(`^reading the run back says it was stopped for hitting its cap$`, func(ctx context.Context) error {
		run, err := readFlowRun(ctx, worldFrom(ctx))
		if err != nil {
			return err
		}
		if run.GetReason() == "" {
			return fmt.Errorf("the run stopped with no reason, so it reads the same as one that went quiet")
		}
		if !strings.Contains(run.GetReason(), "transitions") {
			return fmt.Errorf("the run stopped saying %q, want it to name the cap", run.GetReason())
		}
		return nil
	})

	sc.Step(`^the run's steps were asked no more than (\d+) tasks$`, func(ctx context.Context, most int) error {
		w := worldFrom(ctx)
		asked, err := flowRunTaskCount(ctx, w)
		if err != nil {
			return err
		}
		if asked > most {
			return fmt.Errorf("the run dispatched %d tasks, want no more than %d: the cap did not hold", asked, most)
		}
		return nil
	})

	sc.Step(`^a task takes a moment$`, func(ctx context.Context) error {
		worldFrom(ctx).runner.takes = 200 * time.Millisecond
		return nil
	})

	sc.Step(`^the operator stops the run, saying "([^"]*)"$`, func(ctx context.Context, reason string) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.StopFlowRun(ctx, &quaycrewv1.StopFlowRunRequest{
			Id: w.flowRunID, Reason: reason,
		})
		return nil
	})

	sc.Step(`^the operator stops a run that does not exist$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.StopFlowRun(ctx, &quaycrewv1.StopFlowRunRequest{Id: "no-such-run"})
		return nil
	})

	sc.Step(`^reading the run back says it was stopped saying "([^"]*)"$`, func(ctx context.Context, reason string) error {
		run, err := readFlowRun(ctx, worldFrom(ctx))
		if err != nil {
			return err
		}
		if run.GetReason() != reason {
			return fmt.Errorf("the run says it stopped because %q, want %q", run.GetReason(), reason)
		}
		return nil
	})

	sc.Step(`^the run's steps stop being asked, well short of the cap$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		// The task already under way when the stop landed finishes, by design, so this waits for
		// the count to settle rather than expecting it to be frozen the instant the stop returns.
		settled, err := settledTaskCount(ctx, w)
		if err != nil {
			return err
		}
		// The graph cycles with a cap of 50, so a stop that did nothing would leave 50 tasks here.
		// A handful means the run halted; the exact number depends on where the in flight task was.
		if settled > 5 {
			return fmt.Errorf("the stopped run dispatched %d tasks, so it kept going rather than halting", settled)
		}
		return nil
	})

	sc.Step(`^the run finishes$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr != nil {
			return fmt.Errorf("starting the run was refused: %v", w.lastErr)
		}
		return waitForFlowRun(ctx, w, flow.StatusDone)
	})

	sc.Step(`^reading the run back says it ran "([^"]*)" version (\d+)$`, func(ctx context.Context, name string, version int) error {
		run, err := readFlowRun(ctx, worldFrom(ctx))
		if err != nil {
			return err
		}
		if run.GetGraphName() != name || int(run.GetGraphVersion()) != version {
			return fmt.Errorf("the run ran %s version %d, want %s version %d",
				run.GetGraphName(), run.GetGraphVersion(), name, version)
		}
		return nil
	})

	sc.Step(`^reading the run back says it ended on "([^"]*)"$`, func(ctx context.Context, node string) error {
		run, err := readFlowRun(ctx, worldFrom(ctx))
		if err != nil {
			return err
		}
		if run.GetNode() != node {
			return fmt.Errorf("the run ended on %q, want %q", run.GetNode(), node)
		}
		return nil
	})

	sc.Step(`^reading the run back carries what the last task replied$`, func(ctx context.Context) error {
		run, err := readFlowRun(ctx, worldFrom(ctx))
		if err != nil {
			return err
		}
		if run.GetState()["result.reply"] == "" {
			return fmt.Errorf("the run carries no reply in its state: %v", run.GetState())
		}
		return nil
	})

	sc.Step(`^the run is listed among the project's runs$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListFlowRuns(ctx, &quaycrewv1.ListFlowRunsRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		for _, run := range listed.GetRuns() {
			if run.GetId() == w.flowRunID {
				return nil
			}
		}
		return fmt.Errorf("the project lists %d runs and none is %s", len(listed.GetRuns()), w.flowRunID)
	})

	sc.Step(`^the driver imports a flow graph$`, func(ctx context.Context) error {
		return asDriverCall(ctx, func(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) error {
			_, err := client.ImportFlow(ctx, &quaycrewv1.ImportFlowRequest{Definition: namedGraph})
			return err
		})
	})

	sc.Step(`^the driver starts a run of "([^"]*)" in the project$`, func(ctx context.Context, graph string) error {
		w := worldFrom(ctx)
		return asDriverCall(ctx, func(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) error {
			_, err := client.StartFlow(ctx, &quaycrewv1.StartFlowRequest{Graph: graph, Project: w.projectID})
			return err
		})
	})

	sc.Step(`^the driver is served$`, func(ctx context.Context) error {
		if err := worldFrom(ctx).driverErr; err != nil {
			return fmt.Errorf("the driver was refused: %v", err)
		}
		return nil
	})
}

// settledTaskCount waits for the run's task count to stop changing and answers what it settled at.
// A stop is cooperative, so the task already in flight lands after it; what matters is that no
// further task follows.
func settledTaskCount(ctx context.Context, w *world) (int, error) {
	deadline := time.Now().Add(10 * time.Second)
	last := -1
	stable := 0
	for time.Now().Before(deadline) {
		count, err := flowRunTaskCount(ctx, w)
		if err != nil {
			return 0, err
		}
		if count == last {
			stable++
			if stable >= 3 {
				return count, nil
			}
		} else {
			stable, last = 0, count
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0, fmt.Errorf("the run's task count never settled; it is still dispatching")
}

// flowRunTaskCount is how many tasks the run's steps have been asked, across every session they ran
// in, live or archived.
//
// No session at all is none of them, not a failure. A step's session is made when its job starts, so
// a run stopped before a controller got that far ran nothing. Reading that as an error made the whole
// suite depend on losing a race: on a loaded runner the stop lands first, and a scenario asserting the
// run halted then failed for having halted sooner than expected.
func flowRunTaskCount(ctx context.Context, w *world) (int, error) {
	if w.flowRunID == "" {
		return 0, fmt.Errorf("no run was started")
	}
	sessions, err := sessionsOfRun(ctx, w, w.flowRunID)
	if err != nil {
		return 0, err
	}
	asked := 0
	for _, session := range sessions {
		tasks, err := w.client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: session.GetId()})
		if err != nil {
			return 0, err
		}
		asked += len(tasks.GetTasks())
	}
	return asked, nil
}

// startFlowRun is the shared body of the two ways a scenario starts a run: the operator's, and the
// one that expects a refusal.
func startFlowRun(ctx context.Context, graph string) error {
	w := worldFrom(ctx)
	resp, err := w.client.StartFlow(ctx, &quaycrewv1.StartFlowRequest{Graph: graph, Project: w.projectID})
	w.lastErr = err
	if err == nil {
		w.flowRunID = resp.GetRun().GetId()
	}
	return nil
}

// readFlowRun reads the run the scenario started.
func readFlowRun(ctx context.Context, w *world) (*quaycrewv1.FlowRun, error) {
	if w.flowRunID == "" {
		return nil, fmt.Errorf("no run was started")
	}
	resp, err := w.client.GetFlowRun(ctx, &quaycrewv1.GetFlowRunRequest{Id: w.flowRunID})
	if err != nil {
		return nil, err
	}
	return resp.GetRun(), nil
}

// waitForFlowRun waits for the run to reach a status. A run advances behind the answer that started
// it, so a scenario that read once would be reading a race; this polls the store's own answer
// rather than sleeping a guess.
func waitForFlowRun(ctx context.Context, w *world, want string) error {
	deadline := time.Now().Add(10 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		// The crew is driven rather than waited for: a run declares its step and returns, so nothing
		// moves it until the job controller and the poller tick.
		if err := driveTheCrew(ctx); err != nil {
			return err
		}
		run, err := readFlowRun(ctx, w)
		if err != nil {
			return err
		}
		last = run.GetStatus()
		if last == want {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("the run is %q after ten seconds, want %q", last, want)
}

// asDriverCall makes one call carrying the driver's token, recording what came back, so a scenario
// can say whether the driver was served or refused.
func asDriverCall(ctx context.Context, call func(context.Context, quaycrewv1.ControlPlaneServiceClient) error) error {
	w := worldFrom(ctx)
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return w.listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("dial the control plane: %w", err)
	}
	defer func() { _ = conn.Close() }()
	callCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+w.driverToken)
	w.driverErr = call(callCtx, quaycrewv1.NewControlPlaneServiceClient(conn))
	// The authentication scenarios read their refusal from their own place, so both are filled.
	authFrom(ctx).err = w.driverErr
	return nil
}
