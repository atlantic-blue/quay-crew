package flow_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/atlantic-blue/krewe/internal/flow"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/store"
)

// A run that begins because something happened. The trigger's payload is the run's opening state,
// so the step reads {{url}} from what the trigger carried.
const reactingGraph = `
name: fix-red
version: 1
mode: edits
nodes:
  arrived: { type: trigger }
  fix:     { type: dispatch, prompt: "the build at {{url}} is red. Fix it." }
edges:
  - [arrived, fix]
  - [fix, done]
`

// The whole of the slice, in one test: something happened, nobody started anything, and a tick later
// a run is doing the work with what the trigger carried.
func TestATriggerStartsARunOnTheNextTick(t *testing.T) {
	engine, it, workspace, project := aSystem(t, reactingGraph)
	ctx := context.Background()

	raised, err := engine.Raise(ctx, flow.Trigger{
		GraphName: "fix-red", Workspace: workspace, Project: project,
		Payload: map[string]string{"url": "https://example.test/run/9"},
		Source:  "an in process caller",
	})
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	// Nothing has happened yet. The latency is the poll interval, and this is inside it.
	if runs := runsOf(t, it, "fix-red"); len(runs) != 0 {
		t.Fatalf("%d runs exist before the system ticked", len(runs))
	}

	tick(t, engine)

	runs := runsOf(t, it, "fix-red")
	if len(runs) != 1 {
		t.Fatalf("%d runs exist after the tick, want the one the trigger asked for", len(runs))
	}
	run := runs[0]
	if run.Status != flow.StatusWorking || run.Node != "fix" {
		t.Fatalf("the run is %q on %q, want it working on the step after the trigger node", run.Status, run.Node)
	}
	if run.State["url"] != "https://example.test/run/9" {
		t.Fatalf("the run opened knowing %v, want what the trigger carried", run.State)
	}
	step := stepOf(t, it, *run)
	if step.Brief != "the build at https://example.test/run/9 is red. Fix it." {
		t.Fatalf("the step was written down as %q, want the payload rendered into the prompt", step.Brief)
	}

	// And the row says what became of it, naming the run, so a reader is never left guessing whether
	// a trigger did anything.
	acted, err := it.store.GetTrigger(ctx, raised.ID)
	if err != nil {
		t.Fatalf("GetTrigger: %v", err)
	}
	if acted.Status != flow.TriggerStarted || acted.Run != run.ID {
		t.Fatalf("the trigger reads %q naming run %q, want it started naming the run", acted.Status, acted.Run)
	}
}

// One tree, and it is the job tree. A run something triggered is carried by a job like
// any other, so stopping that job stops the run and the tree budget counts what it spends.
func TestATriggeredRunIsCarriedByAJob(t *testing.T) {
	engine, it, workspace, project := aSystem(t, reactingGraph)
	ctx := context.Background()

	raised := raise(t, engine, flow.Trigger{GraphName: "fix-red", Workspace: workspace, Project: project})
	tick(t, engine)
	run := runsOf(t, it, "fix-red")[0]

	carrier, err := it.store.FlowRunCarrier(ctx, run.ID)
	if err != nil {
		t.Fatalf("FlowRunCarrier: %v", err)
	}
	carrying, err := it.store.GetJob(ctx, carrier)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if carrying.Phase != job.PhaseWaiting {
		t.Errorf("the run's own job is %q, want it held back so no controller sends it as a task", carrying.Phase)
	}
	// The label is how a person finds why a run nobody started exists: krewe job list
	// --label flow.trigger=<trigger>.
	if carrying.Labels["flow.trigger"] != raised.ID {
		t.Errorf("the run's own job is labelled %v, want it to name the trigger that caused it", carrying.Labels)
	}
	if step := stepOf(t, it, *run); step.Parent != carrier {
		t.Errorf("the step hangs under %q, want the run's own job", step.Parent)
	}
}

// A trigger that names the job that caused it puts the whole run under that job, so a
// flow started by job that finished is bounded by the same depth limit as everything else in the
// tree. This is what stops a flow that triggers itself running forever.
func TestARunHangsUnderTheJobThatCausedItsTrigger(t *testing.T) {
	engine, it, workspace, project := aSystem(t, reactingGraph)
	ctx := context.Background()

	cause, _, err := it.PrepareJob(ctx, "", job.Declaration{
		Workspace: workspace, Project: project, Title: "review the release", Brief: "read it",
	})
	if err != nil {
		t.Fatalf("PrepareJob: %v", err)
	}
	if err := it.store.CreateJob(ctx, cause, &job.Event{
		ID: store.NewID(), Kind: job.EventDeclared, Job: cause.ID,
		Workspace: workspace, Project: project,
	}); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	raise(t, engine, flow.Trigger{
		GraphName: "fix-red", Workspace: workspace, Project: project, Cause: cause.ID,
	})
	tick(t, engine)

	run := runsOf(t, it, "fix-red")[0]
	carrier, err := it.store.FlowRunCarrier(ctx, run.ID)
	if err != nil {
		t.Fatalf("FlowRunCarrier: %v", err)
	}
	carrying, err := it.store.GetJob(ctx, carrier)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if carrying.Parent != cause.ID {
		t.Fatalf("the run's own job hangs under %q, want the job that caused the trigger", carrying.Parent)
	}
	if carrying.Depth != cause.Depth+1 {
		t.Errorf("the run's own job is at depth %d and the job that caused it at %d, want one deeper",
			carrying.Depth, cause.Depth)
	}
}

// A trigger naming a flow nobody imported must not quietly do nothing. The row keeps the sentence
// that says what to do about it, because the row is the only place this is ever read.
func TestATriggerForAFlowNobodyImportedFailsOnItsRow(t *testing.T) {
	engine, it, workspace, project := aSystem(t, reactingGraph)
	ctx := context.Background()

	raised := raise(t, engine, flow.Trigger{
		GraphName: "never-imported", Workspace: workspace, Project: project,
	})
	tick(t, engine)

	failed, err := it.store.GetTrigger(ctx, raised.ID)
	if err != nil {
		t.Fatalf("GetTrigger: %v", err)
	}
	if failed.Status != flow.TriggerFailed {
		t.Fatalf("the trigger reads %q, want it failed rather than left as though nobody had got to it", failed.Status)
	}
	if !strings.Contains(failed.Reason, "never-imported") || !strings.Contains(failed.Reason, "krewe flow import") {
		t.Fatalf("the row says %q, want it to name the flow and what to do about it", failed.Reason)
	}
	if runs := runsOf(t, it, "never-imported"); len(runs) != 0 {
		t.Errorf("%d runs were started for a flow nobody imported", len(runs))
	}
	// And it is not read again on every tick for as long as the system runs.
	tick(t, engine)
	again, err := it.store.GetTrigger(ctx, raised.ID)
	if err != nil {
		t.Fatalf("GetTrigger: %v", err)
	}
	if again.Attempts != failed.Attempts {
		t.Errorf("a failed trigger was claimed again: %d attempts, then %d", failed.Attempts, again.Attempts)
	}
}

// A graph that begins with a dispatch begins when a person or a schedule says so. A trigger starting
// it would be an automation running for a reason its own file does not carry.
func TestATriggerForAGraphThatDoesNotReactFailsOnItsRow(t *testing.T) {
	engine, it, workspace, project := aSystem(t, twoStepGraph)
	ctx := context.Background()

	raised := raise(t, engine, flow.Trigger{GraphName: "fix-red", Workspace: workspace, Project: project})
	tick(t, engine)

	failed, err := it.store.GetTrigger(ctx, raised.ID)
	if err != nil {
		t.Fatalf("GetTrigger: %v", err)
	}
	if failed.Status != flow.TriggerFailed {
		t.Fatalf("the trigger reads %q, want it failed", failed.Status)
	}
	if !strings.Contains(failed.Reason, "fix") || !strings.Contains(failed.Reason, flow.NodeTrigger) {
		t.Fatalf("the row says %q, want it to name the node the graph begins at and the node type it needs", failed.Reason)
	}
	if runs := runsOf(t, it, "fix-red"); len(runs) != 0 {
		t.Errorf("%d runs were started for a graph that does not react", len(runs))
	}
}

// Two pollers, one trigger, one run. The claim is what makes this true, and paying for two runs of
// one thing happening is what it costs when it is not.
func TestTwoPollersStartOneRunFromOneTrigger(t *testing.T) {
	engine, it, workspace, project := aSystem(t, reactingGraph)

	raise(t, engine, flow.Trigger{GraphName: "fix-red", Workspace: workspace, Project: project})

	first := flow.NewPoller(engine, 0, quiet()).Owned("poller-one")
	second := flow.NewPoller(engine, 0, quiet()).Owned("poller-two")
	first.Tick(context.Background())
	second.Tick(context.Background())

	if runs := runsOf(t, it, "fix-red"); len(runs) != 1 {
		t.Fatalf("%d runs were started from one trigger", len(runs))
	}
}

// A trigger has to say what to run and where. Refused where it is raised rather than on a row nobody
// reads, because this one is the caller's own mistake and they are still here to be told.
func TestATriggerThatNamesNothingIsRefusedWhereItIsRaised(t *testing.T) {
	engine, it, workspace, project := aSystem(t, reactingGraph)
	ctx := context.Background()

	if _, err := engine.Raise(ctx, flow.Trigger{Workspace: workspace, Project: project}); err == nil {
		t.Error("a trigger naming no flow was accepted")
	}
	if _, err := engine.Raise(ctx, flow.Trigger{GraphName: "fix-red", Workspace: workspace}); err == nil {
		t.Error("a trigger naming no project was accepted")
	}
	pending, err := it.store.PendingTriggers(ctx, 0)
	if err != nil {
		t.Fatalf("PendingTriggers: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("%d triggers were written by calls that were refused", len(pending))
	}
}

// raise writes one down and answers with it.
func raise(t *testing.T, engine *flow.Engine, trigger flow.Trigger) flow.Trigger {
	t.Helper()
	raised, err := engine.Raise(context.Background(), trigger)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	return raised
}

// tick runs one poll, which is what starts the runs the triggers are waiting for.
func tick(t *testing.T, engine *flow.Engine) {
	t.Helper()
	flow.NewPoller(engine, 0, quiet()).Tick(context.Background())
}

// runsOf is every run of one graph, whoever or whatever started it.
func runsOf(t *testing.T, it *system, graph string) []*flow.Run {
	t.Helper()
	listed, err := it.store.ListFlowRuns(context.Background(), "")
	if err != nil {
		t.Fatalf("ListFlowRuns: %v", err)
	}
	out := make([]*flow.Run, 0)
	for _, run := range listed {
		if run.GraphName == graph {
			out = append(out, run)
		}
	}
	return out
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
