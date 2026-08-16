package flow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
)

// Store is what the engine needs from the database: a graph to run, and a run whose every
// transition lands in the same transaction as the state it describes. Implemented by the crew's
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
	// else, so a crew with a thousand finished runs and one waiting does one row's work per tick.
	DueFlowRuns(ctx context.Context, now time.Time) ([]*Run, error)
	// CreateFlowRun writes a fresh run. Its transitions are the events; creation is the row itself.
	CreateFlowRun(ctx context.Context, run *Run) error
	// AdvanceFlowRun writes the run's next position, appends the transition that took it there,
	// and when the transition dispatched, claims the dispatch's key. One transaction, so there is
	// no gap for a crash to hide in: either the run moved and the claim exists, or neither.
	// A dispatch key already claimed refuses the whole transition.
	//
	// It moves only a run the database still holds as running, which is what makes stopping one
	// take effect: the operator's stop lands, and the engine's next write finds the run halted and
	// is refused with ErrRunHalted rather than quietly setting it back to running.
	AdvanceFlowRun(ctx context.Context, run *Run, transition Transition) error
}

// ErrRunHalted is what AdvanceFlowRun answers when the run it was asked to move is no longer
// running: somebody stopped it while the engine was waiting on a turn.
var ErrRunHalted = errors.New("flow: the run is no longer running")

// Schedule is a graph the crew starts on its own, in one project, every so often.
type Schedule struct {
	GraphName string
	Project   string
	Workspace string
	Every     time.Duration
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
	// Dispatch is set when the transition asked for a turn, and its key of run, node and attempt
	// is what makes sending the same turn twice impossible.
	Dispatch *Command
	// Due is when the run should be looked at again, set only when this movement reached a wait.
	Due DueAt
}

// RecordedTransition is a transition as read back: the order it happened in, what arrived, and
// where the run sat afterwards. The audit record and the replay record are these rows.
type RecordedTransition struct {
	Seq   int
	Event string
	Node  string
}

// Spend is what a run's own thread has cost so far, in tokens. The engine reads it before each
// movement and hands it to the reducer, which is the only way a ceiling can stop a turn before it
// is paid for rather than after.
//
// It takes the thread rather than a conversation because resolving one to the other is the crew's
// job, not the engine's: the thread row knows which conversation it is having and which workspace
// keeps the transcript. An implementation that cannot tell answers zero.
type Spend interface {
	ThreadTokens(ctx context.Context, thread string) int64
}

// ControlPlane is what the engine may do on a run's behalf. It is the same service every other
// caller speaks to, deliberately: the engine holds no privileged road into anything.
type ControlPlane interface {
	Dispatch(ctx context.Context, req *quaycrewv1.DispatchRequest) (*quaycrewv1.DispatchResponse, error)
	ArchiveThread(ctx context.Context, req *quaycrewv1.ArchiveThreadRequest) (*quaycrewv1.ArchiveThreadResponse, error)
}

// Prover answers what a dispatch node said would show its turn did the work.
//
// Deliberately not one of the control plane's own calls. Reading a thread's files is not something a
// caller may ask the crew to do, and a graph is written by whoever may import one, so a graph must
// not become a road to it. This asks one question, about one thread, with a path the parser has
// already refused if it climbs anywhere.
//
// An implementation that cannot answer says so, and the run stops. A check that quietly passes when
// it could not be run is the same false green as no check at all.
type Prover interface {
	// ThreadHolds says whether path exists in the thread's own working directory.
	ThreadHolds(ctx context.Context, thread, path string) (bool, error)
}

// Engine drives runs: it loads the graph, calls the pure reducer, persists every transition, and
// carries out the commands the reducer returned. It blocks for the turns it dispatches, one at a
// time per run, which is the shape the graph forces anyway: a run has one current node.
type Engine struct {
	store  Store
	plane  ControlPlane
	spend  Spend
	prover Prover
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
// declaring one is bounded by its transition cap alone; the crew wires the real reader in. A nil
// prover means a graph that says a file proves its work stops rather than being believed.
func NewEngine(store Store, plane ControlPlane, spend Spend, prover Prover) *Engine {
	return &Engine{store: store, plane: plane, spend: spend, prover: prover}
}

// Start begins a run of the newest version of a graph and drives it to a standstill before
// answering. The trigger's payload arrives as the run's opening state, which is what prompt
// templates read.
func (e *Engine) Start(ctx context.Context, graphName, workspace, project string, state map[string]string) (Run, error) {
	run, graph, err := e.create(ctx, graphName, workspace, project, state)
	if err != nil {
		return Run{}, err
	}
	return e.advance(ctx, graph, run, Event{Kind: EventStarted})
}

// Begin makes the run, answers with it, and drives it behind that answer.
//
// A run dispatches turns and a turn takes as long as the model takes, so whoever asked for the run
// gets its identifier now and reads its position back later. The driving context is detached from
// the caller's: a command line that has printed the run's identifier and exited must not take the
// run down with it.
func (e *Engine) Begin(ctx context.Context, graphName, workspace, project string, state map[string]string) (Run, error) {
	run, graph, err := e.create(ctx, graphName, workspace, project, state)
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
		if _, err := e.advance(driving, graph, driven, Event{Kind: EventStarted}); err != nil {
			slog.WarnContext(driving, "a flow run stopped moving", "run", driven.ID, "graph", graphName, "error", err)
		}
	}()
	return run, nil
}

// create reads the graph, pins the run to its version, and writes the run's first row.
func (e *Engine) create(ctx context.Context, graphName, workspace, project string, state map[string]string) (Run, Graph, error) {
	version, definition, err := e.store.LatestFlowGraph(ctx, graphName)
	if err != nil {
		return Run{}, Graph{}, fmt.Errorf("flow: start %s: %w", graphName, err)
	}
	graph, err := Parse([]byte(definition))
	if err != nil {
		return Run{}, Graph{}, fmt.Errorf("flow: graph %s version %d no longer parses, which should have been refused at import: %w", graphName, version, err)
	}

	if state == nil {
		state = map[string]string{}
	}
	run := Run{
		ID:           newRunID(),
		Workspace:    workspace,
		Project:      project,
		GraphName:    graphName,
		GraphVersion: version,
		Status:       StatusRunning,
		State:        state,
		Attempts:     map[string]int{},
	}
	if err := e.store.CreateFlowRun(ctx, &run); err != nil {
		return Run{}, Graph{}, fmt.Errorf("flow: create run of %s: %w", graphName, err)
	}
	return run, graph, nil
}

// advance feeds one event through the reducer, persists the transition, and carries out what came
// back, feeding each dispatch's result straight back in. The loop ends when the reducer returns no
// dispatch: the run is done, or waiting on something no slice has built yet.
func (e *Engine) advance(ctx context.Context, graph Graph, run Run, event Event) (Run, error) {
	for {
		// What the run has cost, read fresh before every movement, so the ceiling is checked
		// against what the model actually charged rather than against a number from the start.
		run.Spent = e.spentBy(ctx, run)

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
			at := e.now().Add(next.DueIn)
			due = &at
		}

		if err := e.store.AdvanceFlowRun(ctx, &next, Transition{
			Event: event.Kind, Node: next.Node, Dispatch: dispatch, Due: due,
		}); err != nil {
			// Somebody stopped the run while this was waiting on a turn. That is not a failure:
			// the stop is the answer, and the run keeps the reason it was stopped with.
			if errors.Is(err, ErrRunHalted) {
				slog.InfoContext(ctx, "a flow run was stopped while it was working", "run", next.ID)
				if halted, err := e.store.GetFlowRun(ctx, next.ID); err == nil {
					return *halted, nil
				}
				return next, nil
			}
			// The claim on run, node and attempt refused: this exact dispatch was already made, so
			// making it again would spend money twice. The run stays where the store says it is.
			return next, fmt.Errorf("flow: run %s did not move: %w", next.ID, err)
		}
		run = next

		if archive {
			e.archive(ctx, run)
		}
		if dispatch == nil {
			return run, nil
		}

		// The mode travels with every dispatch, not only the first. The control plane applies it
		// before the sandbox is built, and a run's thread is made by its first dispatch, so this is
		// the only moment anything can say what an automation's turns are allowed to do.
		resp, err := e.plane.Dispatch(ctx, &quaycrewv1.DispatchRequest{
			Project:        run.Project,
			Handle:         run.ThreadHandle(),
			Text:           dispatch.Prompt,
			PermissionMode: graph.Mode,
		})
		if err != nil {
			event = Event{Kind: EventTurnFinished, Node: dispatch.Node, Failed: true, Reply: err.Error()}
			continue
		}
		run.State[ThreadKey] = resp.GetId()
		event = Event{
			Kind:  EventTurnFinished,
			Node:  dispatch.Node,
			Reply: resp.GetReply(),
			Unmet: e.unmet(ctx, graph.Nodes[dispatch.Node], run, resp.GetReply()),
		}
	}
}

// unmet is what the node said would show its turn did the work, where that is not there. Empty means
// the node claimed nothing, or the claim held.
//
// Checked here rather than in the reducer because it reads the world, and read after the turn rather
// than described by it: the model reporting on its own work is the thing this exists to stop.
func (e *Engine) unmet(ctx context.Context, node Node, run Run, reply string) string {
	if node.Expect == nil {
		return ""
	}
	if carries := node.Expect.Contains; carries != "" && !strings.Contains(reply, carries) {
		return fmt.Sprintf("the reply does not carry %q", carries)
	}
	path := node.Expect.File
	if path == "" {
		return ""
	}
	if e.prover == nil {
		return fmt.Sprintf("%s could not be checked: this crew cannot read a thread's files", path)
	}
	held, err := e.prover.ThreadHolds(ctx, run.State[ThreadKey], path)
	if err != nil {
		return fmt.Sprintf("%s could not be checked: %v", path, err)
	}
	if !held {
		return fmt.Sprintf("%s is not in the run's thread", path)
	}
	return ""
}

// spentBy is what the run's thread has cost. It keeps the last known number when there is no reader
// wired or no thread yet: a cost that cannot be read must not silently reset a run's spend to zero
// and hand it a fresh ceiling.
func (e *Engine) spentBy(ctx context.Context, run Run) int64 {
	if e.spend == nil || run.State[ThreadKey] == "" {
		return run.Spent
	}
	return e.spend.ThreadTokens(ctx, run.State[ThreadKey])
}

// ThreadKey is where the run remembers its thread's identifier, learned from the first dispatch.
// The handle is the run's own by construction; the identifier is what archiving needs.
//
// Exported because a run outlives the reading of it: the thread is archived when the run ends, and
// whoever reads the run afterwards has to be told where its history is.
const ThreadKey = "thread.id"

// archive puts the run's thread away. A run that never dispatched has no thread, and a thread that
// cannot be archived is logged by the control plane's side of the call; neither is a reason to call
// a finished run anything else.
func (e *Engine) archive(ctx context.Context, run Run) {
	id := run.State[ThreadKey]
	if id == "" {
		return
	}
	_, _ = e.plane.ArchiveThread(ctx, &quaycrewv1.ArchiveThreadRequest{Id: id})
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

// ThreadHandle names the run's own thread: the graph and a short run identifier, so a listing reads
// as what the run is doing. The thread is created by the first dispatch and continued by every one
// after, which is what lets a restarted run land back in its own conversation.
func (r Run) ThreadHandle() string {
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
