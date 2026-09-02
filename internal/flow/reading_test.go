package flow

import (
	"os"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A plan read by several roles, in the reducer. The rows themselves live on the jobs, and what is
// held here is the part a table test can prove: a reply kept per node, a question rendered into the
// prompt of the reader that comes next, and the run that asks nobody because every row was settled.

// readingGraph is the shape the shipped file has, small enough to hold in the head: two readings,
// a branch on whether anything is left, and one question at the end.
const readingGraph = `
name: three-readers
version: 1
mode: edits
nodes:
  critic:   { type: dispatch, role: plan-critic, prompt: "read the plan" }
  tester:   { type: dispatch, role: test-writer, prompt: "read it again. Still open:\n{{questions.open}}" }
  anything: { type: choice, on: { questions.open: "" } }
  ask:      { type: ask, text: "nobody could settle these:\n{{questions.open}}" }
edges:
  - [critic, tester]
  - [tester, anything]
  - [anything, done, "true"]
  - [anything, ask, "false"]
  - [ask, done]
`

func readingParsed(t *testing.T) Graph {
	t.Helper()
	graph, err := Parse([]byte(readingGraph))
	if err != nil {
		t.Fatalf("parsing the graph: %v", err)
	}
	return graph
}

// The key this whole reading rests on. A run kept one result.reply, so the second reader's answer
// took the first one's place and the run held one of two readings.
func TestEachReadingsReplyLandsUnderItsOwnKey(t *testing.T) {
	graph := readingParsed(t)
	run := Run{ID: "r", Status: StatusRunning, Node: "critic", State: map[string]string{}, Attempts: map[string]int{}}

	after, _, err := Advance(graph, run, Event{Kind: EventTaskFinished, Node: "critic", Reply: "the critic read it"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	after, _, err = Advance(graph, after, Event{Kind: EventTaskFinished, Node: "tester", Reply: "the tester read it"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if got := after.State[ReplyKeyPrefix+"critic"]; got != "the critic read it" {
		t.Fatalf("the first reading reads back as %q, and a run that keeps one reply holds only the last", got)
	}
	if got := after.State[ReplyKeyPrefix+"tester"]; got != "the tester read it" {
		t.Fatalf("the second reading reads back as %q", got)
	}
	// And the key every graph already branches on is exactly what it was.
	if got := after.State["result.reply"]; got != "the tester read it" {
		t.Fatalf("result.reply is %q, want the last reply, unchanged", got)
	}
}

// A reading that settled everything reaches the work without asking anything. A graph that always
// asks is the interrogation this must not become.
func TestAReadingThatLeftNothingOpenAsksNobody(t *testing.T) {
	graph := readingParsed(t)
	run := Run{ID: "r", Status: StatusRunning, Node: "tester",
		State: map[string]string{QuestionsKey: ""}, Attempts: map[string]int{}}

	after, commands, err := Advance(graph, run, Event{Kind: EventTaskFinished, Node: "tester", Reply: "nothing left"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if after.Status != StatusDone {
		t.Fatalf("the run is %s with every row settled, want it at the work", after.Status)
	}
	if after.Question != "" {
		t.Fatalf("a run with nothing open asked %q", after.Question)
	}
	if len(commands) != 1 || commands[0].Kind != CommandArchive {
		t.Fatalf("the run asked for %+v, want only the archive that ends it", commands)
	}
}

// And the other half, which is the one the feature exists for: a row nobody settled reaches a
// person, whole, and the reply of any reading does not.
func TestARowNobodySettledIsWhatThePersonIsAsked(t *testing.T) {
	graph := readingParsed(t)
	open := job.RenderQuestions([]job.Question{
		{Seq: 2, Text: "what does a person type, and what comes back", AskedBy: "test-writer"},
	})
	run := Run{ID: "r", Status: StatusRunning, Node: "tester",
		State: map[string]string{QuestionsKey: open}, Attempts: map[string]int{}}

	after, _, err := Advance(graph, run, Event{Kind: EventTaskFinished, Node: "tester",
		Reply: "a long reading nobody should be asked to read"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if after.Status != StatusAsking {
		t.Fatalf("the run is %s with a row open, want it asking", after.Status)
	}
	if !strings.Contains(after.Question, "what does a person type") {
		t.Fatalf("the question put to the person is %q, want the open row in it", after.Question)
	}
	if strings.Contains(after.Question, "a long reading nobody should be asked to read") {
		t.Fatalf("the question carries a reading rather than the rows: %q", after.Question)
	}
}

// The reader that comes second is handed the rows in its own prompt, which is the whole of what
// "handed the open rows" means to the model.
func TestTheNextReadingIsGivenTheOpenRowsInItsPrompt(t *testing.T) {
	graph := readingParsed(t)
	open := job.RenderQuestions([]job.Question{{Seq: 1, Text: "which store holds the text"}})
	run := Run{ID: "r", Status: StatusRunning, Node: "critic",
		State: map[string]string{QuestionsKey: open}, Attempts: map[string]int{}}

	_, commands, err := Advance(graph, run, Event{Kind: EventTaskFinished, Node: "critic", Reply: "read"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if len(commands) != 1 || commands[0].Kind != CommandDispatch {
		t.Fatalf("the run asked for %+v, want the next reading", commands)
	}
	if !strings.Contains(commands[0].Prompt, "1. which store holds the text") {
		t.Fatalf("the next reading is given %q, want the open row in it", commands[0].Prompt)
	}
}

// The shipped file, held to the two things that make it this feature rather than three sessions
// reading in a row: the roles are roles the crew holds, and the question carries rows and no reply.
func TestTheShippedPlanReadingGraphAsksWithRowsAndNamesRolesTheCrewHolds(t *testing.T) {
	body, err := os.ReadFile("../../flows/plan-reading.yaml")
	if err != nil {
		t.Fatalf("reading the graph: %v", err)
	}
	graph, err := Parse(body)
	if err != nil {
		t.Fatalf("the shipped graph does not parse: %v", err)
	}

	readers := 0
	for name, node := range graph.Nodes {
		if node.Type != NodeDispatch {
			continue
		}
		readers++
		// The reading has to be handed the plan it is told to read. A prompt that says "read the plan"
		// and renders none gives the lens nothing, and every reading then reports on nothing while the
		// run walks its whole success path.
		if !strings.Contains(node.Prompt, "{{"+PlanKey+"}}") {
			t.Errorf("reading %s is told to read a plan and is handed none: %q", name, node.Prompt)
		}
		if node.Role == "" {
			t.Errorf("reading %s names no role, so two readings would run through one lens", name)
			continue
		}
		if _, err := os.Stat("../../roles/" + node.Role); err != nil {
			t.Errorf("reading %s names the role %q, which the crew does not hold: %v", name, node.Role, err)
		}
	}
	if readers < 3 {
		t.Fatalf("the graph holds %d readings, and one lens is what this replaces", readers)
	}

	asked := 0
	for name, node := range graph.Nodes {
		if node.Type != NodeAsk {
			continue
		}
		asked++
		if !strings.Contains(node.Text, "{{"+QuestionsKey+"}}") {
			t.Errorf("ask node %s does not render the open rows: %q", name, node.Text)
		}
		// A question that renders a reading is a question carrying somebody's prose rather than the
		// holes in it, which is the gate becoming the thing nobody reads.
		if strings.Contains(node.Text, "result.reply") || strings.Contains(node.Text, ReplyKeyPrefix) {
			t.Errorf("ask node %s renders a reading rather than the rows: %q", name, node.Text)
		}
	}
	if asked != 1 {
		t.Fatalf("the graph asks %d times, and a person is asked once", asked)
	}
}
