package flow

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A choice node branches on the word a step ended on, not on the sentence it wrote. The refusal
// comes first: a graph that took any word would take the other edge every time and say nothing.

func TestAChoiceWaitingForAWordThatIsNotAnOutcomeIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: ship
version: 1
mode: edits
nodes:
  work:  { type: dispatch, prompt: "do it" }
  went:  { type: choice, on: { result.outcome: "complete" } }
edges:
  - [work, went]
  - [went, done, "true"]
  - [went, done, "false"]
`))
	if err == nil {
		t.Fatal("a choice waiting for an outcome nothing hands out was imported")
	}
	for _, said := range []string{"complete", job.OutcomeProved, job.OutcomeBlocked} {
		if !strings.Contains(err.Error(), said) {
			t.Fatalf("the refusal says %q, want it to name %q", err, said)
		}
	}
}

func TestAChoiceWaitingForAnOutcomeIsImported(t *testing.T) {
	for _, outcome := range job.Outcomes() {
		graph, err := Parse([]byte(`
name: ship
version: 1
mode: edits
nodes:
  work:  { type: dispatch, prompt: "do it" }
  went:  { type: choice, on: { result.outcome: "` + outcome + `" } }
edges:
  - [work, went]
  - [went, done, "true"]
  - [went, done, "false"]
`))
		if err != nil {
			t.Fatalf("a choice waiting for %q was refused: %v", outcome, err)
		}
		if graph.Nodes["went"].On[OutcomeKey] != outcome {
			t.Fatalf("the choice waits for %q, want %q", graph.Nodes["went"].On[OutcomeKey], outcome)
		}
	}
}

// A choice on anything else is left alone. The rule is about the one key the system writes, and a
// graph comparing its own state keys is none of its business.
func TestAChoiceOnAnyOtherKeyIsNotHeldToTheOutcomes(t *testing.T) {
	if _, err := Parse([]byte(`
name: ship
version: 1
mode: edits
nodes:
  work:  { type: dispatch, prompt: "do it" }
  went:  { type: choice, on: { result.reply: "green" } }
edges:
  - [work, went]
  - [went, done, "true"]
  - [went, done, "false"]
`)); err != nil {
		t.Fatalf("a choice on the reply was refused: %v", err)
	}
}

// The word arrives beside the prose rather than inside it, so a graph that compares a reply is
// comparing what the session wrote and nothing the system added.
func TestAStepsOutcomeReachesTheRunBesideItsReply(t *testing.T) {
	graph, err := Parse([]byte(`
name: ship
version: 1
mode: edits
nodes:
  work:  { type: dispatch, prompt: "do it" }
  went:  { type: choice, on: { result.outcome: "blocked" } }
  say:   { type: dispatch, prompt: "say what stopped it" }
edges:
  - [work, went]
  - [went, say, "true"]
  - [went, done, "false"]
  - [say, done]
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	run := Run{ID: "run-1", Status: StatusRunning, Node: "work", State: map[string]string{}}
	moved, commands, err := Advance(graph, run, Event{
		Kind: EventTaskFinished, Node: "work",
		Reply: "the credential ran out", Outcome: job.OutcomeBlocked,
	})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if moved.State[OutcomeKey] != job.OutcomeBlocked {
		t.Fatalf("the run reads the outcome as %q, want %q", moved.State[OutcomeKey], job.OutcomeBlocked)
	}
	if moved.State["result.reply"] != "the credential ran out" {
		t.Fatalf("the run reads the reply as %q", moved.State["result.reply"])
	}
	if moved.Node != "say" {
		t.Fatalf("the run sits on %q, want the edge the outcome takes", moved.Node)
	}
	if len(commands) != 1 || commands[0].Kind != CommandDispatch {
		t.Fatalf("the run produced %v, want the dispatch on the branch it took", commands)
	}
}
