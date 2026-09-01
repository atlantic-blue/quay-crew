package flow_test

import (
	"context"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/flow"
	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A run of a graph that stops at its first usable path, driven through the engine and the store
// rather than through the reducer alone.
//
// The reducer decides where the run goes. What this holds is the part the reducer cannot: the
// sentence lands on the job carrying the run, every step under it is declared with the same one, and
// an answer of no replaces it before the next step is declared rather than after. That order is the
// whole of "the work continues from it": a step reads what it serves off the job above it as it is
// written down, so a replacement a moment late reaches every step except the one the answer was
// about.

const theTranscriptGraph = `
name: transcript
version: 1
mode: edits
product: paste a link and get the text back
nodes:
  page:
    type: dispatch
    prompt: "put the thinnest page up and reply with its address"
    usable: true
  polish:
    type: dispatch
    prompt: "finish the page"
edges:
  - [page, polish]
  - [polish, done]
`

const theAddress = "https://transcripts.example/watch?v=gyN9lV9QgyA"

// The answer of no comes first, because it is the answer the whole feature exists for and the one a
// test about yes passes without.
func TestAnAnswerOfNoReplacesTheSentenceBeforeTheNextStepIsDeclared(t *testing.T) {
	engine, it, workspace, project := aSystem(t, theTranscriptGraph)
	ctx := context.Background()

	run := started(t, engine, it, "transcript", workspace, project)
	carrier := carrierOf(t, it, run)
	if carrier.Product != "paste a link and get the text back" {
		t.Fatalf("the job carrying the run serves %q, want the graph's sentence", carrier.Product)
	}
	if first := stepOf(t, it, run); first.Product != carrier.Product {
		t.Fatalf("the first step serves %q and the run serves %q, want the same one", first.Product, carrier.Product)
	}

	run = worked(t, engine, it, run, theAddress)
	if run.Status != flow.StatusAsking {
		t.Fatalf("the run is %q with the first thing a person can open built, want it asking", run.Status)
	}
	for _, named := range []string{theAddress, "paste a link and get the text back"} {
		if !strings.Contains(run.Question, named) {
			t.Fatalf("the question is %q, want it to name %q", run.Question, named)
		}
	}
	// The question is on the job carrying the run too, which is where an operator reading the tree
	// finds it.
	if asking := carrierOf(t, it, run); asking.Question != run.Question {
		t.Errorf("the job carrying the run asks %q and the run asks %q", asking.Question, run.Question)
	}

	told, err := engine.Answer(ctx, run, "paste a YouTube link and get the text back")
	if err != nil {
		t.Fatalf("answering the run: %v", err)
	}
	if told.Status == flow.StatusDone || told.Status == flow.StatusStopped {
		t.Fatalf("the run is %q after being told no, want it carrying on from the new sentence", told.Status)
	}
	after := carrierOf(t, it, told)
	if after.Product != "paste a YouTube link and get the text back" {
		t.Fatalf("the job carrying the run serves %q, want the sentence the operator gave", after.Product)
	}
	// The step declared by the same movement as the answer. This is the assertion the ordering exists
	// for, and it fails if the sentence is written after the step is prepared rather than before.
	next := stepOf(t, it, told)
	if next.Product != "paste a YouTube link and get the text back" {
		t.Fatalf("the step declared after the answer serves %q, want the sentence that replaced the first", next.Product)
	}
	// And what the session doing it is handed, which is the only place the sentence does any work.
	if !strings.Contains(job.Asked(next), "paste a YouTube link and get the text back") {
		t.Errorf("the session doing the next step is asked %q, want the new sentence above its brief", job.Asked(next))
	}
	// The replacement is on the record, so a reader of the tree can say what the rest of the work was
	// done against.
	if !recorded(it, after.ID, flow.EventProductReplaced) {
		t.Errorf("the job carrying the run records %v, want the sentence being replaced among them",
			kindsOn(it, after.ID))
	}
}

func TestAnAnswerOfYesLeavesTheSentenceWhereItWas(t *testing.T) {
	engine, it, workspace, project := aSystem(t, theTranscriptGraph)

	run := started(t, engine, it, "transcript", workspace, project)
	run = worked(t, engine, it, run, theAddress)
	told, err := engine.Answer(context.Background(), run, "yes")
	if err != nil {
		t.Fatalf("answering the run: %v", err)
	}
	carrier := carrierOf(t, it, told)
	if carrier.Product != "paste a link and get the text back" {
		t.Fatalf("the job carrying the run serves %q after a yes, want the sentence it started with", carrier.Product)
	}
	if step := stepOf(t, it, told); step.Labels["flow.node"] != "polish" {
		t.Fatalf("the step out after the answer is %q, want the one after the question", step.Labels["flow.node"])
	}
	if recorded(it, carrier.ID, flow.EventProductReplaced) {
		t.Error("a yes replaced the sentence, and a yes says the sentence was right")
	}
}

// carrierOf is the job carrying a run, read back from the store.
func carrierOf(t *testing.T, it *system, run flow.Run) *job.Job {
	t.Helper()
	ctx := context.Background()
	id, err := it.store.FlowRunCarrier(ctx, run.ID)
	if err != nil {
		t.Fatalf("FlowRunCarrier: %v", err)
	}
	carrying, err := it.store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	return carrying
}

func kindsOn(it *system, id string) []string {
	records, err := it.store.ListJobEvents(context.Background(), id)
	if err != nil {
		return nil
	}
	kinds := make([]string, 0, len(records))
	for _, record := range records {
		kinds = append(kinds, record.Kind)
	}
	return kinds
}

func recorded(it *system, id, kind string) bool {
	for _, held := range kindsOn(it, id) {
		if held == kind {
			return true
		}
	}
	return false
}
