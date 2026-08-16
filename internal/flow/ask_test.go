package flow

import (
	"strings"
	"testing"
)

// The shape an ask is for: the crew does something, then a person decides whether it goes further.
// That decision is the whole difference between an automation and a shell script.
const askingGraph = `
name: careful
version: 1
nodes:
  fix:    { type: dispatch, prompt: "fix the build" }
  permit: { type: ask, text: "fixed it locally. push?" }
  yes:    { type: choice, on: { answer: "yes" } }
  push:   { type: dispatch, prompt: "push it" }
edges:
  - [fix, permit]
  - [permit, yes]
  - [yes, push, "true"]
  - [yes, done, "false"]
  - [push, done]
`

func TestAnAskNodeParsesItsQuestion(t *testing.T) {
	graph, err := Parse([]byte(askingGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := graph.Nodes["permit"].Text; got != "fixed it locally. push?" {
		t.Fatalf("the question is %q", got)
	}
}

func TestAnAskWithNoQuestionIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: mute
version: 1
nodes:
  go:     { type: dispatch, prompt: "go" }
  permit: { type: ask }
edges:
  - [go, permit]
  - [permit, done]
`))
	if err == nil {
		t.Fatal("an ask with nothing to ask parsed, and the operator would see an empty question")
	}
	if !strings.Contains(err.Error(), "text") {
		t.Errorf("the refusal says %q, want it to name the field that is missing", err)
	}
}

// Reaching an ask puts the run down the way a wait does, and carries the question, rendered from
// the run's own state so it can say what it is asking about.
func TestReachingAnAskPutsTheQuestionToTheOperator(t *testing.T) {
	graph, err := Parse([]byte(`
name: careful
version: 1
nodes:
  fix:    { type: dispatch, prompt: "fix it" }
  permit: { type: ask, text: "fixed {{what}}. push?" }
edges:
  - [fix, permit]
  - [permit, done]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	run := Run{
		ID: "r1", Node: "fix", Status: StatusRunning,
		State: map[string]string{"what": "the build"}, Attempts: map[string]int{},
	}

	next, commands, err := Advance(graph, run, Event{Kind: EventTaskFinished, Node: "fix", Reply: "done"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if next.Status != StatusAsking {
		t.Fatalf("the run is %q, want it asking", next.Status)
	}
	if next.Question != "fixed the build. push?" {
		t.Fatalf("the run asks %q, want the question rendered from its state", next.Question)
	}
	for _, command := range commands {
		if command.Kind == CommandDispatch {
			t.Fatal("a run waiting on an answer still asked for a dispatch")
		}
	}
}

// The answer lands in state under one name, so an ordinary choice branches on it and the graph
// needs no expression language to read a person's decision.
func TestAnAnswerCarriesTheRunOnThroughAChoice(t *testing.T) {
	graph, err := Parse([]byte(askingGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	run := Run{ID: "r1", Node: "permit", Status: StatusAsking, State: map[string]string{}, Attempts: map[string]int{}}

	next, commands, err := Advance(graph, run, Event{Kind: EventAnswered, Node: "permit", Answer: "yes"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if next.State["answer"] != "yes" {
		t.Fatalf("the run's state carries %q as the answer", next.State["answer"])
	}
	if next.Node != "push" || next.Status != StatusRunning {
		t.Fatalf("the run went to %q as %q, want the push dispatch", next.Node, next.Status)
	}
	if len(commands) != 1 || commands[0].Kind != CommandDispatch {
		t.Fatalf("commands are %+v, want the dispatch the yes edge leads to", commands)
	}
	if next.Question != "" {
		t.Fatalf("the answered run still carries the question %q", next.Question)
	}
}

func TestAnyOtherAnswerTakesTheOtherEdge(t *testing.T) {
	graph, err := Parse([]byte(askingGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	run := Run{ID: "r1", Node: "permit", Status: StatusAsking, State: map[string]string{}, Attempts: map[string]int{}}

	next, _, err := Advance(graph, run, Event{Kind: EventAnswered, Node: "permit", Answer: "no"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if next.Status != StatusDone {
		t.Fatalf("the run is %q after being told no, want done", next.Status)
	}
}

// An answer for a question the run is not asking is refused, the same way a stale task result and a
// duplicate due event are: two answers to one question must not move a run twice.
func TestAnAnswerToAQuestionNobodyAskedIsRefused(t *testing.T) {
	graph, err := Parse([]byte(askingGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	run := Run{ID: "r1", Node: "push", Status: StatusRunning, State: map[string]string{}, Attempts: map[string]int{}}

	if _, _, err := Advance(graph, run, Event{Kind: EventAnswered, Node: "permit", Answer: "yes"}); err == nil {
		t.Fatal("an answer moved a run that was not asking anything")
	}
}

// A run waiting on a person is not waiting on a timer: nothing due ever wakes it, or an automation
// nobody answered would push to production on its own.
func TestATimerDoesNotAnswerAQuestion(t *testing.T) {
	graph, err := Parse([]byte(askingGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	run := Run{ID: "r1", Node: "permit", Status: StatusAsking, State: map[string]string{}, Attempts: map[string]int{}}

	if _, _, err := Advance(graph, run, Event{Kind: EventDue, Node: "permit"}); err == nil {
		t.Fatal("a due event answered a question, so an unanswered automation carries on by itself")
	}
}
