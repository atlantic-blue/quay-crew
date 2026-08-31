package flow

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/krewe/internal/job"
)

// The stop at the first thing a person can open, as the reducer decides it.
//
// The failure these exist for: a run built a design document faithfully, every check was green, and
// the operator opened it two days later and could not use it. Nothing in a run ever asked whether
// the document was the product. So a run stops once, at the first thing a person can open, and asks
// that question while an answer of no still costs one step.
//
// The refusals come first here, and the answer of no comes before the answer of yes. A gate that
// always passes satisfies every test written about passing.

// theUsableGraph builds something a person can open, asks about it, and carries on either way. The
// loop back through `again` is what proves the run stops once rather than every time round.
const theUsableGraph = `
name: transcript
version: 1
mode: edits
product: paste a link and get the text back
nodes:
  scaffold:
    type: dispatch
    prompt: "stand the repository up"
  page:
    type: dispatch
    prompt: "put the thinnest page up and reply with its address"
    usable: true
  more:
    type: choice
    on:
      result.reply: "again"
  polish:
    type: dispatch
    prompt: "finish the page. It serves: {{product}}"
edges:
  - [scaffold, page]
  - [page, more]
  - [more, page, "true"]
  - [more, polish, "false"]
  - [polish, done]
`

func TestAGraphThatStopsForAPersonAndSaysNothingAboutWhatTheyGetIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: transcript
version: 1
mode: edits
nodes:
  page:   { type: dispatch, prompt: "put the page up", usable: true }
  polish: { type: dispatch, prompt: "finish it" }
edges:
  - [page, polish]
  - [polish, done]
`))
	if err == nil {
		t.Fatal("a graph that stops for a person with no sentence parsed, so the operator would be shown an address and asked whether it is right, against nothing")
	}
	if !strings.Contains(err.Error(), "product:") {
		t.Errorf("the refusal says %q, want it to name the line the author has to add", err)
	}
}

// A run stops once, so which node is the first usable one is a fact about the file rather than a
// race in the run.
func TestAGraphWithTwoUsableNodesIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: transcript
version: 1
mode: edits
product: paste a link and get the text back
nodes:
  page:   { type: dispatch, prompt: "put the page up", usable: true }
  polish: { type: dispatch, prompt: "finish it", usable: true }
edges:
  - [page, polish]
  - [polish, done]
`))
	if err == nil {
		t.Fatal("a graph marking two nodes as the first thing a person can open parsed, and a run of it would ask twice")
	}
	for _, named := range []string{"page", "polish", "stops once"} {
		if !strings.Contains(err.Error(), named) {
			t.Errorf("the refusal says %q, want it to say %q", err, named)
		}
	}
}

// Only a dispatch builds anything, so only a dispatch can be the thing a person opens. Refused
// rather than ignored: a graph that reads as stopping for a person and does not is worse than one
// that never said it would.
func TestOnlyADispatchCanBeTheFirstThingAPersonCanOpen(t *testing.T) {
	_, err := Parse([]byte(`
name: transcript
version: 1
mode: edits
product: paste a link and get the text back
nodes:
  page:   { type: dispatch, prompt: "put the page up" }
  permit: { type: ask, text: "is it up?", usable: true }
edges:
  - [page, permit]
  - [permit, done]
`))
	if err == nil {
		t.Fatal("an ask node was taken as the first thing a person can open, and a run would never stop there")
	}
	if !strings.Contains(err.Error(), NodeDispatch) {
		t.Errorf("the refusal says %q, want it to say only a dispatch builds anything", err)
	}
}

func TestASentenceTooLongForOneLineIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: transcript
version: 1
mode: edits
product: ` + strings.Repeat("a", job.ProductLimit+1) + `
nodes:
  page: { type: dispatch, prompt: "put the page up", usable: true }
edges:
  - [page, done]
`))
	if err == nil {
		t.Fatalf("a sentence of %d bytes parsed, and a paragraph there is a design document arriving by the back door", job.ProductLimit+1)
	}
	if !strings.Contains(err.Error(), "200") {
		t.Errorf("the refusal says %q, want it to say what the ceiling is", err)
	}
}

// A step that answers with nothing leaves the question naming an address the operator cannot open,
// so the run stops rather than asking it. A gate whose question is empty is a gate that passes.
func TestAUsableStepThatRepliedWithNoAddressStopsTheRun(t *testing.T) {
	graph := graphOf(t, theUsableGraph)
	run := aRunOn(graph, "page")

	next, commands, err := Advance(graph, run, Event{Kind: EventTaskFinished, Node: "page", Reply: "   "})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if next.Status != StatusStopped {
		t.Fatalf("the run is %q after a step that named no address, want it stopped", next.Status)
	}
	if !strings.Contains(next.Reason, "address") {
		t.Errorf("the run stopped saying %q, want it to name what was missing", next.Reason)
	}
	if len(commands) != 0 {
		t.Errorf("the run asked for %+v while it had nothing to ask about", commands)
	}
}

// The stop itself: the question carries the address and the sentence, and nothing is dispatched
// until a person has answered.
func TestTheRunStopsAtTheFirstThingAPersonCanOpen(t *testing.T) {
	graph := graphOf(t, theUsableGraph)
	run := aRunOn(graph, "page")
	run.State[stateProduct] = graph.Product

	next, commands, err := Advance(graph, run, Event{
		Kind: EventTaskFinished, Node: "page", Reply: "https://transcripts.example/watch?v=gyN9lV9QgyA",
	})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if next.Status != StatusAsking {
		t.Fatalf("the run is %q at the first thing a person can open, want it asking", next.Status)
	}
	for _, named := range []string{"https://transcripts.example/watch?v=gyN9lV9QgyA", "paste a link and get the text back"} {
		if !strings.Contains(next.Question, named) {
			t.Errorf("the question is %q, want it to name %q", next.Question, named)
		}
	}
	for _, command := range commands {
		if command.Kind == CommandDispatch {
			t.Fatal("a run waiting to be told whether it built the product dispatched the next step anyway")
		}
	}
}

// The answer the whole feature exists for. It does not end the run, and what the work carries on
// against is the operator's sentence rather than the one that was wrong.
func TestAnAnswerOfNoReplacesTheSentenceAndTheRunCarriesOn(t *testing.T) {
	graph := graphOf(t, theUsableGraph)
	run := asked(t, graph)

	next, commands, err := Advance(graph, run, Event{
		Kind: EventAnswered, Node: "page", Answer: "paste a YouTube link and get the text back",
	})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if next.Status == StatusDone || next.Status == StatusStopped {
		t.Fatalf("the run is %q after being told no, and the answer is meant to cost one step rather than the run", next.Status)
	}
	if next.State[stateProduct] != "paste a YouTube link and get the text back" {
		t.Fatalf("the run serves %q, want the sentence the operator gave", next.State[stateProduct])
	}
	if len(commands) != 1 || commands[0].Kind != CommandDispatch {
		t.Fatalf("commands are %+v, want the next step", commands)
	}
	// The prompt is rendered from the state, so the step that carries on is asked against the new
	// sentence rather than the one the operator refused.
	if !strings.Contains(commands[0].Prompt, "paste a YouTube link and get the text back") {
		t.Errorf("the next step is asked %q, want it to carry the sentence that replaced the first", commands[0].Prompt)
	}
}

func TestAnAnswerOfYesLeavesTheSentenceAloneAndCarriesOn(t *testing.T) {
	graph := graphOf(t, theUsableGraph)
	run := asked(t, graph)

	next, commands, err := Advance(graph, run, Event{Kind: EventAnswered, Node: "page", Answer: "yes"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if next.State[stateProduct] != graph.Product {
		t.Fatalf("the run serves %q after a yes, want the sentence it started with", next.State[stateProduct])
	}
	if next.Node != "polish" || len(commands) != 1 {
		t.Fatalf("the run went to %q with commands %+v, want the step after the question", next.Node, commands)
	}
}

// Silence must not take the product further. It is the same rule an asking run already keeps about
// timers, said about an answer that carries no words.
func TestAnEmptyAnswerIsRefused(t *testing.T) {
	graph := graphOf(t, theUsableGraph)
	run := asked(t, graph)

	if _, _, err := Advance(graph, run, Event{Kind: EventAnswered, Node: "page", Answer: "  "}); err == nil {
		t.Fatal("an empty answer moved the run, so nothing was said about the product and the work carried on anyway")
	}
}

func TestAnAnswerTooLongToBeASentenceIsRefused(t *testing.T) {
	graph := graphOf(t, theUsableGraph)
	run := asked(t, graph)

	_, _, err := Advance(graph, run, Event{
		Kind: EventAnswered, Node: "page", Answer: strings.Repeat("a", job.ProductLimit+1),
	})
	if err == nil {
		t.Fatalf("an answer of %d bytes replaced the sentence, and the ceiling is %d", job.ProductLimit+1, job.ProductLimit)
	}
	if !strings.Contains(err.Error(), "200") {
		t.Errorf("the refusal says %q, want it to say what the ceiling is", err)
	}
}

// Once. A graph that sends the work round again reaches the same step a second time, and the
// operator has already answered that question.
func TestTheRunStopsOnceHoweverOftenItReachesTheStep(t *testing.T) {
	graph := graphOf(t, theUsableGraph)
	run := asked(t, graph)

	// Told no, so the work goes round again through the choice that leads back to the same step.
	next, _, err := Advance(graph, run, Event{Kind: EventAnswered, Node: "page", Answer: "paste a link and read it aloud"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	next.Node, next.Status = "page", StatusRunning
	next, commands, err := Advance(graph, next, Event{Kind: EventTaskFinished, Node: "page", Reply: "again"})
	if err != nil {
		t.Fatalf("Advance the second time round: %v", err)
	}
	if next.Status == StatusAsking {
		t.Fatalf("the run asked again at %s, and the operator has already answered that question", next.Node)
	}
	if len(commands) != 1 || commands[0].Kind != CommandDispatch {
		t.Fatalf("commands are %+v, want the run carrying on", commands)
	}
}

// A graph that never marks a step is every graph that ships today, and none of them may start
// asking about a product because this landed.
func TestAGraphThatMarksNothingNeverStops(t *testing.T) {
	graph := graphOf(t, `
name: fix-red
version: 1
mode: edits
nodes:
  fix:  { type: dispatch, prompt: "fix the build" }
  push: { type: dispatch, prompt: "push the fix" }
edges:
  - [fix, push]
  - [push, done]
`)
	run := aRunOn(graph, "fix")

	next, commands, err := Advance(graph, run, Event{Kind: EventTaskFinished, Node: "fix", Reply: "fixed"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if next.Status != StatusRunning || len(commands) != 1 {
		t.Fatalf("the run is %q with commands %+v, want it carrying straight on to the next step", next.Status, commands)
	}
}

func graphOf(t *testing.T, source string) Graph {
	t.Helper()
	graph, err := Parse([]byte(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return graph
}

// aRunOn is a run sitting on one node, mid graph.
func aRunOn(graph Graph, node string) Run {
	return Run{
		ID: "r1", GraphName: graph.Name, Node: node, Status: StatusRunning,
		State: map[string]string{}, Attempts: map[string]int{},
	}
}

// asked is a run that has reached the first thing a person can open and is waiting to be told.
func asked(t *testing.T, graph Graph) Run {
	t.Helper()
	run := aRunOn(graph, "page")
	run.State[stateProduct] = graph.Product
	next, _, err := Advance(graph, run, Event{
		Kind: EventTaskFinished, Node: "page", Reply: "https://transcripts.example/watch?v=gyN9lV9QgyA",
	})
	if err != nil {
		t.Fatalf("driving the run to its question: %v", err)
	}
	if next.Status != StatusAsking {
		t.Fatalf("the run is %q, want it asking before anybody answers", next.Status)
	}
	return next
}
