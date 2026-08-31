package flow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/job"
)

// StatusWorking is a run whose step is out with a job.
//
// It sits beside waiting and asking: a live run that costs the engine nothing, because what moves it
// is a row a poller reads rather than a call somebody is holding open. The reducer never sees this
// word. The engine puts a run back to running before it feeds the step's result in, which is why
// advance.go did not change when this arrived.
const StatusWorking = "working"

// The records a run writes about itself, which quay-crew#349 named and nothing ever wrote. They are
// job events against the job that carries the run, so a reader has one history to read
// rather than two, and the export they reach is the one `<workspace>.job` already carries.
const (
	EventRunStarted  = "flow.run.started"
	EventRunAsked    = "flow.run.asked"
	EventRunFinished = "flow.run.finished"
	EventRunStopped  = "flow.run.stopped"
	// EventProductReplaced is the operator answering the question at the first usable path with a
	// sentence of their own. The detail is the new sentence, so the tree says what the rest of the
	// work was done against.
	EventProductReplaced = "flow.product.replaced"
)

// pollBatch is how many landed steps one tick carries on. A system with a thousand finished runs is
// not a reason for one tick to hold the store open.
const pollBatch = 50

// Store is what the engine needs from the database: a graph to run, and a run whose every
// transition lands in the same transaction as the state it describes. Implemented by the system's
// store; defined here so this package stays importable by it.
type Store interface {
	// ImportFlowGraph stores a graph at a version. The same name and version twice is refused,
	// because a run is pinned to the version it started with and a version that can change is not
	// a pin.
	ImportFlowGraph(ctx context.Context, name string, version int, definition string) error
	// LatestFlowGraph returns the newest version of a graph and its definition. A new run takes
	// the newest version, and pins it.
	LatestFlowGraph(ctx context.Context, name string) (int, string, error)
	// FlowGraph returns one exact version. A run already under way is carried on with the version
	// it pinned, never the newest, or editing a file would change an automation halfway through.
	FlowGraph(ctx context.Context, name string, version int) (string, error)
	// GetFlowRun reads a run back, which the engine needs to answer with the stopped run rather
	// than with what it held before the stop landed.
	GetFlowRun(ctx context.Context, id string) (*Run, error)
	// FlowRunCarrier is the job that carries a run. Read from the row rather than kept in
	// a process, so a run picked up after a restart is still in the same place in the tree.
	FlowRunCarrier(ctx context.Context, run string) (string, error)
	// GetJob reads one job back. The engine reads the job carrying a run to write a
	// record against it, because a record has to agree with the row it describes.
	GetJob(ctx context.Context, id string) (*job.Job, error)
	// ReplaceJobProduct writes the one sentence a job serves over what it carried. The engine calls
	// it when the operator answers the question at the first usable path with a sentence of their
	// own, and it is the whole of what "the work continues from it" means: every step declared after
	// this carries the new sentence, because a step takes it from the job above it.
	ReplaceJobProduct(ctx context.Context, id, product string, event *job.Event) (*job.Job, error)
	// ScheduleFlow records that a graph runs in a project every so often, from now. Re-recording
	// the same pair moves its schedule rather than making a second one.
	ScheduleFlow(ctx context.Context, graph, project string, every time.Duration, next time.Time) error
	// UnscheduleFlow stops a graph running on its own in a project.
	UnscheduleFlow(ctx context.Context, graph, project string) error
	// DueFlowSchedules are the schedules whose time has come, each with the moment to set next.
	DueFlowSchedules(ctx context.Context, now time.Time) ([]Schedule, error)
	// MarkFlowScheduled moves a schedule on to its next due time, which the poller does before
	// starting the run, so a start that fails does not leave the schedule firing every tick.
	MarkFlowScheduled(ctx context.Context, graph, project string, next time.Time) error
	// DueFlowRuns are the waiting runs whose time has come. The poller asks for these and nothing
	// else, so a system with a thousand finished runs and one waiting does one row's job per tick.
	DueFlowRuns(ctx context.Context, now time.Time) ([]*Run, error)
	// LandedFlowSteps are the runs whose step has ended: working runs whose step's job reached a
	// terminal phase. This is what moves a run now that it does not hold its own dispatch open.
	LandedFlowSteps(ctx context.Context, limit int) ([]Landed, error)
	// CreateFlowRun writes a fresh run and the job that carries it, in one transaction.
	// Its transitions are the events; creation is the row itself.
	//
	// trigger is the pending trigger this run answers, empty for a run nothing triggered, and it is
	// marked started in the same transaction. That is what makes one trigger start exactly one run:
	// a poller that dies after this lands finds a row that says started when it comes back, and one
	// that dies before it finds a row nobody acted on.
	CreateFlowRun(ctx context.Context, run *Run, carrier *job.Job, records []*job.Event, trigger string) error
	// RaiseTrigger writes down that something happened. The row is the queue entry a run starts
	// from, and it is written in one statement so it can sit in the transaction of whatever caused
	// it.
	RaiseTrigger(ctx context.Context, trigger *Trigger) error
	// PendingTriggers are the triggers nothing has started a run from and nobody is holding: pending,
	// under no live claim, oldest first. The poller reads these and nothing else.
	PendingTriggers(ctx context.Context, limit int) ([]*Trigger, error)
	// ClaimTrigger takes a lease on one trigger row, in the same statement as the condition, so two
	// pollers reading one pending trigger leave one holder. A row somebody else holds, or one that
	// has already been acted on, answers ErrTriggerTaken.
	ClaimTrigger(ctx context.Context, id string, lease job.Lease) (*Trigger, error)
	// FailTrigger records that a claimed trigger started no run, and why. The reason is the whole
	// point of the row surviving: a trigger that did nothing must not read the same as one nobody
	// raised.
	FailTrigger(ctx context.Context, id, reason string) error
	// GetTrigger reads one trigger back, which is where what became of it is read.
	GetTrigger(ctx context.Context, id string) (*Trigger, error)
	// AdvanceFlowRun writes the run's next position, appends the transition that took it there,
	// claims the dispatch's key where the transition dispatched, and writes the job that movement
	// declares. One transaction, so there is no gap for a crash to hide in: either the run moved and
	// the claim and the job exist, or none of them does.
	// A dispatch key already claimed refuses the whole transition.
	//
	// It moves only a run the database still holds as live, which is what makes stopping one take
	// effect: the operator's stop lands, and the engine's next write finds the run halted and is
	// refused with ErrRunHalted rather than quietly setting it back to running. Where the transition
	// answers a step, it moves only a run still out with that step, so two pollers reading the same
	// landed step move the run once.
	AdvanceFlowRun(ctx context.Context, run *Run, transition Transition) error
}

// ErrRunHalted is what AdvanceFlowRun answers when the run it was asked to move is no longer where
// this caller left it: somebody stopped it, or another poller carried it on first.
var ErrRunHalted = errors.New("flow: the run is no longer where this movement left it")

// Schedule is a graph the system starts on its own, in one project, every so often.
type Schedule struct {
	GraphName string
	Project   string
	Workspace string
	Every     time.Duration
}

// Landed is a run whose step has ended, with the job it ended as. The step's parent is the
// run's own job, so nothing else has to be read to know where in the tree the run sits.
type Landed struct {
	Run  Run
	Step *job.Job
}

// DueAt is when a waiting run should be looked at again, or nil for a run that is not waiting. It
// travels with the transition because the due time and the position it belongs to have to land in
// the same write: a run recorded as waiting with no due time would wait forever.
type DueAt = *time.Time

// Transition is one movement of a run, as recorded: what arrived, where the run now sits, and what
// the engine was told to do about it.
type Transition struct {
	Event string
	Node  string
	// Dispatch is set when the transition asked for a task, and its key of run, node and attempt
	// is what makes sending the same task twice impossible.
	Dispatch *Command
	// Due is when the run should be looked at again, set only when this movement reached a wait.
	Due DueAt
	// Answers is the step this movement carries the run on from, empty on every other movement. The
	// store moves only a run still out with it, so two pollers reading one landed step move the run
	// once.
	Answers string
	// Job is what this movement does to the job tree, written in the same transaction as the
	// movement itself.
	Job JobWrite
}

// JobWrite is the job tree's side of one movement.
//
// Both halves land in the transaction that moves the run, which is what makes a step exactly once:
// a job written before the movement would be paid for by a movement that was then refused,
// and one written after would be lost by a crash in between.
type JobWrite struct {
	// Declared is the job this movement's step goes out as, and it becomes the step the
	// run is out with. Nil on a movement that dispatches nothing, which also clears that step.
	Declared *job.Job
	// Carrier is what this movement writes onto the job that carries the run.
	Carrier *Carrier
	// Records are the movements to write against the job, in the order they happened.
	Records []*job.Event
}

// Carrier is what a movement writes onto the job that carries a run: the phase it is in
// now, the question it is putting to a person, and what the run came to once it has ended.
type Carrier struct {
	Job      string
	Phase    string
	Question string
	Answer   string
	Reason   string
}

// RecordedTransition is a transition as read back: the order it happened in, what arrived, and
// where the run sat afterwards. The audit record and the replay record are these rows.
type RecordedTransition struct {
	Seq   int
	Event string
	Node  string
}

// Spend is what a run's own session has cost so far, in tokens. The engine reads it before each
// movement and hands it to the reducer, which is the only way a ceiling can stop a task before it
// is paid for rather than after.
//
// It takes the session rather than a conversation because resolving one to the other is the system's
// job, not the engine's: the session row knows which conversation it is having and which workspace
// keeps the transcript. An implementation that cannot tell answers zero.
type Spend interface {
	SessionTokens(ctx context.Context, session string) int64
}

// ControlPlane is what the engine may do on a run's behalf. It is the same service every other
// caller speaks to, deliberately: the engine holds no privileged road into anything.
//
// It no longer dispatches. A step is a job now, and the job controller is what sends its
// task, so the one call left here is putting a session away.
type ControlPlane interface {
	ArchiveSession(ctx context.Context, req *quaycrewv1.ArchiveSessionRequest) (*quaycrewv1.ArchiveSessionResponse, error)
}

// Works is what the engine needs from the system to keep a run inside the job tree.
//
// It prepares rather than writes, because the declaration and the movement that asked for it land in
// one transaction. Preparing is where every rule a caller's declaration is held to is applied: the
// depth, the workspace's ceiling, the role's pinned version and the trace. The engine does not get a
// cheaper road than a caller, it gets the same one.
type Works interface {
	// PrepareJob holds a declaration to every rule and answers with the row to write and the record
	// of writing it. `under` names the job this one hangs under; empty means the parent
	// comes from the credential the caller presented, which is what a run started by a person wants.
	PrepareJob(ctx context.Context, under string, declaration job.Declaration) (*job.Job, *job.Event, error)
	// ExportJob offers records to the event log, after the transaction that wrote them.
	ExportJob(ctx context.Context, events ...*job.Event)
}

// Engine drives runs: it loads the graph, calls the pure reducer, persists every transition, and
// declares the job the reducer asked for.
//
// It never waits on a model. A step is written down as a job and the call returns, so a
// run holds no goroutine, no dispatch and no container while its step runs, and a run that then asks
// a person holds nothing at all.
type Engine struct {
	store Store
	plane ControlPlane
	spend Spend
	works Works
	// clock is where the engine reads the time, so a test can put a wait's due time in the past
	// rather than waiting out a real one. Nil means the wall clock.
	clock func() time.Time
}

// now is the time, from the clock this engine was given.
func (e *Engine) now() time.Time {
	if e.clock == nil {
		return time.Now().UTC()
	}
	return e.clock()
}

// WithClock returns an engine that reads the time from clock. For tests about waits, which would
// otherwise have to wait.
func (e *Engine) WithClock(clock func() time.Time) *Engine {
	e.clock = clock
	return e
}

// NewEngine builds one. A nil spend reader means the token ceiling has nothing to read, so a graph
// declaring one is bounded by its transition cap alone; the system wires the real reader in.
func NewEngine(store Store, plane ControlPlane, spend Spend, works Works) *Engine {
	return &Engine{store: store, plane: plane, spend: spend, works: works}
}

// Start begins a run of the newest version of a graph and drives it to a standstill before
// answering. The trigger's payload arrives as the run's opening state, which is what prompt
// templates read.
func (e *Engine) Start(ctx context.Context, graphName, workspace, project string, state map[string]string) (Run, error) {
	run, carrier, graph, err := e.create(ctx, starting{graph: graphName, workspace: workspace, project: project, state: state})
	if err != nil {
		return Run{}, err
	}
	return e.advance(ctx, graph, run, where{carrier: carrier}, Event{Kind: EventStarted})
}

// Begin makes the run, answers with it, and takes its first movement behind that answer.
//
// The first movement declares a job and returns, so this no longer outlives the caller by
// minutes. It is still detached from the caller's context: a command line that has printed the run's
// identifier and exited must not take the run's first movement down with it.
func (e *Engine) Begin(ctx context.Context, graphName, workspace, project string, state map[string]string) (Run, error) {
	run, carrier, graph, err := e.create(ctx, starting{graph: graphName, workspace: workspace, project: project, state: state})
	if err != nil {
		return Run{}, err
	}
	driving := context.WithoutCancel(ctx)
	// The goroutine drives its own copy. A run carries maps, so handing it the same value hands it the
	// same maps, and it then writes to them while the caller is reading the answer: gRPC marshalled a
	// response whose map grew between protobuf's sizing pass and its encoding pass, and refused it with
	// "size mismatch". About one run in six.
	driven := run.copy()
	go func() {
		if _, err := e.advance(driving, graph, driven, where{carrier: carrier}, Event{Kind: EventStarted}); err != nil {
			slog.WarnContext(driving, "a flow run stopped moving", "run", driven.ID, "graph", graphName, "error", err)
		}
	}()
	return run, nil
}

// create reads the graph, declares the job that carries the run, and writes the run's
// first row under it.
//
// The job comes first because the run hangs under it. Its parent is the caller's, read from the
// credential the caller presented: an operator starts a root, and a session that starts a flow puts
// the whole run one level below its own job, which is what makes a run something the depth limit
// bounds.
func (e *Engine) create(ctx context.Context, from starting) (Run, string, Graph, error) {
	graphName := from.graph
	version, definition, err := e.store.LatestFlowGraph(ctx, graphName)
	if err != nil {
		return Run{}, "", Graph{}, fmt.Errorf("flow: start %s: %w", graphName, err)
	}
	graph, err := Parse([]byte(definition))
	if err != nil {
		return Run{}, "", Graph{}, fmt.Errorf("flow: graph %s version %d does not parse, so no run of it can start; fix the file and import the next version: %w", graphName, version, err)
	}
	// A trigger starts a graph whose author said it reacts, and nothing else. Refused rather than
	// run, because a graph that begins with a dispatch begins when a person or a schedule says so,
	// and a trigger starting it would be an automation running for a reason its file does not carry.
	if from.trigger != "" && !graph.StartsOnTrigger() {
		return Run{}, "", Graph{}, fmt.Errorf("the flow %s version %d begins at %s, which is a %s node, so a trigger starts nothing; give the graph a %s node as the node it begins at",
			graphName, version, graph.Start, graph.Nodes[graph.Start].Type, NodeTrigger)
	}
	if e.works == nil {
		return Run{}, "", Graph{}, fmt.Errorf("flow: this system cannot declare job, so it cannot run a flow: a run is carried by a job")
	}

	state := from.state
	if state == nil {
		state = map[string]string{}
	}
	run := Run{
		ID:           newRunID(),
		Workspace:    from.workspace,
		Project:      from.project,
		GraphName:    graphName,
		GraphVersion: version,
		Status:       StatusRunning,
		State:        state,
		Attempts:     map[string]int{},
	}
	labels := map[string]string{labelRun: run.ID, labelGraph: graphName}
	if from.trigger != "" {
		labels[labelTrigger] = from.trigger
	}
	carrier, declared, err := e.works.PrepareJob(ctx, from.under, job.Declaration{
		Workspace: from.workspace, Project: from.project,
		Title: fmt.Sprintf("flow %s version %d", graphName, version),
		Brief: fmt.Sprintf("carries the run of flow %s, version %d. Its steps hang under it, and it "+
			"ends when the run does.", graphName, version),
		Labels: labels,
		// The graph's sentence goes on the job carrying the run, so every step under it carries the
		// same one and every session doing a step is given it above its brief. A run started by a
		// session whose job already serves a different sentence is refused there, which is the rule a
		// tree with two products already keeps.
		Product: graph.Product,
	})
	if err != nil {
		return Run{}, "", Graph{}, fmt.Errorf("flow: start %s: %w", graphName, err)
	}
	// Read off the carrier rather than off the graph, because a run started inside a job tree that
	// already serves a sentence carries that one.
	if carrier.Product != "" {
		run.State[stateProduct] = carrier.Product
	}
	// Held back rather than pending, because a controller must never send this one as a task. It is a
	// parent whose children are outstanding, which is what waiting already means, and the controller's
	// queries pass over it on that.
	carrier.Phase = job.PhaseWaiting

	records := []*job.Event{declared, e.record(carrier, EventRunStarted,
		fmt.Sprintf("run %s of %s version %d", run.ID, graphName, version))}
	if err := e.store.CreateFlowRun(ctx, &run, carrier, records, from.trigger); err != nil {
		return Run{}, "", Graph{}, fmt.Errorf("flow: create run of %s: %w", graphName, err)
	}
	e.exported(ctx, records...)
	return run, carrier.ID, graph, nil
}

// advance feeds one event through the reducer, declares whatever job it asked for, and persists the
// movement. It takes one movement and returns.
//
// It does not wait for the step. Where the reducer asked for a dispatch, the step is written down as
// a job whose parent is the run's own, the run is recorded as working, and a controller
// sends the task. What carries the run on is that job reaching a terminal phase, read off a row by
// the poller, so nothing here holds a call, a goroutine or a container open.
func (e *Engine) advance(ctx context.Context, graph Graph, run Run, at where, event Event) (Run, error) {
	// What the run has cost, read fresh before the movement, so the ceiling is checked against what
	// the model actually charged rather than against a number from the start.
	run.Spent = e.spentBy(ctx, run)

	// The sentence as it stands, kept before the movement rather than compared afterwards. A run
	// carries maps, so the reducer writes into the same state this run holds and the two would read
	// the same however the movement changed it.
	served := run.State[stateProduct]

	next, commands, err := Advance(graph, run, event)
	if err != nil {
		return run, err
	}

	var dispatch *Command
	archive := false
	for i := range commands {
		switch commands[i].Kind {
		case CommandDispatch:
			dispatch = &commands[i]
		case CommandArchive:
			archive = true
		}
	}

	// A run that reached a wait carries its due time into the same write as its position, so it
	// can never be recorded as waiting with nothing to wake it.
	var due DueAt
	if next.Status == StatusWaiting && next.DueIn > 0 {
		when := e.now().Add(next.DueIn)
		due = &when
	}

	// The operator answered the question at the first usable path with a sentence of their own, so the
	// sentence the tree serves is replaced before the next step is declared. It has to be this way
	// round: a step reads what it serves off the job above it as it is declared, so a replacement
	// written afterwards would reach every step but the one the answer was about.
	if at.carrier != "" && next.State[stateProduct] != served {
		if err := e.replaceTheSentence(ctx, at.carrier, next.State[stateProduct]); err != nil {
			return run, fmt.Errorf("flow: run %s was told what it should serve and the sentence could not be replaced, so it has not moved: %w", run.ID, err)
		}
	}

	written := JobWrite{}
	if dispatch != nil {
		declared, record, err := e.declare(ctx, graph, next, at.carrier, *dispatch)
		if err != nil {
			// The system refused this step: too deep, a role the workspace does not hold, a project
			// that has gone. No job was declared, so the run must not walk a success edge on a reply
			// that will never exist. It stops with the system's own sentence, which names what to do.
			next.Status = StatusStopped
			next.Reason = fmt.Sprintf("stopped at %s, which the system refused: %s", dispatch.Node, oneLine(err.Error()))
			dispatch = nil
		} else {
			written.Declared, written.Records = declared, append(written.Records, record)
			next.Status = StatusWorking
		}
	}
	written.Carrier, written.Records = e.carrier(ctx, at.carrier, next, written.Records)

	if err := e.store.AdvanceFlowRun(ctx, &next, Transition{
		Event: event.Kind, Node: next.Node, Dispatch: dispatch, Due: due,
		Answers: at.answers, Job: written,
	}); err != nil {
		// Somebody stopped the run while its step was out, or another poller carried it on first.
		// Neither is a failure: the row is the answer, so it is read back and handed over.
		if errors.Is(err, ErrRunHalted) {
			slog.InfoContext(ctx, "a flow run had already moved on, so this movement was dropped", "run", next.ID)
			if halted, err := e.store.GetFlowRun(ctx, next.ID); err == nil {
				return *halted, nil
			}
			return next, nil
		}
		// The claim on run, node and attempt refused: this exact dispatch was already made, so
		// making it again would spend money twice. The run stays where the store says it is.
		return next, fmt.Errorf("flow: run %s did not move: %w", next.ID, err)
	}
	e.exported(ctx, written.Records...)

	if archive {
		e.archive(ctx, next)
	}
	return next, nil
}

// replaceTheSentence writes the sentence the operator gave onto the job carrying the run, and
// records that it moved.
//
// The record is the point of doing it here rather than in a column update nobody sees. An operator
// reading the tree afterwards wants the question, the answer, and the sentence the rest of the work
// was done against, and the first two are already records on this job.
func (e *Engine) replaceTheSentence(ctx context.Context, id, sentence string) error {
	carrying, err := e.store.GetJob(ctx, id)
	if err != nil {
		return err
	}
	record := e.record(carrying, EventProductReplaced, sentence)
	if _, err := e.store.ReplaceJobProduct(ctx, id, sentence, record); err != nil {
		return err
	}
	e.exported(ctx, record)
	return nil
}

// declare writes down one step as a job: same rules, same tree, same ceilings as anything
// a session declares.
//
// What the node said would prove it worked travels with the declaration rather than being checked
// here. The controller reads it after the task, which is the one place that can see the session the
// job actually ran in, and it is the same check for a step of a flow as for anything else.
func (e *Engine) declare(ctx context.Context, graph Graph, run Run, carrier string, dispatch Command) (*job.Job, *job.Event, error) {
	declaration := job.Declaration{
		Workspace: run.Workspace, Project: run.Project,
		Title: fmt.Sprintf("%s step %s", run.GraphName, dispatch.Node),
		Brief: dispatch.Prompt,
		Role:  dispatch.Role,
		// The mode travels with every step. A session is born in the system's own mode, so this is the
		// only moment anything can say what an automation's tasks may do without asking.
		Mode: graph.Mode,
		Labels: map[string]string{
			labelRun: run.ID, labelGraph: run.GraphName, labelNode: dispatch.Node,
			labelAttempt: fmt.Sprintf("%d", dispatch.Attempt),
		},
	}
	if expect := graph.Nodes[dispatch.Node].Expect; expect != nil {
		declaration.ExpectFile, declaration.ExpectContains = expect.File, expect.Contains
	}
	return e.works.PrepareJob(ctx, carrier, declaration)
}

// carrier is what this movement writes onto the job that carries the run, and the record
// of it where the run reached something worth a record.
//
// The phase follows the run: held back while it is working, waiting or moving, asking while it is
// asking, and ended when the run has ended. So `krewe job show` on a run's own job says where the
// run is, and the answer of the whole run is a field rather than a transcript.
func (e *Engine) carrier(ctx context.Context, id string, run Run, records []*job.Event) (*Carrier, []*job.Event) {
	if id == "" {
		return nil, records
	}
	on := &Carrier{Job: id, Phase: job.PhaseWaiting}
	kind, detail := "", ""
	switch run.Status {
	case StatusAsking:
		on.Phase, on.Question = job.PhaseAsking, run.Question
		kind, detail = EventRunAsked, fmt.Sprintf("at %s: %s", run.Node, run.Question)
	case StatusDone:
		on.Phase, on.Answer = job.PhaseDone, run.State["result.reply"]
		kind, detail = EventRunFinished,
			fmt.Sprintf("done at %s after %d transitions, %d tokens", run.Node, run.Transitions, run.Spent)
	case StatusFailed:
		on.Phase, on.Reason = job.PhaseFailed, run.Reason
		kind, detail = EventRunStopped, oneLine(run.Reason)
	case StatusStopped:
		on.Phase, on.Reason = job.PhaseStopped, run.Reason
		kind, detail = EventRunStopped, oneLine(run.Reason)
	}
	if kind == "" {
		return on, records
	}
	// The row rather than the identifier, because a record carries the workspace, the project and the
	// trace it belongs to, and a record that disagreed with the job it describes is a record nothing
	// can join. A read that fails costs the record and not the movement.
	carrying, err := e.store.GetJob(ctx, id)
	if err != nil {
		slog.WarnContext(ctx, "the job carrying this run could not be read, so the record of this movement is not written",
			"run", run.ID, "job", id, "error", err)
		return on, records
	}
	return on, append(records, e.record(carrying, kind, detail))
}

// Worked carries a run on from the job its step went out as.
//
// This is what replaced holding a dispatch open. The step's answer, its failure or its unmet claim
// arrive as the same event the reducer always took, so advance.go does not know a job
// exists. The run is put back to running first, because working is the engine's word and the pure
// function keeps the four statuses it always had.
func (e *Engine) Worked(ctx context.Context, run Run, step *job.Job) (Run, error) {
	definition, err := e.store.FlowGraph(ctx, run.GraphName, run.GraphVersion)
	if err != nil {
		return run, err
	}
	graph, err := Parse([]byte(definition))
	if err != nil {
		return run, err
	}
	if run.State == nil {
		run.State = map[string]string{}
	}
	// Where the step ran is remembered against its node, so reading a run says which conversation did
	// which step, and the run can put every one of them away at the end.
	if step.Session != "" {
		run.State[sessionKeyPrefix+run.Node] = step.Session
	}
	// The step's session is put away as soon as its job has ended. This is the whole point of the
	// change: the container belongs to the job, the job is over, and a run that then asks
	// a person holds nothing while it waits.
	e.archiveSession(ctx, step.Session)

	run.Status = StatusRunning
	return e.advance(ctx, graph, run, where{carrier: step.Parent, answers: step.ID}, Event{
		Kind:   EventTaskFinished,
		Node:   run.Node,
		Reply:  replyOf(step),
		Failed: step.Phase == job.PhaseFailed,
		// Job halted over a claim it did not meet is the reducer's unmet: the system knows the job
		// did not happen and does not know why, so the run stops rather than branching.
		Unmet: unmetOf(step),
	})
}

// replyOf is what the step said, as the run reads it. Job that failed carries the reason instead,
// because there is no answer and a graph branching on the reply has to have something to read.
func replyOf(step *job.Job) string {
	if step.Phase == job.PhaseFailed {
		return step.Reason
	}
	return step.Answer
}

// unmetOf is the claim the step did not meet, empty where it claimed nothing or the claim held.
func unmetOf(step *job.Job) string {
	if step.Phase == job.PhaseStopped {
		return step.Reason
	}
	return ""
}

// The labels every job a run declares carries, so a reader finds the whole run in the job
// tree without being told an identifier: `krewe job list --label flow.run=<run>`.
const (
	labelRun     = "flow.run"
	labelGraph   = "flow.graph"
	labelNode    = "flow.node"
	labelAttempt = "flow.attempt"
	// labelTrigger is on the job carrying a run that something triggered, and on nothing else, so
	// the tree says why a run nobody started exists.
	labelTrigger = "flow.trigger"
)

// starting is what a new run is made from.
//
// A struct rather than five arguments, because three of them are only ever set by a trigger, and a
// call site passing two empty strings to say "nobody triggered this" reads as an oversight.
type starting struct {
	graph     string
	workspace string
	project   string
	// state is what the run opens knowing: what a trigger carried, or what an operator passed.
	state map[string]string
	// trigger is the row this run answers, marked started in the transaction that writes the run,
	// empty for a run a person or a schedule asked for.
	trigger string
	// under is the job the run's own job hangs under. Empty means the parent comes from
	// the credential the caller presented, which is what a run started by a person wants.
	under string
}

// where is a run's place in the job tree at the moment of a movement: the job that
// carries the run, and the step this movement answers. It is a parameter rather than two fields on
// the run, because advance.go holds the run and the reducer has no business knowing either.
type where struct {
	carrier string
	answers string
}

// exported offers records to the log after they have landed in the store, and does nothing where
// there is nothing to export to. Called after every write rather than inside it, because a record on
// the log that is not in the store is a record nothing can explain.
func (e *Engine) exported(ctx context.Context, records ...*job.Event) {
	if e.works == nil || len(records) == 0 {
		return
	}
	e.works.ExportJob(ctx, records...)
}

// spentBy is what the run has cost, over every session it started. It keeps the last known number
// when there is no reader wired or no session yet: a cost that cannot be read must not silently reset
// a run's spend to zero and hand it a fresh ceiling.
//
// Every session, because a run's steps each have their own, and a ceiling that counted one
// conversation would be a ceiling a graph could walk around by taking another step.
func (e *Engine) spentBy(ctx context.Context, run Run) int64 {
	sessions := run.Sessions()
	if e.spend == nil || len(sessions) == 0 {
		return run.Spent
	}
	var total int64
	for _, id := range sessions {
		total += e.spend.SessionTokens(ctx, id)
	}
	return total
}

// SessionKey is where a run made before its steps were job remembered its own session. Runs no
// longer have one: a step's session belongs to the job that ran it, and is remembered
// against that step's node.
//
// Exported and still read, because a run outlives the reading of it and the runs already in the
// store carry this key.
const SessionKey = "session.id"

// sessionKeyPrefix is where a run remembers the session a step ran in, one key per node. Per node
// rather than one list, so reading a run's state says which step ran where.
const sessionKeyPrefix = "session."

// Sessions are the identifiers of every session this run started.
func (r Run) Sessions() []string { return SessionsIn(r.State) }

// SessionsIn reads the sessions a run started out of its state, in the order of the nodes that
// started them.
//
// Exported because a run is read back as much as it is driven, and whoever is reading one has to be
// able to reach every conversation it had rather than only the first.
func SessionsIn(state map[string]string) []string {
	var out []string
	if id := state[SessionKey]; id != "" {
		out = append(out, id)
	}
	keys := make([]string, 0, len(state))
	for key := range state {
		if key != SessionKey && strings.HasPrefix(key, sessionKeyPrefix) && state[key] != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, state[key])
	}
	return out
}

// archive puts away every session the run started. Each one is normally already away, put there as
// its step ended, so this is the sweep that catches a step whose archive did not land. A run that
// never dispatched has no session, and a session that cannot be archived is logged by the control
// plane's side of the call; neither is a reason to call a finished run anything else.
func (e *Engine) archive(ctx context.Context, run Run) {
	for _, id := range run.Sessions() {
		e.archiveSession(ctx, id)
	}
}

// archiveSession gives one session's container back.
func (e *Engine) archiveSession(ctx context.Context, id string) {
	if id == "" || e.plane == nil {
		return
	}
	_, _ = e.plane.ArchiveSession(ctx, &quaycrewv1.ArchiveSessionRequest{Id: id})
}

// record describes one thing a run did, against the job that carries it.
func (e *Engine) record(carrier *job.Job, kind, detail string) *job.Event {
	return &job.Event{
		ID: newEventID(), Kind: kind, Job: carrier.ID,
		Workspace: carrier.Workspace, Project: carrier.Project,
		Parent: carrier.Parent, Depth: carrier.Depth, Detail: detail,
		TraceID: carrier.TraceID, OccurredAt: time.Now().UTC(),
	}
}

// copy is a run that shares nothing writable with the one it came from.
//
// A struct copy is not enough. State and Attempts are maps, so a copied Run points at the same two,
// and a goroutine advancing one writes into the other. Both are made rather than left nil, because
// the run that gets driven writes into them on its first transition.
func (r Run) copy() Run {
	copied := r
	copied.State = make(map[string]string, len(r.State))
	for key, value := range r.State {
		copied.State[key] = value
	}
	copied.Attempts = make(map[string]int, len(r.Attempts))
	for key, value := range r.Attempts {
		copied.Attempts[key] = value
	}
	return copied
}

// SessionHandle names a run, in the shape its own session used to be named: the graph and a short
// run identifier. Nothing is dispatched to it now, because a step's session is named after the
// job that ran it, and it is kept because a listing and a console still read a run by it.
func (r Run) SessionHandle() string {
	short := r.ID
	if len(short) > 8 {
		short = short[:8]
	}
	return r.GraphName + "-" + short
}

// newRunID mints a run identifier, the same shape the store uses for everything else.
func newRunID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// newEventID mints an identifier for a record. Minted here rather than by the store, because writing
// the same record twice has to leave one row and the identifier is what makes that possible.
func newEventID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// oneLine keeps a reason readable on a listing: a record is read on one line beside others.
func oneLine(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	const most = 200
	if len(flat) <= most {
		return flat
	}
	return flat[:most] + "..."
}
