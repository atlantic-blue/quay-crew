package flow

import (
	"strings"
	"testing"
)

// The graph every test here drives: dispatch, then branch on how the task went, then either a
// second dispatch or the end. Small enough to hold in the head, wide enough to reach every node
// type slice one ships.
const fixGraph = `
name: fix-red
version: 1
nodes:
  fix:   { type: dispatch, prompt: "fix the build" }
  ok:    { type: choice, on: { result.failed: "false" } }
  push:  { type: dispatch, prompt: "push the fix" }
edges:
  - [fix, ok]
  - [ok, push, "true"]
  - [ok, done, "false"]
  - [push, done]
`

func parsed(t *testing.T) Graph {
	t.Helper()
	graph, err := Parse([]byte(fixGraph))
	if err != nil {
		t.Fatalf("parsing the graph: %v", err)
	}
	return graph
}

func TestAGraphParsesAndKnowsItsStart(t *testing.T) {
	graph := parsed(t)
	if graph.Name != "fix-red" || graph.Version != 1 {
		t.Fatalf("the graph is %q version %d", graph.Name, graph.Version)
	}
	if graph.Start != "fix" {
		t.Fatalf("the start node is %q, want the one no edge points into", graph.Start)
	}
}

func TestAGraphWhereEveryNodeHasAnIncomingEdgeIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: cycle
version: 1
nodes:
  a: { type: dispatch, prompt: "a" }
  b: { type: dispatch, prompt: "b" }
edges:
  - [a, b]
  - [b, a]
`))
	if err == nil {
		t.Fatal("a graph with no start parses, and a run of it would have nowhere to begin")
	}
	if !strings.Contains(err.Error(), "start") {
		t.Errorf("the refusal says %q, want it to name the missing start", err)
	}
}

func TestAnEdgeToANodeNobodyDeclaredIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: dangling
version: 1
nodes:
  a: { type: dispatch, prompt: "a" }
edges:
  - [a, nowhere]
`))
	if err == nil {
		t.Fatal("an edge to an undeclared node parses, and a run would fall off it at runtime instead")
	}
}

func TestStartingARunDispatchesTheFirstNode(t *testing.T) {
	graph := parsed(t)
	run := Run{ID: "r1", Node: "", Status: StatusRunning, State: map[string]string{}}

	next, commands, err := Advance(graph, run, Event{Kind: EventStarted})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if next.Node != "fix" {
		t.Fatalf("the run sits on %q, want the start node", next.Node)
	}
	if len(commands) != 1 || commands[0].Kind != CommandDispatch {
		t.Fatalf("commands are %+v, want one dispatch", commands)
	}
	if commands[0].Prompt != "fix the build" || commands[0].Node != "fix" || commands[0].Attempt != 1 {
		t.Fatalf("the dispatch is %+v", commands[0])
	}
}

func TestAFinishedTaskBranchesOnItsResult(t *testing.T) {
	graph := parsed(t)
	run := Run{ID: "r1", Node: "fix", Status: StatusRunning, State: map[string]string{}}

	next, commands, err := Advance(graph, run, Event{Kind: EventTaskFinished, Node: "fix", Reply: "all green"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	// The task worked, so result.failed is "false", the choice answers true, and the run moves to
	// the second dispatch.
	if next.Node != "push" || next.Status != StatusRunning {
		t.Fatalf("the run sits on %q as %q, want push, still running", next.Node, next.Status)
	}
	if next.State["result.reply"] != "all green" {
		t.Fatalf("the state carries %q, want the reply", next.State["result.reply"])
	}
	if len(commands) != 1 || commands[0].Kind != CommandDispatch || commands[0].Node != "push" {
		t.Fatalf("commands are %+v, want the second dispatch", commands)
	}
}

func TestAFailedTaskTakesTheOtherEdge(t *testing.T) {
	graph := parsed(t)
	run := Run{ID: "r1", Node: "fix", Status: StatusRunning, State: map[string]string{}}

	next, commands, err := Advance(graph, run, Event{Kind: EventTaskFinished, Node: "fix", Failed: true})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	// The task failed, so the choice answers false and the run ends without pushing anything.
	if next.Node != DoneNode || next.Status != StatusDone {
		t.Fatalf("the run sits on %q as %q, want done", next.Node, next.Status)
	}
	if len(commands) != 1 || commands[0].Kind != CommandArchive {
		t.Fatalf("commands are %+v, want the archive that keeps a finished run from leaving a container behind", commands)
	}
}

func TestAResultForANodeTheRunLeftIsRefused(t *testing.T) {
	graph := parsed(t)
	run := Run{ID: "r1", Node: "push", Status: StatusRunning, State: map[string]string{}}

	_, _, err := Advance(graph, run, Event{Kind: EventTaskFinished, Node: "fix", Reply: "late"})
	if err == nil {
		t.Fatal("a stale result advanced the run, so a delayed duplicate would move it twice")
	}
}

func TestADoneRunRefusesToMove(t *testing.T) {
	graph := parsed(t)
	run := Run{ID: "r1", Node: "done", Status: StatusDone, State: map[string]string{}}

	_, _, err := Advance(graph, run, Event{Kind: EventTaskFinished, Node: "push", Reply: "again"})
	if err == nil {
		t.Fatal("a finished run advanced")
	}
}

func TestTheAttemptCountsPerNode(t *testing.T) {
	graph := parsed(t)
	run := Run{ID: "r1", Node: "fix", Status: StatusRunning, State: map[string]string{}}

	// The worked task routes through the choice to push; the dispatch for push is its first attempt
	// even though the run has dispatched before.
	next, commands, err := Advance(graph, run, Event{Kind: EventTaskFinished, Node: "fix", Reply: "green"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if commands[0].Attempt != 1 {
		t.Fatalf("the push dispatch is attempt %d, want 1: the key is run, node and attempt", commands[0].Attempt)
	}
	_ = next
}

func TestPromptsRenderFromState(t *testing.T) {
	graph, err := Parse([]byte(`
name: templated
version: 1
nodes:
  greet: { type: dispatch, prompt: "hello {{who}}" }
edges:
  - [greet, done]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	run := Run{ID: "r1", Status: StatusRunning, State: map[string]string{"who": "julian"}}
	_, commands, err := Advance(graph, run, Event{Kind: EventStarted})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if commands[0].Prompt != "hello julian" {
		t.Fatalf("the prompt rendered as %q", commands[0].Prompt)
	}
}
