package flow

import (
	"strings"
	"testing"
	"time"
)

// A graph that waits between two turns, which is the shape most real automations have: do a thing,
// leave it a while, look again.
const waitingGraph = `
name: patient
version: 1
nodes:
  ask:   { type: dispatch, prompt: "start the build" }
  pause: { type: wait, for: 10m }
  check: { type: dispatch, prompt: "is it done" }
edges:
  - [ask, pause]
  - [pause, check]
  - [check, done]
`

func TestAWaitNodeParsesItsDuration(t *testing.T) {
	graph, err := Parse([]byte(waitingGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := graph.Nodes["pause"].For; got != 10*time.Minute {
		t.Fatalf("the wait is %s, want ten minutes", got)
	}
}

func TestAWaitWithNoDurationIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: forever
version: 1
nodes:
  ask:   { type: dispatch, prompt: "go" }
  pause: { type: wait }
edges:
  - [ask, pause]
  - [pause, done]
`))
	if err == nil {
		t.Fatal("a wait with no duration parsed, and a run would sit on it forever")
	}
	if !strings.Contains(err.Error(), "for") {
		t.Errorf("the refusal says %q, want it to name the field that is missing", err)
	}
}

func TestAWaitWithAnUnreadableDurationIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: nonsense
version: 1
nodes:
  ask:   { type: dispatch, prompt: "go" }
  pause: { type: wait, for: "soon" }
edges:
  - [ask, pause]
  - [pause, done]
`))
	if err == nil {
		t.Fatal("a wait of \"soon\" parsed")
	}
}

// Reaching a wait puts the run down rather than pushing on: it answers with when to come back and
// asks for no dispatch, which is what keeps a waiting run free rather than a goroutine holding a
// timer.
func TestReachingAWaitPutsTheRunDown(t *testing.T) {
	graph, err := Parse([]byte(waitingGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	run := Run{ID: "r1", Node: "ask", Status: StatusRunning, State: map[string]string{}, Attempts: map[string]int{}}

	next, commands, err := Advance(graph, run, Event{Kind: EventTurnFinished, Node: "ask", Reply: "started"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if next.Status != StatusWaiting {
		t.Fatalf("the run is %q on node %q, want it waiting", next.Status, next.Node)
	}
	if next.Node != "pause" {
		t.Fatalf("the run waits on %q, want the wait node", next.Node)
	}
	if next.DueIn != 10*time.Minute {
		t.Fatalf("the run says come back in %s, want ten minutes", next.DueIn)
	}
	for _, command := range commands {
		if command.Kind == CommandDispatch {
			t.Fatal("a run that reached a wait still asked for a dispatch")
		}
	}
}

// When its time comes the run carries on from the node after the wait.
func TestAWaitThatIsDueCarriesOn(t *testing.T) {
	graph, err := Parse([]byte(waitingGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	run := Run{ID: "r1", Node: "pause", Status: StatusWaiting, State: map[string]string{}, Attempts: map[string]int{}}

	next, commands, err := Advance(graph, run, Event{Kind: EventDue, Node: "pause"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if next.Status != StatusRunning {
		t.Fatalf("the run is %q, want it running again", next.Status)
	}
	if next.Node != "check" {
		t.Fatalf("the run carried on to %q, want the node after the wait", next.Node)
	}
	if len(commands) != 1 || commands[0].Kind != CommandDispatch || commands[0].Prompt != "is it done" {
		t.Fatalf("commands are %+v, want the dispatch after the wait", commands)
	}
}

// A due event for a wait the run is not sitting on is refused, the same way a stale turn result is:
// a poller that fired twice must not move a run twice.
func TestADueEventForAnotherNodeIsRefused(t *testing.T) {
	graph, err := Parse([]byte(waitingGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	run := Run{ID: "r1", Node: "check", Status: StatusRunning, State: map[string]string{}, Attempts: map[string]int{}}

	if _, _, err := Advance(graph, run, Event{Kind: EventDue, Node: "pause"}); err == nil {
		t.Fatal("a due event for a node the run had left advanced it")
	}
}

// A waiting run is still a live run: it can be stopped, and it counts its transitions, so a graph
// that cycles through a wait is bounded like any other.
func TestAWaitingRunStillCountsTowardsItsCap(t *testing.T) {
	graph, err := Parse([]byte(`
name: patient-loop
version: 1
limits:
  transitions: 4
nodes:
  begin: { type: dispatch, prompt: "go" }
  pause: { type: wait, for: 1s }
  more:  { type: choice, on: { result.failed: "false" } }
  again: { type: dispatch, prompt: "again" }
edges:
  - [begin, pause]
  - [pause, more]
  - [more, again, "true"]
  - [more, done, "false"]
  - [again, pause]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	run := Run{ID: "r1", Status: StatusRunning, State: map[string]string{}, Attempts: map[string]int{}}
	next, _, err := Advance(graph, run, Event{Kind: EventStarted})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	for range 20 {
		switch next.Status {
		case StatusRunning:
			next, _, err = Advance(graph, next, Event{Kind: EventTurnFinished, Node: next.Node, Reply: "ok"})
		case StatusWaiting:
			next, _, err = Advance(graph, next, Event{Kind: EventDue, Node: next.Node})
		default:
			goto done
		}
		if err != nil {
			t.Fatalf("Advance: %v", err)
		}
	}
done:
	if next.Status != StatusStopped {
		t.Fatalf("the run is %q after cycling through a wait, want stopped at its cap", next.Status)
	}
}
