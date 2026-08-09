package flow

import (
	"fmt"
	"strings"
)

// Run statuses. Failed is a run the graph could not carry any further, which is different from a
// turn that failed: a graph that branches on result.failed handles the second without ever being
// the first.
const (
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
	// StatusStopped is a run brought to a halt rather than one that finished: it hit a limit, or
	// somebody stopped it. A run that went quiet and a run that was halted must never read the
	// same, which is why this is its own word and why it carries a reason.
	StatusStopped = "stopped"
)

// Event kinds. Started begins a run; a finished turn is the result of the one dispatch the run was
// waiting on.
const (
	EventStarted      = "started"
	EventTurnFinished = "turn.finished"
)

// Command kinds. Dispatch sends a turn to the run's own thread; archive puts that thread away when
// the run ends, because a finished run must not leave a container behind.
const (
	CommandDispatch = "dispatch"
	CommandArchive  = "archive"
)

// Run is one instance of a graph: where it is, what it knows, and which graph version it is pinned
// to, so editing a file cannot change an automation that is halfway through.
type Run struct {
	ID           string
	Workspace    string
	Project      string
	GraphName    string
	GraphVersion int
	// Node is where the run sits. Empty before the started event places it.
	Node   string
	Status string
	// State is the run's small memory: what turns answered, what the trigger carried, what a
	// prompt template reads.
	State map[string]string
	// Attempts counts dispatches per node, which is one third of the idempotency key run, node and
	// attempt.
	Attempts map[string]int
	// Transitions is how many movements the run has taken, counted against the graph's cap so a
	// cycling graph terminates on its own.
	Transitions int
	// Spent is what the run's own conversation has cost in tokens, read from the model's transcript
	// before each dispatch and checked against the graph's ceiling.
	Spent int64
	// Reason says why a stopped run stopped. Empty on a run that is running or that finished.
	Reason string
}

// Event is one thing that happened to a run.
type Event struct {
	Kind string
	// Node is the node the event answers, so a late duplicate for a node the run already left is
	// refused rather than moving it twice.
	Node   string
	Reply  string
	Failed bool
}

// Command is one thing the engine must do on the run's behalf. The reducer never does it: it
// touches no Docker, no Postgres and no model, which is what keeps it a table test.
type Command struct {
	Kind    string
	Node    string
	Attempt int
	Prompt  string
}

// Advance is the whole of the flow logic: a pure function from a run and an event to the next run
// and the commands to carry out.
func Advance(graph Graph, run Run, event Event) (Run, []Command, error) {
	if run.Status != StatusRunning {
		return run, nil, fmt.Errorf("flow: run %s is %s, and a run that ended does not move", run.ID, run.Status)
	}
	// The brakes are checked before the movement rather than after it, so the dispatch that would
	// cross a line is never made and never paid for.
	if stopped, halted := brake(graph, run); halted {
		return stopped, nil, nil
	}
	run.Transitions++
	if run.State == nil {
		run.State = map[string]string{}
	}
	if run.Attempts == nil {
		run.Attempts = map[string]int{}
	}

	switch event.Kind {
	case EventStarted:
		if run.Node != "" {
			return run, nil, fmt.Errorf("flow: run %s already started, at node %s", run.ID, run.Node)
		}
		return settle(graph, run, graph.Start)
	case EventTurnFinished:
		if event.Node != run.Node {
			return run, nil, fmt.Errorf("flow: a result arrived for node %s while run %s sits on %s; a late duplicate must not move a run twice", event.Node, run.ID, run.Node)
		}
		node, known := graph.Nodes[run.Node]
		if !known || node.Type != NodeDispatch {
			return run, nil, fmt.Errorf("flow: run %s sits on %s, which is not a dispatch, so no turn result belongs to it", run.ID, run.Node)
		}
		run.State["result.reply"] = event.Reply
		run.State["result.failed"] = fmt.Sprintf("%t", event.Failed)
		next, err := follow(graph, run.Node, "")
		if err != nil {
			return run, nil, err
		}
		return settle(graph, run, next)
	default:
		return run, nil, fmt.Errorf("flow: run %s was handed an event of kind %q", run.ID, event.Kind)
	}
}

// brake stops a run that has reached a limit, saying which one in words the operator can act on.
//
// Both limits are the same idea: an automation runs with nobody watching, so it has to be able to
// stop itself. The transition cap catches a graph that cycles; the token ceiling catches a graph
// that does not cycle but whose turns are expensive.
func brake(graph Graph, run Run) (Run, bool) {
	if run.Transitions >= graph.Limits.Transitions {
		run.Status = StatusStopped
		run.Reason = fmt.Sprintf("stopped after %d transitions, which is the cap graph %s declares; raise limits.transitions if the automation genuinely needs more steps",
			run.Transitions, graph.Name)
		return run, true
	}
	if graph.Limits.Tokens > 0 && run.Spent >= graph.Limits.Tokens {
		run.Status = StatusStopped
		run.Reason = fmt.Sprintf("stopped having spent %d tokens against the ceiling of %d that graph %s declares; raise limits.tokens if this is the cost of the work",
			run.Spent, graph.Limits.Tokens, graph.Name)
		return run, true
	}
	return run, false
}

// settle places the run on a node and keeps moving while the node is pure, so a chain of choices
// costs one advance. It stops on a dispatch, whose command it returns, or on done. The loop is
// bounded by the graph's own size: a pure cycle would otherwise spin forever, and a graph that
// loops without a dispatch in the loop is a graph that computes nothing.
func settle(graph Graph, run Run, at string) (Run, []Command, error) {
	for range len(graph.Nodes) + 1 {
		if at == DoneNode {
			run.Node = DoneNode
			run.Status = StatusDone
			return run, []Command{{Kind: CommandArchive}}, nil
		}
		node, known := graph.Nodes[at]
		if !known {
			return run, nil, fmt.Errorf("flow: run %s was routed to %q, which is not a node", run.ID, at)
		}
		run.Node = at
		switch node.Type {
		case NodeDispatch:
			run.Attempts[at]++
			return run, []Command{{
				Kind:    CommandDispatch,
				Node:    at,
				Attempt: run.Attempts[at],
				Prompt:  render(node.Prompt, run.State),
			}}, nil
		case NodeChoice:
			answer := "true"
			for key, want := range node.On {
				if run.State[key] != want {
					answer = "false"
				}
			}
			next, err := follow(graph, at, answer)
			if err != nil {
				return run, nil, err
			}
			at = next
		default:
			return run, nil, fmt.Errorf("flow: node %s has type %q, which the reducer cannot advance", at, node.Type)
		}
	}
	return run, nil, fmt.Errorf("flow: run %s cycled through %d pure nodes without reaching a dispatch or the end", run.ID, len(graph.Nodes)+1)
}

// follow finds the one edge out of a node with the given label.
func follow(graph Graph, from, when string) (string, error) {
	for _, edge := range graph.Edges {
		if edge.From == from && edge.When == when {
			return edge.To, nil
		}
	}
	return "", fmt.Errorf("flow: node %s has no edge labeled %q", from, when)
}

// render fills {{key}} from the run's state. A key the state does not hold stays as typed, so a
// misspelling is visible in the thread's own transcript rather than silently blank.
func render(prompt string, state map[string]string) string {
	out := prompt
	for key, value := range state {
		out = strings.ReplaceAll(out, "{{"+key+"}}", value)
	}
	return out
}
