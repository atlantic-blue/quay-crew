package flow

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// The sentence a graph serves is kept at whatever length it is written.
//
// It is the one line that says what a person does and what they get back, and every step of a run is
// declared against it. A graph refused for the length of that line is a flow nobody can run: the
// file does not parse, so the automation the operator wrote is gone rather than long. The answer
// that replaces the sentence at the first usable stop is the same line, written by the person the
// run stopped for, and it is refused the same way today.

// aSentenceOf is one line of exactly this many bytes, with a start and an end an assertion can look
// for, so a sentence cut at either end shows as a cut rather than as a pass.
func aSentenceOf(size int) string {
	const opens, ends = "paste a link and get the text back", "and this sentence ends here"
	middle := size - len(opens) - len(ends) - 2
	if middle < 1 {
		panic("a sentence this short cannot carry both ends")
	}
	return opens + " " + strings.Repeat("x", middle) + " " + ends
}

func TestAGraphSentenceOfAnyLengthIsKeptWordForWord(t *testing.T) {
	sentence := aSentenceOf(job.ProductLimit * 2)

	graph, err := Parse([]byte(`
name: transcript
version: 1
mode: edits
product: ` + sentence + `
nodes:
  page: { type: dispatch, prompt: "put the page up", usable: true }
edges:
  - [page, done]
`))
	if err != nil {
		t.Fatalf("a graph whose sentence is %d bytes was refused: %v", len(sentence), err)
	}
	if graph.Product != sentence {
		t.Fatalf("the sentence was kept as %d bytes of the %d it was written with",
			len(graph.Product), len(sentence))
	}
}

// The answer at the first usable stop replaces the sentence, and the run carries on against what the
// person wrote rather than against a refusal.
func TestALongAnswerReplacesTheSentenceWordForWord(t *testing.T) {
	graph := graphOf(t, theUsableGraph)
	run := asked(t, graph)
	instead := aSentenceOf(job.ProductLimit * 2)

	next, commands, err := Advance(graph, run, Event{Kind: EventAnswered, Node: "page", Answer: instead})
	if err != nil {
		t.Fatalf("an answer of %d bytes was refused, and the run is waiting on it: %v", len(instead), err)
	}
	if next.State[stateProduct] != instead {
		t.Fatalf("the run serves %q, want the sentence the operator gave", next.State[stateProduct])
	}
	if len(commands) != 1 || commands[0].Kind != CommandDispatch {
		t.Fatalf("commands are %+v, want the next step", commands)
	}
	if !strings.Contains(commands[0].Prompt, instead) {
		t.Fatalf("the next step is asked %q, want it to carry the whole sentence that replaced the first",
			commands[0].Prompt)
	}
}
