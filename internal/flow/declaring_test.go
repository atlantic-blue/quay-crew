package flow_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/flow"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// The graph these drive: two steps with a choice between them, which is the shape every flow has.
const twoStepGraph = `
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

// The fault this slice exists for. Starting a run used to call Dispatch and read the reply from the
// same statement, so the call lasted as long as the model did and the run could react to nothing
// while it waited.
//
// Now the call returns with the step written down and nothing sent. The engine cannot wait on a
// model at all: its view of the control plane has one method on it, and it is archiving a session.
func TestARunOutWithAStepHoldsNoDispatchOpen(t *testing.T) {
	engine, it, workspace, project := aCrew(t, twoStepGraph)

	run := started(t, engine, it, "fix-red", workspace, project)

	kept, err := it.store.GetFlowRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetFlowRun: %v", err)
	}
	if kept.Status != flow.StatusWorking {
		t.Fatalf("the run reads back as %q on %q, want it working", kept.Status, kept.Node)
	}
	step := stepOf(t, it, *kept)
	// Nothing has run it. A start that waited for the model would answer with the step already done,
	// and this is the assertion that tells the two apart.
	if step.Phase != job.PhasePending {
		t.Fatalf("the step is %q by the time the start returned, so the call waited for it", step.Phase)
	}
	if step.Session != "" {
		t.Errorf("the step already has session %q, so a task was sent inside the call", step.Session)
	}
	if step.Brief != "fix the build" {
		t.Errorf("the step was written down as %q, want the node's prompt", step.Brief)
	}
}

// One tree, and it is the job tree. A step hangs under the run's own job, one level deeper, which
// is what makes the depth limit and the tree budget bound a run at all.
func TestAStepHangsUnderTheRunOneLevelDeeper(t *testing.T) {
	engine, it, workspace, project := aCrew(t, twoStepGraph)
	ctx := context.Background()

	run := started(t, engine, it, "fix-red", workspace, project)

	carrier, err := it.store.FlowRunCarrier(ctx, run.ID)
	if err != nil {
		t.Fatalf("FlowRunCarrier: %v", err)
	}
	carrying, err := it.store.GetJob(ctx, carrier)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if carrying.Parent != "" || carrying.Depth != 0 {
		t.Fatalf("a run an operator started hangs under %q at depth %d, want a root", carrying.Parent, carrying.Depth)
	}
	if !strings.Contains(carrying.Brief, "fix-red") || !strings.Contains(carrying.Brief, "version 1") {
		t.Errorf("the run's own job says %q, want it to name the flow and its version", carrying.Brief)
	}
	// Held back rather than pending, or the job controller would send the run's own job as a task
	// and a person would read a second session doing nothing.
	if carrying.Phase != job.PhaseWaiting {
		t.Errorf("the run's own job is %q, want it held back so no controller runs it", carrying.Phase)
	}

	step := stepOf(t, it, run)
	if step.Parent != carrier {
		t.Errorf("the step hangs under %q, want the run's own job %q", step.Parent, carrier)
	}
	if step.Depth != carrying.Depth+1 {
		t.Errorf("the step is at depth %d and the run at %d, want one deeper", step.Depth, carrying.Depth)
	}
}

// The run carries on from the job rather than from a reply held in memory, so a crew restarted
// while twenty steps were running picks all twenty up off their rows.
func TestAStepThatEndedCarriesTheRunOn(t *testing.T) {
	engine, it, workspace, project := aCrew(t, twoStepGraph)

	run := started(t, engine, it, "fix-red", workspace, project)
	lands(t, it, stepOf(t, it, run), "session-of-fix", answered("fixed it"))

	run = ticked(t, engine, it, run)

	if run.Node != "push" || run.Status != flow.StatusWorking {
		t.Fatalf("the run is %q at %q, want it working on push", run.Status, run.Node)
	}
	if run.State["result.reply"] != "fixed it" {
		t.Errorf("the run read the step's answer as %q, want what the job carried", run.State["result.reply"])
	}
	// The step's session is put away the moment its job ends. That is what stops a run holding a
	// container while it does something else, and it is the whole point of the change.
	if len(it.archived) != 1 || it.archived[0] != "session-of-fix" {
		t.Errorf("the sessions put away are %v, want the step's own", it.archived)
	}

	lands(t, it, stepOf(t, it, run), "session-of-push", answered("pushed"))
	run = ticked(t, engine, it, run)
	if run.Status != flow.StatusDone {
		t.Fatalf("the run is %q at %q, want done", run.Status, run.Node)
	}
}

// The trap quay-crew#354 names: a run that asks holds its container for the whole wait, because a
// run used to close its session only at the end. The job ended when it answered, so there is
// nothing left running while a person decides.
func TestAnAskingRunHoldsNoContainer(t *testing.T) {
	engine, it, workspace, project := aCrew(t, `
name: careful
version: 1
mode: edits
nodes:
  fix:    { type: dispatch, prompt: "fix the build" }
  permit: { type: ask, text: "fixed it. push?" }
edges:
  - [fix, permit]
  - [permit, done]
`)
	ctx := context.Background()

	run := started(t, engine, it, "careful", workspace, project)
	lands(t, it, stepOf(t, it, run), "session-of-fix", answered("fixed it"))
	run = ticked(t, engine, it, run)

	if run.Status != flow.StatusAsking {
		t.Fatalf("the run is %q at %q, want it asking", run.Status, run.Node)
	}
	if len(it.archived) != 1 || it.archived[0] != "session-of-fix" {
		t.Fatalf("the asking run holds sessions %v away, want the step's own already put away", it.archived)
	}
	// And nothing is out: no step, so no container, no lease and no task.
	landed, err := it.store.LandedFlowSteps(ctx, 0)
	if err != nil {
		t.Fatalf("LandedFlowSteps: %v", err)
	}
	if len(landed) != 0 {
		t.Errorf("the asking run still has %d steps out", len(landed))
	}
	open, err := it.store.ListJobs(ctx, job.Filter{Phase: job.PhaseRunning})
	if err != nil {
		t.Fatalf("ListJob: %v", err)
	}
	if len(open) != 0 {
		t.Errorf("%d jobs are still running while the run asks", len(open))
	}
}

// A run's own job says where the run is and what it came to, so `quay job show` on it answers the
// two questions a person has without their reading a transcript.
func TestTheRunsOwnJobFollowsTheRun(t *testing.T) {
	engine, it, workspace, project := aCrew(t, `
name: careful
version: 1
mode: edits
nodes:
  fix:    { type: dispatch, prompt: "fix the build" }
  permit: { type: ask, text: "fixed it. push?" }
edges:
  - [fix, permit]
  - [permit, done]
`)
	ctx := context.Background()

	run := started(t, engine, it, "careful", workspace, project)
	carrier, err := it.store.FlowRunCarrier(ctx, run.ID)
	if err != nil {
		t.Fatalf("FlowRunCarrier: %v", err)
	}
	lands(t, it, stepOf(t, it, run), "session-of-fix", answered("fixed it"))
	run = ticked(t, engine, it, run)

	asking, err := it.store.GetJob(ctx, carrier)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if asking.Phase != job.PhaseAsking || asking.Question != "fixed it. push?" {
		t.Fatalf("the run's own job is %q asking %q, want it asking the run's question", asking.Phase, asking.Question)
	}

	if _, err := engine.Answer(ctx, run, "yes"); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	ended, err := it.store.GetJob(ctx, carrier)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if ended.Phase != job.PhaseDone {
		t.Fatalf("the run finished and its own job is %q", ended.Phase)
	}
	if ended.Answer != "fixed it" {
		t.Errorf("the run's own job answers %q, want what the run came to", ended.Answer)
	}
	if ended.FinishedAt == nil {
		t.Error("the run's own job has ended and carries no finishing time")
	}
}

// The records quay-crew#349 named and nothing ever wrote. They are job events against the run's own
// job, so a reader has one history rather than two.
func TestARunWritesTheRecordsOfItsOwnLife(t *testing.T) {
	engine, it, workspace, project := aCrew(t, twoStepGraph)
	ctx := context.Background()

	run := started(t, engine, it, "fix-red", workspace, project)
	carrier, err := it.store.FlowRunCarrier(ctx, run.ID)
	if err != nil {
		t.Fatalf("FlowRunCarrier: %v", err)
	}
	lands(t, it, stepOf(t, it, run), "session-of-fix", answered("fixed it"))
	run = ticked(t, engine, it, run)
	lands(t, it, stepOf(t, it, run), "session-of-push", answered("pushed"))
	ticked(t, engine, it, run)

	kinds := []string{}
	records, err := it.store.ListJobEvents(ctx, carrier)
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	for _, record := range records {
		kinds = append(kinds, record.Kind)
	}
	want := []string{job.EventDeclared, flow.EventRunStarted, flow.EventRunFinished}
	if len(kinds) != len(want) {
		t.Fatalf("the run's own job records %v, want %v", kinds, want)
	}
	for at := range want {
		if kinds[at] != want[at] {
			t.Fatalf("the run's own job records %v, want %v", kinds, want)
		}
	}
	// And every one of them is offered to the log, after the transaction that wrote it.
	offered := 0
	for _, record := range it.exported {
		if record.Job == carrier {
			offered++
		}
	}
	if offered != len(want) {
		t.Errorf("%d of the run's own records reached the log, want %d", offered, len(want))
	}
}

// A step that failed is a reply the graph may branch on: the crew ran it, the model did not finish,
// and the graph author decides what that means.
func TestAStepThatFailedIsAResultTheGraphReads(t *testing.T) {
	engine, it, workspace, project := aCrew(t, twoStepGraph)

	run := started(t, engine, it, "fix-red", workspace, project)
	lands(t, it, stepOf(t, it, run), "session-of-fix",
		job.Landing{Phase: job.PhaseFailed, Reason: "the model ran out of room"})

	run = ticked(t, engine, it, run)

	if run.State["result.failed"] != "true" {
		t.Errorf("the run read result.failed as %q, want true", run.State["result.failed"])
	}
	// The false edge of the choice, so the run finished without pushing anything.
	if run.Status != flow.StatusDone || run.Node != "done" {
		t.Fatalf("the run is %q at %q, want done down the failed edge", run.Status, run.Node)
	}
	if !strings.Contains(run.State["result.reply"], "ran out of room") {
		t.Errorf("the run read the failure as %q, want the reason on the job", run.State["result.reply"])
	}
}

// Job halted over a claim it did not meet stops the run rather than branching. The crew knows the
// job did not happen and does not know why, and a run that walks its success path through job that
// never happened ends with the model's plausible account of it.
func TestAStepStoppedOverItsClaimStopsTheRun(t *testing.T) {
	engine, it, workspace, project := aCrew(t, `
name: site-check
version: 1
mode: edits
nodes:
  read: { type: dispatch, prompt: "read package.json", expect: { file: package.json } }
  tell: { type: dispatch, prompt: "summarise it" }
edges:
  - [read, tell]
  - [tell, done]
`)

	run := started(t, engine, it, "site-check", workspace, project)
	step := stepOf(t, it, run)
	// What the node said would prove it worked travels on the job, so the controller is what checks
	// it: the one place that can see the session the task actually ran in.
	if step.ExpectFile != "package.json" {
		t.Fatalf("the step went out expecting %q, want the node's claim", step.ExpectFile)
	}
	lands(t, it, step, "session-of-read", job.Landing{
		Phase: job.PhaseStopped, Reason: "package.json is not in the session that did the job",
	})

	run = ticked(t, engine, it, run)

	if run.Status != flow.StatusStopped {
		t.Fatalf("the run is %q at %q, want it stopped", run.Status, run.Node)
	}
	if !strings.Contains(run.Reason, "package.json") {
		t.Errorf("the run stopped saying %q, want it to name what was not there", run.Reason)
	}
}

// A step the crew will not take is not a step that failed. No job was declared, so there is no
// reply and the run must not walk a success edge on one that will never exist.
func TestAStepTheCrewRefusesStopsTheRunWithTheReason(t *testing.T) {
	engine, it, workspace, project := aCrew(t, twoStepGraph)
	it.refuse = errors.New("this workspace allows job no deeper than 1, and this would be at depth 2")

	run, err := engine.Start(context.Background(), "fix-red", workspace, project, nil)
	if err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if run.Status != flow.StatusStopped {
		t.Fatalf("the run is %q at %q, want it stopped over the refusal", run.Status, run.Node)
	}
	if !strings.Contains(run.Reason, "no deeper than") {
		t.Errorf("the run stopped saying %q, want the crew's own sentence", run.Reason)
	}
	if !strings.Contains(run.Reason, "fix") {
		t.Errorf("the run stopped saying %q, want it to name the step", run.Reason)
	}
}

// Two pollers reading the same landed step move the run once. The second one holds a run that has
// moved on, and the store refuses it rather than replaying the movement.
func TestOneLandedStepMovesARunOnce(t *testing.T) {
	engine, it, workspace, project := aCrew(t, twoStepGraph)
	ctx := context.Background()

	run := started(t, engine, it, "fix-red", workspace, project)
	step := stepOf(t, it, run)
	ended := lands(t, it, step, "session-of-fix", answered("fixed it"))

	first, err := engine.Worked(ctx, run, ended)
	if err != nil {
		t.Fatalf("carrying the run on: %v", err)
	}
	// The same landed step again, from a poller that read the row a moment earlier.
	second, err := engine.Worked(ctx, run, ended)
	if err != nil {
		t.Fatalf("the second reading of one landed step failed: %v", err)
	}
	if second.Node != first.Node || second.Transitions != first.Transitions {
		t.Fatalf("the run moved twice: %d transitions at %q, then %d at %q",
			first.Transitions, first.Node, second.Transitions, second.Node)
	}
	transitions, err := it.store.ListFlowTransitions(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListFlowTransitions: %v", err)
	}
	if len(transitions) != 2 {
		t.Fatalf("the run recorded %d movements, want the start and the one step", len(transitions))
	}
}

// A run somebody stopped while its step was out stays stopped: the step finishes, because the model
// is already working, and the run takes no further step.
func TestAStoppedRunIsNotCarriedOnByItsStep(t *testing.T) {
	engine, it, workspace, project := aCrew(t, twoStepGraph)
	ctx := context.Background()

	run := started(t, engine, it, "fix-red", workspace, project)
	if _, err := it.store.StopFlowRun(ctx, run.ID, "the operator stopped it"); err != nil {
		t.Fatalf("StopFlowRun: %v", err)
	}
	lands(t, it, stepOf(t, it, run), "session-of-fix", answered("fixed it"))

	run = ticked(t, engine, it, run)

	if run.Status != flow.StatusStopped {
		t.Fatalf("the run is %q at %q, want it left stopped", run.Status, run.Node)
	}
	if run.Reason != "the operator stopped it" {
		t.Errorf("the run says %q, want the reason it was stopped with", run.Reason)
	}
}

// A step names its role on the job it goes out as, so the boundary is on the record rather
// than inside the call that made it.
func TestAStepNamingARoleDeclaresJobInThatRole(t *testing.T) {
	engine, it, workspace, project := aCrew(t, `
name: write-tests
version: 1
mode: edits
nodes:
  plan:  { type: dispatch, prompt: "say what needs testing" }
  tests: { type: dispatch, role: test-writer, prompt: "write the tests" }
edges:
  - [plan, tests]
  - [tests, done]
`)

	run := started(t, engine, it, "write-tests", workspace, project)
	plan := stepOf(t, it, run)
	if plan.Role != "" {
		t.Errorf("the step naming no role went out as job in role %q", plan.Role)
	}
	lands(t, it, plan, "session-of-plan", answered("these need testing"))

	run = ticked(t, engine, it, run)

	tests := stepOf(t, it, run)
	if tests.Role != "test-writer" {
		t.Errorf("the step went out as job in role %q, want test-writer", tests.Role)
	}
	if tests.Parent != plan.Parent {
		t.Errorf("the two steps hang under %q and %q, want both under the run", tests.Parent, plan.Parent)
	}
}

// The mode travels with every step, because a session is born in the crew's own mode and a step is a
// new session now. A graph whose first step is "clone this" needs the network, and a dispatched task
// has nobody to ask.
func TestAGraphsModeTravelsWithEveryStep(t *testing.T) {
	engine, it, workspace, project := aCrew(t, `
name: clone-first
version: 1
mode: dangerous
nodes:
  clone: { type: dispatch, prompt: "clone the repository" }
  read:  { type: dispatch, prompt: "read it" }
edges:
  - [clone, read]
  - [read, done]
`)

	run := started(t, engine, it, "clone-first", workspace, project)
	first := stepOf(t, it, run)
	// The runtime's own spelling, which is what the parser turns "dangerous" into at import.
	if first.Mode != "bypassPermissions" {
		t.Fatalf("the first step goes out in mode %q, want the graph's", first.Mode)
	}
	lands(t, it, first, "session-of-clone", answered("cloned"))

	run = ticked(t, engine, it, run)

	if second := stepOf(t, it, run); second.Mode != "bypassPermissions" {
		t.Errorf("the second step goes out in mode %q, want the graph's on every step", second.Mode)
	}
}

// watchingStore records the phase a run's own job is written in, which is the one thing that cannot
// be read afterwards: the first movement of the run writes over it a moment later.
//
// It matters because the job controller reads pending job and sends a task for it. A run's own job
// written pending would be dispatched as a task, in the window between the run being created and its
// first movement, and a person would read a session doing nothing.
type watchingStore struct {
	store.Store
	writtenAs string
}

func (w *watchingStore) CreateFlowRun(ctx context.Context, run *flow.Run, carrier *job.Job, records []*job.Event, trigger string) error {
	w.writtenAs = carrier.Phase
	return w.Store.CreateFlowRun(ctx, run, carrier, records, trigger)
}

func TestARunsOwnJobIsNeverOfferedToAController(t *testing.T) {
	ctx := context.Background()
	kept := store.NewMemory()
	workspace, err := kept.CreateWorkspace(ctx, "acme")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := kept.CreateProject(ctx, workspace.GetId(), "house-bills")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := kept.ImportFlowGraph(ctx, "fix-red", 1, twoStepGraph); err != nil {
		t.Fatalf("ImportFlowGraph: %v", err)
	}
	watching := &watchingStore{Store: kept}
	it := &crew{store: kept, maxDepth: 4}
	engine := flow.NewEngine(watching, it, nil, it)

	if _, err := engine.Start(ctx, "fix-red", workspace.GetId(), project.GetId(), nil); err != nil {
		t.Fatalf("starting the run: %v", err)
	}

	if watching.writtenAs != job.PhaseWaiting {
		t.Fatalf("the run's own job was written %q, want it held back: pending job is what a controller sends",
			watching.writtenAs)
	}
	// And nothing pending is left behind for a controller to find, other than the step itself.
	runnable, err := kept.RunnableJob(ctx, 0)
	if err != nil {
		t.Fatalf("RunnableJob: %v", err)
	}
	for _, one := range runnable {
		if one.Labels["flow.node"] == "" {
			t.Errorf("the controller is offered %q, which is the run itself rather than a step", one.Title)
		}
	}
}
