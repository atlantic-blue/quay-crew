package flow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

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
	// LatestFlowGraph returns the newest version of a graph and its definition.
	LatestFlowGraph(ctx context.Context, name string) (int, string, error)
	// CreateFlowRun writes a fresh run. Its transitions are the events; creation is the row itself.
	CreateFlowRun(ctx context.Context, run *Run) error
	// AdvanceFlowRun writes the run's next position, appends the transition that took it there,
	// and when the transition dispatched, claims the dispatch's key. One transaction, so there is
	// no gap for a crash to hide in: either the run moved and the claim exists, or neither.
	// A dispatch key already claimed refuses the whole transition.
	AdvanceFlowRun(ctx context.Context, run *Run, transition Transition) error
}

// Transition is one movement of a run, as recorded: what arrived, where the run now sits, and what
// the engine was told to do about it.
type Transition struct {
	Event string
	Node  string
	// Dispatch is set when the transition asked for a turn, and its key of run, node and attempt
	// is what makes sending the same turn twice impossible.
	Dispatch *Command
}

// RecordedTransition is a transition as read back: the order it happened in, what arrived, and
// where the run sat afterwards. The audit record and the replay record are these rows.
type RecordedTransition struct {
	Seq   int
	Event string
	Node  string
}

// ControlPlane is what the engine may do on a run's behalf. It is the same service every other
// caller speaks to, deliberately: the engine holds no privileged road into anything.
type ControlPlane interface {
	Dispatch(ctx context.Context, req *quaycrewv1.DispatchRequest) (*quaycrewv1.DispatchResponse, error)
	ArchiveThread(ctx context.Context, req *quaycrewv1.ArchiveThreadRequest) (*quaycrewv1.ArchiveThreadResponse, error)
}

// Engine drives runs: it loads the graph, calls the pure reducer, persists every transition, and
// carries out the commands the reducer returned. It blocks for the turns it dispatches, one at a
// time per run, which is the shape the graph forces anyway: a run has one current node.
type Engine struct {
	store Store
	plane ControlPlane
}

// NewEngine builds one.
func NewEngine(store Store, plane ControlPlane) *Engine {
	return &Engine{store: store, plane: plane}
}

// Start begins a run of the newest version of a graph and drives it until it needs something that
// has not happened yet or it ends. The trigger's payload arrives as the run's opening state, which
// is what prompt templates read.
func (e *Engine) Start(ctx context.Context, graphName, workspace, project string, state map[string]string) (Run, error) {
	version, definition, err := e.store.LatestFlowGraph(ctx, graphName)
	if err != nil {
		return Run{}, fmt.Errorf("flow: start %s: %w", graphName, err)
	}
	graph, err := Parse([]byte(definition))
	if err != nil {
		return Run{}, fmt.Errorf("flow: graph %s version %d no longer parses, which should have been refused at import: %w", graphName, version, err)
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
		return Run{}, fmt.Errorf("flow: create run of %s: %w", graphName, err)
	}
	return e.advance(ctx, graph, run, Event{Kind: EventStarted})
}

// advance feeds one event through the reducer, persists the transition, and carries out what came
// back, feeding each dispatch's result straight back in. The loop ends when the reducer returns no
// dispatch: the run is done, or waiting on something no slice has built yet.
func (e *Engine) advance(ctx context.Context, graph Graph, run Run, event Event) (Run, error) {
	for {
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

		if err := e.store.AdvanceFlowRun(ctx, &next, Transition{
			Event: event.Kind, Node: next.Node, Dispatch: dispatch,
		}); err != nil {
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

		resp, err := e.plane.Dispatch(ctx, &quaycrewv1.DispatchRequest{
			Project: run.Project,
			Handle:  run.ThreadHandle(),
			Text:    dispatch.Prompt,
		})
		if err != nil {
			event = Event{Kind: EventTurnFinished, Node: dispatch.Node, Failed: true, Reply: err.Error()}
			continue
		}
		run.State[threadKey] = resp.GetId()
		event = Event{Kind: EventTurnFinished, Node: dispatch.Node, Reply: resp.GetReply()}
	}
}

// threadKey is where the run remembers its thread's identifier, learned from the first dispatch.
// The handle is the run's own by construction; the identifier is what archiving needs.
const threadKey = "thread.id"

// archive puts the run's thread away. A run that never dispatched has no thread, and a thread that
// cannot be archived is logged by the control plane's side of the call; neither is a reason to call
// a finished run anything else.
func (e *Engine) archive(ctx context.Context, run Run) {
	id := run.State[threadKey]
	if id == "" {
		return
	}
	_, _ = e.plane.ArchiveThread(ctx, &quaycrewv1.ArchiveThreadRequest{Id: id})
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
