package flow

import (
	"strings"
	"testing"
)

// A graph that cycles, which is the shape the brakes exist for: without a cap this run dispatches
// until somebody notices the bill.
const loopGraph = `
name: loop
version: 1
mode: edits
limits:
  transitions: 5
nodes:
  begin: { type: dispatch, prompt: "begin" }
  more:  { type: choice, on: { result.failed: "false" } }
  again: { type: dispatch, prompt: "again" }
edges:
  - [begin, more]
  - [more, again, "true"]
  - [more, done, "false"]
  - [again, more]
`

func TestAGraphDeclaresItsLimits(t *testing.T) {
	graph, err := Parse([]byte(loopGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if graph.Limits.Transitions != 5 {
		t.Fatalf("the graph allows %d transitions, want 5", graph.Limits.Transitions)
	}
}

// A graph that declares no cap still gets one. An automation dispatches tasks with nobody watching,
// so the default has to be a number rather than "unbounded until somebody remembers".
func TestAGraphWithNoDeclaredCapGetsTheDefault(t *testing.T) {
	graph, err := Parse([]byte(`
name: plain
version: 1
mode: edits
nodes:
  say: { type: dispatch, prompt: "hello" }
edges:
  - [say, done]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if graph.Limits.Transitions != DefaultTransitions {
		t.Fatalf("a graph with no limits allows %d transitions, want the default %d",
			graph.Limits.Transitions, DefaultTransitions)
	}
	// Against a number rather than against the constant itself, which would be the constant
	// compared to itself and would stay green if somebody set the default to a billion. The point
	// of a default is that it is a real bound: high enough that no reasonable graph meets it by
	// accident, low enough that a runaway is a bill somebody shrugs at.
	if DefaultTransitions < 10 || DefaultTransitions > 1000 {
		t.Fatalf("the default cap is %d, which is not a bound anybody is protected by", DefaultTransitions)
	}
}

func TestACapOfZeroOrLessIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: nope
version: 1
mode: edits
limits:
  transitions: 0
nodes:
  say: { type: dispatch, prompt: "hello" }
edges:
  - [say, done]
`))
	if err == nil {
		t.Fatal("a cap of zero parsed, and a run of it could never take its first step")
	}
}

// The cap is what stops a cycling graph. The run reaches it, stops, and says so: a run that went
// quiet and one that was halted must not read the same.
func TestACyclingRunStopsAtItsCap(t *testing.T) {
	graph, err := Parse([]byte(loopGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	run := Run{ID: "r1", Status: StatusRunning, State: map[string]string{}, Attempts: map[string]int{}}

	next, _, err := Advance(graph, run, Event{Kind: EventStarted})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	// Every task works, so the choice keeps taking the edge back to the dispatch.
	for range 20 {
		if next.Status != StatusRunning {
			break
		}
		next, _, err = Advance(graph, next, Event{Kind: EventTaskFinished, Node: next.Node, Reply: "ok"})
		if err != nil {
			t.Fatalf("Advance: %v", err)
		}
	}

	if next.Status != StatusStopped {
		t.Fatalf("the run is %q after cycling, want it stopped at its cap", next.Status)
	}
	if !strings.Contains(next.Reason, "transitions") {
		t.Fatalf("the run stopped saying %q, want it to name the cap it hit", next.Reason)
	}
	if next.Transitions > graph.Limits.Transitions {
		t.Fatalf("the run took %d transitions, want no more than %d", next.Transitions, graph.Limits.Transitions)
	}
}

// A stopped run dispatches nothing more. The cap is worth nothing if the command that costs money
// still comes back with it.
func TestAStoppedRunReturnsNoDispatch(t *testing.T) {
	graph, err := Parse([]byte(loopGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	run := Run{
		ID: "r1", Node: "begin", Status: StatusRunning,
		State: map[string]string{}, Attempts: map[string]int{},
		Transitions: graph.Limits.Transitions,
	}

	next, commands, err := Advance(graph, run, Event{Kind: EventTaskFinished, Node: "begin", Reply: "ok"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if next.Status != StatusStopped {
		t.Fatalf("the run is %q, want stopped", next.Status)
	}
	for _, command := range commands {
		if command.Kind == CommandDispatch {
			t.Fatalf("a run stopped at its cap still asked for a dispatch: %+v", command)
		}
	}
}

// A run under its ceiling carries on; over it, it stops before the dispatch rather than after, so
// the task that would have crossed the line is never paid for.
func TestARunStopsBeforeTheDispatchThatWouldCrossItsCeiling(t *testing.T) {
	graph, err := Parse([]byte(`
name: costly
version: 1
mode: edits
limits:
  tokens: 1000
nodes:
  one: { type: dispatch, prompt: "one" }
  two: { type: dispatch, prompt: "two" }
edges:
  - [one, two]
  - [two, done]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if graph.Limits.Tokens != 1000 {
		t.Fatalf("the graph allows %d tokens, want 1000", graph.Limits.Tokens)
	}

	run := Run{
		ID: "r1", Node: "one", Status: StatusRunning,
		State: map[string]string{}, Attempts: map[string]int{},
		Spent: 1200,
	}
	next, commands, err := Advance(graph, run, Event{Kind: EventTaskFinished, Node: "one", Reply: "ok"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if next.Status != StatusStopped {
		t.Fatalf("the run is %q having spent 1200 of 1000, want stopped", next.Status)
	}
	if !strings.Contains(next.Reason, "1200") || !strings.Contains(next.Reason, "1000") {
		t.Fatalf("the run stopped saying %q, want it to name what was spent and what was allowed", next.Reason)
	}
	for _, command := range commands {
		if command.Kind == CommandDispatch {
			t.Fatal("a run over its ceiling still asked for a dispatch")
		}
	}
}

// A graph with no ceiling declared is not capped on tokens, only on transitions. Tokens are a
// number the operator has to choose, because what is reasonable differs per automation, and
// inventing one would either stop real work or protect nothing.
func TestNoDeclaredCeilingMeansNoTokenCeiling(t *testing.T) {
	graph, err := Parse([]byte(`
name: plain
version: 1
mode: edits
nodes:
  say: { type: dispatch, prompt: "hello" }
edges:
  - [say, done]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if graph.Limits.Tokens != 0 {
		t.Fatalf("a graph with no ceiling allows %d tokens, want 0 meaning none", graph.Limits.Tokens)
	}

	run := Run{
		ID: "r1", Status: StatusRunning, State: map[string]string{}, Attempts: map[string]int{},
		Spent: 999_999_999,
	}
	next, commands, err := Advance(graph, run, Event{Kind: EventStarted})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if next.Status != StatusRunning {
		t.Fatalf("a run with no declared ceiling is %q, want it still running", next.Status)
	}
	if len(commands) != 1 || commands[0].Kind != CommandDispatch {
		t.Fatalf("commands are %+v, want the dispatch", commands)
	}
}
