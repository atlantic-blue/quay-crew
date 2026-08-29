package flow

import (
	"strings"
	"testing"
)

// The graph these drive: a run that begins because something happened, and then does one
// job with what the trigger carried.
const reactingGraph = `
name: fix-red
version: 1
nodes:
  arrived: { type: trigger }
  fix:     { type: dispatch, prompt: "the build at {{url}} is red. Fix it." }
edges:
  - [arrived, fix]
  - [fix, done]
`

func TestAGraphBeginsAtItsTriggerNode(t *testing.T) {
	graph, err := Parse([]byte(reactingGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if graph.Start != "arrived" {
		t.Fatalf("the graph begins at %q, want the trigger node", graph.Start)
	}
	if !graph.StartsOnTrigger() {
		t.Fatal("the graph does not say it reacts, so a trigger would start nothing")
	}
}

// A graph nobody wrote a trigger node into is one a person or a schedule starts, and it must not
// read as reacting.
func TestAGraphWithNoTriggerNodeDoesNotStartOnATrigger(t *testing.T) {
	graph, err := Parse([]byte(fixGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if graph.StartsOnTrigger() {
		t.Fatal("a graph that begins with a dispatch says it reacts")
	}
}

// A trigger node in the middle would be a node a run walks onto after the thing that triggered it
// already happened, which reads as reacting and is not.
func TestATriggerNodeThatIsNotWhereARunBeginsIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: late
version: 1
nodes:
  fix:     { type: dispatch, prompt: "fix the build" }
  arrived: { type: trigger }
  push:    { type: dispatch, prompt: "push the fix" }
edges:
  - [fix, arrived]
  - [arrived, push]
  - [push, done]
`))
	if err == nil {
		t.Fatal("a graph whose trigger node has an edge into it was accepted")
	}
	if !strings.Contains(err.Error(), "arrived") || !strings.Contains(err.Error(), "where a run begins") {
		t.Fatalf("the refusal says %q, want it to name the node and why", err)
	}
}

// One way in, because a trigger row names the graph rather than a node.
func TestAGraphWithTwoTriggerNodesIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: two-ways-in
version: 1
nodes:
  pushed: { type: trigger }
  opened: { type: trigger }
  fix:    { type: dispatch, prompt: "fix the build" }
edges:
  - [pushed, fix]
  - [opened, fix]
  - [fix, done]
`))
	if err == nil {
		t.Fatal("a graph with two trigger nodes was accepted")
	}
	if !strings.Contains(err.Error(), "opened") || !strings.Contains(err.Error(), "pushed") {
		t.Fatalf("the refusal says %q, want it to name both trigger nodes", err)
	}
}

// The same rule every other node that does not branch is held to: one way out, and nothing to
// choose between. A trigger that branched would be branching on nothing, because what it carries is
// state and a choice node is what reads state.
func TestATriggerNodeNeedsOneEdgeOut(t *testing.T) {
	_, err := Parse([]byte(`
name: forked
version: 1
nodes:
  arrived: { type: trigger }
  fix:     { type: dispatch, prompt: "fix the build" }
  push:    { type: dispatch, prompt: "push the fix" }
edges:
  - [arrived, fix, "true"]
  - [arrived, push, "false"]
  - [fix, done]
  - [push, done]
`))
	if err == nil {
		t.Fatal("a trigger node with two edges out was accepted, so a run of it would have two ways to go")
	}
	if !strings.Contains(err.Error(), "trigger node arrived") {
		t.Fatalf("the refusal says %q, want it to name the node", err)
	}
}

// A role is who does the work, and a trigger does none.
func TestATriggerNodeNamingARoleIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: roled
version: 1
nodes:
  arrived: { type: trigger, role: reviewer }
  fix:     { type: dispatch, prompt: "fix the build" }
edges:
  - [arrived, fix]
  - [fix, done]
`))
	if err == nil {
		t.Fatal("a trigger node naming a role was accepted, so a boundary would read as in force and not be")
	}
	if !strings.Contains(err.Error(), "reviewer") {
		t.Fatalf("the refusal says %q, want it to name the role", err)
	}
}

// A typo in a node's type is refused with the list of what a graph may say, and the list has to
// carry the type this slice added or an author cannot find it.
func TestTheRefusalOfAnUnknownNodeTypeOffersTheTriggerNode(t *testing.T) {
	_, err := Parse([]byte(`
name: mistyped
version: 1
nodes:
  arrived: { type: triger }
  fix:     { type: dispatch, prompt: "fix the build" }
edges:
  - [arrived, fix]
  - [fix, done]
`))
	if err == nil {
		t.Fatal("a node of an unknown type was accepted")
	}
	if !strings.Contains(err.Error(), NodeTrigger) {
		t.Fatalf("the refusal says %q, want it to offer %s", err, NodeTrigger)
	}
}

// The reducer walks straight through the entry node. The trigger arrived before the run existed and
// its payload is already the run's state, so there is nothing here to wait for.
func TestARunSettlesThroughItsTriggerNodeOntoTheFirstStep(t *testing.T) {
	graph, err := Parse([]byte(reactingGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	run := Run{
		ID: "run-1", GraphName: "fix-red", GraphVersion: 1, Status: StatusRunning,
		State: map[string]string{"url": "https://example.test/run/9"},
	}

	next, commands, err := Advance(graph, run, Event{Kind: EventStarted})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if next.Node != "fix" {
		t.Fatalf("the run sits on %q, want it through the trigger node and onto the step", next.Node)
	}
	if len(commands) != 1 || commands[0].Kind != CommandDispatch {
		t.Fatalf("the run asked for %v, want one dispatch", commands)
	}
	// What the trigger carried is what the prompt is rendered from, which is the whole point of the
	// payload becoming the opening state.
	if commands[0].Prompt != "the build at https://example.test/run/9 is red. Fix it." {
		t.Fatalf("the step was asked %q, want the trigger's payload rendered into it", commands[0].Prompt)
	}
	// One movement, not two: passing through a node that does nothing must not spend a transition
	// out of the graph's cap.
	if next.Transitions != 1 {
		t.Errorf("the run took %d transitions to reach its first step", next.Transitions)
	}
}
