package features_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/flow"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/atlantic-blue/quay-crew/internal/work"
	"github.com/cucumber/godog"
)

// The flow scenarios drive the crew the way the crew drives itself. A run declares its step as a
// piece of work and returns, so nothing here finishes a run on its own: the work controller sends
// the task, and the flow poller carries the run on when that work ends. Both are ticked rather than
// waited for, because a scenario that slept would be slow when it passed and flaky when it did not.

// planeClient adapts the gRPC client to the one call the engine is allowed. The engine dispatches
// nothing now: a step is a piece of work, and the work controller is what sends its task.
type planeClient struct {
	client quaycrewv1.ControlPlaneServiceClient
}

func (p planeClient) ArchiveSession(ctx context.Context, req *quaycrewv1.ArchiveSessionRequest) (*quaycrewv1.ArchiveSessionResponse, error) {
	return p.client.ArchiveSession(ctx, req)
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

	// A refusal that only says the word is wrong leaves the author guessing. The modes are three
	// words nobody remembers, so the refusal carries them.
	sc.Step(`^the refusal names the modes there are$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("nothing was refused")
		}
		for _, offered := range model.PermissionModesOffered() {
			if !strings.Contains(w.lastErr.Error(), offered) {
				return fmt.Errorf("the refusal says %q, want it to offer %q", w.lastErr, offered)
			}
		}
		return nil
	})

	sc.Step(`^the operator starts the flow "([^"]*)" in the project$`, func(ctx context.Context, name string) error {
		w := worldFrom(ctx)
		engine := flow.NewEngine(w.store, planeClient{client: w.client}, nil, w.server)
		run, err := engine.Start(ctx, name, w.workspaceID, w.projectID, nil)
		w.flowRun, w.lastErr = run, err
		if err != nil {
			return nil
		}
		return driveTheCrew(ctx)
	})

	// Started and left alone, so a scenario can say what is true the moment the call returns rather
	// than what is true once the crew has driven the run to a standstill.
	sc.Step(`^the operator starts the flow "([^"]*)" in the project, without driving the crew$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			engine := flow.NewEngine(w.store, planeClient{client: w.client}, nil, w.server)
			run, err := engine.Start(ctx, name, w.workspaceID, w.projectID, nil)
			w.flowRun, w.lastErr = run, err
			return err
		})

	sc.Step(`^the crew is driven$`, func(ctx context.Context) error {
		return driveTheCrew(ctx)
	})

	// A step out with the model, and a run that is not waiting on it. The call that started the run
	// returned long ago, and reading the run answers now rather than when the model does.
	sc.Step(`^the run's step is running while the model has not answered$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
		if err != nil {
			return err
		}
		step, err := runStepOn(ctx, w, kept.Node)
		if err != nil {
			return err
		}
		if step.Phase != work.PhaseRunning {
			return fmt.Errorf("the step is %q, want it running with the model", step.Phase)
		}
		if step.Answer != "" {
			return fmt.Errorf("the step already answers %q, so the model had answered", step.Answer)
		}
		return nil
	})

	sc.Step(`^the flow run is working$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
		if err != nil {
			return err
		}
		if kept.Status != flow.StatusWorking {
			return fmt.Errorf("the run reads back as %q on node %q, want it working", kept.Status, kept.Node)
		}
		return nil
	})

	sc.Step(`^the run's step is a piece of work under the run, one level deeper$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
		if err != nil {
			return err
		}
		carrier, err := runCarrier(ctx, w)
		if err != nil {
			return err
		}
		step, err := runStepOn(ctx, w, kept.Node)
		if err != nil {
			return err
		}
		if step.Parent != carrier.ID {
			return fmt.Errorf("the step hangs under %q, want the work carrying the run %q", step.Parent, carrier.ID)
		}
		if step.Depth != carrier.Depth+1 {
			return fmt.Errorf("the step is at depth %d and the run at %d, want one deeper", step.Depth, carrier.Depth)
		}
		if step.Brief == "" {
			return fmt.Errorf("the step was written down with no brief, so nothing would be asked")
		}
		return nil
	})

	sc.Step(`^no session the run started is still live$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		sessions, err := runSessions(ctx, w)
		if err != nil {
			return err
		}
		if len(sessions) == 0 {
			return fmt.Errorf("the run started no sessions at all, so this proves nothing")
		}
		for _, session := range sessions {
			if session.GetArchivedAt() == nil {
				return fmt.Errorf("session %s is still live, so the run holds a container while it asks",
					session.GetHandle())
			}
		}
		return nil
	})

	sc.Step(`^no piece of work of the run is still open$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		steps, err := runSteps(ctx, w)
		if err != nil {
			return err
		}
		for _, step := range steps {
			if !work.Terminal(step.Phase) {
				return fmt.Errorf("the step %q is %q, so the run is still out with work while it asks",
					step.Title, step.Phase)
			}
		}
		return nil
	})

	sc.Step(`^each of the run's steps carries the answer its task gave$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		steps, err := runSteps(ctx, w)
		if err != nil {
			return err
		}
		if len(steps) == 0 {
			return fmt.Errorf("the run took no steps, so this proves nothing")
		}
		for _, step := range steps {
			if step.Answer == "" {
				return fmt.Errorf("the step %q is %q and carries no answer, so a caller reading it back gets nothing",
					step.Title, step.Phase)
			}
		}
		return nil
	})

	sc.Step(`^the run's own work carries what the run came to$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		carrier, err := runCarrier(ctx, w)
		if err != nil {
			return err
		}
		if carrier.Phase != work.PhaseDone {
			return fmt.Errorf("the run finished and the work carrying it is %q", carrier.Phase)
		}
		if carrier.Answer == "" {
			return fmt.Errorf("the work carrying the run carries no answer")
		}
		return nil
	})

	sc.Step(`^the run's own work records "([^"]*)" and then "([^"]*)"$`,
		func(ctx context.Context, first, second string) error {
			w := worldFrom(ctx)
			carrier, err := runCarrier(ctx, w)
			if err != nil {
				return err
			}
			records, err := w.store.ListWorkEvents(ctx, carrier.ID)
			if err != nil {
				return err
			}
			kinds := make([]string, 0, len(records))
			for _, record := range records {
				kinds = append(kinds, record.Kind)
			}
			at, then := indexOfKind(kinds, first), indexOfKind(kinds, second)
			if at < 0 || then < 0 || at > then {
				return fmt.Errorf("the work carrying the run records %v, want %q before %q", kinds, first, second)
			}
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
		if w.lastErr != nil {
			return w.lastErr
		}
		// The answer moves the run to its next step, and that step is a piece of work a controller
		// runs rather than a call the answer waits on.
		return driveTheCrew(ctx)
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
			if err := driveTheCrew(ctx); err != nil {
				return err
			}
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

	sc.Step(`^the run's steps were asked "([^"]*)" and then "([^"]*)"$`, func(ctx context.Context, first, second string) error {
		tasks, err := flowRunTasks(ctx, worldFrom(ctx))
		if err != nil {
			return err
		}
		if len(tasks) != 2 {
			return fmt.Errorf("the run's steps hold %d tasks, want 2", len(tasks))
		}
		if tasks[0].GetPrompt() != first || tasks[1].GetPrompt() != second {
			return fmt.Errorf("the steps were asked %q then %q, want %q then %q",
				tasks[0].GetPrompt(), tasks[1].GetPrompt(), first, second)
		}
		return nil
	})

	sc.Step(`^the run's steps were asked (\d+) tasks?$`, func(ctx context.Context, want int) error {
		tasks, err := flowRunTasks(ctx, worldFrom(ctx))
		if err != nil {
			return err
		}
		if len(tasks) != want {
			return fmt.Errorf("the run's steps hold %d tasks, want %d", len(tasks), want)
		}
		return nil
	})

	sc.Step(`^the flow run is stopped$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr != nil {
			return fmt.Errorf("the run did not start: %v", w.lastErr)
		}
		kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
		if err != nil {
			return err
		}
		if kept.Status != flow.StatusStopped {
			return fmt.Errorf("the run reads back as %q on node %q, want it stopped", kept.Status, kept.Node)
		}
		return nil
	})

	sc.Step(`^reading the run back says it stopped over "([^"]*)"$`, func(ctx context.Context, named string) error {
		w := worldFrom(ctx)
		kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
		if err != nil {
			return err
		}
		// The reason, not only the state: a run somebody reads tomorrow has the reason on its own
		// line, and "stopped" with nothing beside it reads the same as a run that went quiet.
		if !strings.Contains(kept.Reason, named) {
			return fmt.Errorf("the run stopped saying %q, want it to name %q", kept.Reason, named)
		}
		if kept.State["result.expected"] == "" {
			return fmt.Errorf("the run carries nothing about what it expected")
		}
		return nil
	})

	// A model that does the work rather than describing it. The file lands in the session's own
	// working directory, which is the same directory the crew checks, so this proves the whole road
	// rather than a double agreeing with itself.
	sc.Step(`^the model writes "([^"]*)" while it works$`, func(ctx context.Context, name string) error {
		w := worldFrom(ctx)
		w.runner.onTask = func() {
			for _, cfg := range w.provider.Configurations() {
				dir, kept := w.storage.WorkingDir(cfg)
				if !kept {
					continue
				}
				if err := os.MkdirAll(dir, 0o777); err != nil {
					continue
				}
				_ = os.WriteFile(filepath.Join(dir, name), []byte("written by the task\n"), 0o600)
			}
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
	engine := flow.NewEngine(w.store, planeClient{client: w.client}, nil, w.server).
		WithClock(func() time.Time { return at })
	flow.NewPoller(engine, 0, slog.New(slog.NewTextHandler(io.Discard, nil))).Tick(ctx)
	return driveTheCrew(ctx)
}

// driveTheCrew ticks the crew's two loops until nothing is moving.
//
// This is what the crew does on its own every few seconds, done in one pass rather than over an
// interval: the work controller claims each step and sends its task, the task lands, the controller
// writes the answer onto the work, and the poller carries the run on to its next step. It ends when
// no piece of work is open and no run has a step that has ended, which is a run that finished,
// waited, asked or stopped.
//
// The cap is a graph's own transition cap plus room, so a run that will not settle fails here rather
// than hanging.
func driveTheCrew(ctx context.Context) error {
	w := worldFrom(ctx)
	for range flow.DefaultTransitions + 10 {
		// Send what has not started, let every task land, then write what came back.
		w.server.TickWork(ctx)
		if err := w.settled(ctx); err != nil {
			return err
		}
		w.server.TickWork(ctx)
		w.server.TickFlows(ctx)
		if !w.somethingIsMoving(ctx) {
			return nil
		}
	}
	return fmt.Errorf("the crew was still moving after %d passes", flow.DefaultTransitions+10)
}

// somethingIsMoving says whether any piece of work has yet to end, or any run is sitting on a step
// that already has.
func (w *world) somethingIsMoving(ctx context.Context) bool {
	landed, err := w.store.LandedFlowSteps(ctx, 0)
	if err != nil || len(landed) > 0 {
		return true
	}
	for _, phase := range []string{work.PhasePending, work.PhaseRunning} {
		open, err := w.store.ListWork(ctx, work.Filter{Phase: phase})
		if err != nil || len(open) > 0 {
			return true
		}
	}
	return false
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

// runSessions are the sessions a run's steps ran in, in the order the run took them.
//
// A run has no session of its own any more. Each step is a piece of work, the work owns the session
// that did it, and the run remembers which one against the node, so this reads the run's own state
// rather than looking for a handle named after the run.
func runSessions(ctx context.Context, w *world) ([]*quaycrewv1.Session, error) {
	return sessionsOfRun(ctx, w, w.flowRun.ID)
}

func sessionsOfRun(ctx context.Context, w *world, id string) ([]*quaycrewv1.Session, error) {
	run, err := w.store.GetFlowRun(ctx, id)
	if err != nil {
		return nil, err
	}
	held := map[string]*quaycrewv1.Session{}
	for _, archived := range []bool{false, true} {
		listed, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Archived: archived})
		if err != nil {
			return nil, err
		}
		for _, session := range listed.GetSessions() {
			held[session.GetId()] = session
		}
	}
	out := make([]*quaycrewv1.Session, 0)
	for _, wanted := range flow.SessionsIn(run.State) {
		session, found := held[wanted]
		if !found {
			return nil, fmt.Errorf("the run says step session %q did some of its work, and the crew does not hold it", wanted)
		}
		out = append(out, session)
	}
	return out, nil
}

// flowRunTasks is what the run's steps were asked, in the order the run took them.
func flowRunTasks(ctx context.Context, w *world) ([]*quaycrewv1.Task, error) {
	sessions, err := runSessions(ctx, w)
	if err != nil {
		return nil, err
	}
	out := make([]*quaycrewv1.Task, 0)
	for _, session := range sessions {
		resp, err := w.client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: session.GetId()})
		if err != nil {
			return nil, err
		}
		out = append(out, resp.GetTasks()...)
	}
	return out, nil
}

// runCarrier is the piece of work that carries the run the scenario started.
func runCarrier(ctx context.Context, w *world) (*work.Work, error) {
	id, err := w.store.FlowRunCarrier(ctx, w.flowRun.ID)
	if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, fmt.Errorf("the run hangs under no piece of work, so it is outside the tree")
	}
	return w.store.GetWork(ctx, id)
}

// runSteps is every piece of work the run declared for a step, oldest first. Found by the labels each
// one carries, which is the road a person takes too: quay work list --label flow.run=<run>.
func runSteps(ctx context.Context, w *world) ([]*work.Work, error) {
	listed, err := w.store.ListWork(ctx, work.Filter{LabelKey: "flow.run", LabelValue: w.flowRun.ID})
	if err != nil {
		return nil, err
	}
	out := make([]*work.Work, 0, len(listed))
	for at := len(listed) - 1; at >= 0; at-- {
		if listed[at].Labels["flow.node"] == "" {
			continue
		}
		whole, err := w.store.GetWork(ctx, listed[at].ID)
		if err != nil {
			return nil, err
		}
		out = append(out, whole)
	}
	return out, nil
}

// runStepOn is the piece of work the run declared for one node.
func runStepOn(ctx context.Context, w *world, node string) (*work.Work, error) {
	steps, err := runSteps(ctx, w)
	if err != nil {
		return nil, err
	}
	for _, step := range steps {
		if step.Labels["flow.node"] == node {
			return step, nil
		}
	}
	return nil, fmt.Errorf("the run is on %s and declared no work for it", node)
}

func indexOfKind(kinds []string, want string) int {
	for at, kind := range kinds {
		if kind == want {
			return at
		}
	}
	return -1
}
