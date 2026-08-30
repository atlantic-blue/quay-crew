package flow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The graph quay-crew#513 asked for, held to what the issue asked of it.
//
// It is one file, so these read it off disk rather than from a copy written here: a test carrying
// its own copy of the graph proves the copy and says nothing about the thing that ships.

const reviewGraph = "pull-request-review.yaml"

func theReviewGraph(t *testing.T) Graph {
	t.Helper()
	at := filepath.Join(shippedFlows, reviewGraph)
	body, err := os.ReadFile(at)
	if err != nil {
		t.Fatalf("reading %s: %v", at, err)
	}
	graph, err := Parse(body)
	if err != nil {
		t.Fatalf("%s does not parse, so it could not be imported: %v", at, err)
	}
	return graph
}

// The order is the whole design. A security finding blocks the merge whatever else is true, so it
// comes first; a feature that does not work end to end comes next; what is missing comes last. A
// graph that ran them in any other order would still produce a review, and the review would bury
// the finding that decides the merge.
func TestTheReviewMakesItsThreePassesInThatOrder(t *testing.T) {
	graph := theReviewGraph(t)
	want := []string{"security", "features", "completeness", "draft"}
	at := want[0]
	for _, next := range want[1:] {
		went, err := follow(graph, at, "")
		if err != nil {
			t.Fatalf("the pass %s leads nowhere: %v", at, err)
		}
		if went != next {
			t.Fatalf("%s leads to %q, want %q", at, went, next)
		}
		at = next
	}
	for _, pass := range want[:3] {
		if graph.Nodes[pass].Type != NodeDispatch {
			t.Errorf("the %s pass is a %s, and only a dispatch does any work", pass, graph.Nodes[pass].Type)
		}
	}
}

// Posting a review is sending a message to a person, so it is the operator's call and not the
// system's. Every road to the posting step goes through an ask, and this walks the graph rather than
// looking at one edge: a second road to the same node would be the way this fails.
func TestNothingIsPostedWithoutPassingTheAsk(t *testing.T) {
	graph := theReviewGraph(t)
	for _, road := range roadsTo(graph, "post") {
		if !strings.Contains(road, "|"+NodeAsk+"|") {
			t.Fatalf("the review reaches the posting step by %s, and nothing on that road stops for a person", road)
		}
	}
}

// The other half of the stop. A run told anything but yes ends and posts nothing, which is what
// makes the question a real one rather than a pause.
func TestAnAnswerThatIsNotYesEndsTheRun(t *testing.T) {
	graph := theReviewGraph(t)
	agreed, held := graph.Nodes["agreed"]
	if !held || agreed.Type != NodeChoice {
		t.Fatalf("the node after the question is %+v, want a choice on the answer", agreed)
	}
	if agreed.On["answer"] != "yes" {
		t.Fatalf("the choice reads %v, want it to carry on only on a yes", agreed.On)
	}
	refused, err := follow(graph, "agreed", "false")
	if err != nil {
		t.Fatalf("the answered graph leads nowhere: %v", err)
	}
	if refused != DoneNode {
		t.Fatalf("an answer that is not yes goes to %q, want the run to end", refused)
	}
}

// A question about a draft nobody can see is not a question. The operator reads the whole review in
// the question itself, because the session that wrote it is put away by the time they are asked.
func TestTheQuestionCarriesTheDraft(t *testing.T) {
	graph := theReviewGraph(t)
	asked := graph.Nodes["permit"]
	if asked.Type != NodeAsk {
		t.Fatalf("the node that stops for the operator is a %s", asked.Type)
	}
	if !strings.Contains(asked.Text, "{{result.reply}}") {
		t.Fatalf("the question is %q, and it never renders what the draft step wrote", asked.Text)
	}
	if !strings.Contains(asked.Text, "yes") {
		t.Errorf("the question is %q, and it does not say what answer posts the review", asked.Text)
	}
}

// A run that finds nothing to review ends, rather than reviewing whatever it found anyway. The
// reply is matched for the one word the pick step is told to answer with.
func TestARunWithNothingToReviewEnds(t *testing.T) {
	graph := theReviewGraph(t)
	found := graph.Nodes["found"]
	if found.Type != NodeChoice {
		t.Fatalf("the node after the pick is a %s, want a choice", found.Type)
	}
	if found.On["result.reply"] != "none" {
		t.Fatalf("the choice reads %v, want it to read the word the pick step answers with", found.On)
	}
	nothing, err := follow(graph, "found", "true")
	if err != nil {
		t.Fatalf("the choice leads nowhere: %v", err)
	}
	if nothing != DoneNode {
		t.Fatalf("a run with nothing to review goes to %q, want it to end", nothing)
	}
	if !strings.Contains(graph.Nodes["pick"].Prompt, "none") {
		t.Error("the pick step is never told the word that ends the run")
	}
}

// The reply is the model's account of itself, and a step is checked against a file instead. The
// pick step declares one too, so a run that found nothing still leaves the reading behind it.
func TestEveryStepOfTheReviewSaysWhatWouldShowItWorked(t *testing.T) {
	graph := theReviewGraph(t)
	for name, node := range graph.Nodes {
		if node.Type != NodeDispatch {
			continue
		}
		if node.Expect == nil || node.Expect.File == "" {
			t.Errorf("the %s step claims no file, so the system has only its own account of itself", name)
		}
	}
}

// A pass with nothing to say has to say that in one line, or a reader cannot tell a pass that found
// nothing from a pass that did not run. And a review of naming and formatting is a review nobody
// asked for: the linter already does it, and a finding earns its place by changing what a user or
// an operator gets.
func TestEachPassSaysWhatToDoWithNothingAndWithNits(t *testing.T) {
	graph := theReviewGraph(t)
	for _, pass := range []string{"security", "features", "completeness"} {
		prompt := graph.Nodes[pass].Prompt
		if !strings.Contains(prompt, "say so in one line") {
			t.Errorf("the %s pass is never told to say so in one line when it finds nothing", pass)
		}
		if !strings.Contains(prompt, "Report nothing") {
			t.Errorf("the %s pass is never told what not to report", pass)
		}
	}
}

// Each pass reads the repository as well as the diff, because most of what matters lives in the
// files the pull request did not touch.
func TestThePassesReadTheRepositoryAndNotOnlyTheDiff(t *testing.T) {
	graph := theReviewGraph(t)
	if !strings.Contains(graph.Nodes["security"].Prompt, "not only the diff") {
		t.Error("the security pass is never told to read the code around the change")
	}
	if !strings.Contains(graph.Nodes["features"].Prompt, "did not touch") {
		t.Error("the features pass is never told to read the files the pull request did not touch")
	}
}

// There is no trigger node yet, quay-crew#433, so the graph picks its own subject on a schedule. A
// graph that says nothing about when it runs is one somebody has to remember to start.
func TestTheReviewRunsOnItsOwn(t *testing.T) {
	graph := theReviewGraph(t)
	if graph.Every < MinimumEvery {
		t.Fatalf("the graph runs every %s, and the system refuses anything under %s", graph.Every, MinimumEvery)
	}
}

// roadsTo walks every road from the start of the graph to a node, as a string of node names and
// node types, so a test can ask what is on the way rather than which edge leads where.
func roadsTo(graph Graph, target string) []string {
	var roads []string
	var walk func(at, so string, seen map[string]bool)
	walk = func(at, so string, seen map[string]bool) {
		if seen[at] {
			return
		}
		seen[at] = true
		defer delete(seen, at)
		so += at + "|" + graph.Nodes[at].Type + "|"
		if at == target {
			roads = append(roads, so)
			return
		}
		for _, edge := range graph.Edges {
			if edge.From == at && edge.To != DoneNode {
				walk(edge.To, so, seen)
			}
		}
	}
	walk(graph.Start, "", map[string]bool{})
	return roads
}
