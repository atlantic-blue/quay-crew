package flow

import (
	"strings"
	"testing"
)

// The graph the first real run should have been: it says what reading the repository would leave
// behind, so a task that could not find the repository cannot report success.
const provingGraph = `
name: site-check
version: 1
mode: edits
nodes:
  read: { type: dispatch, prompt: "read package.json and say what runs the tests", expect: { file: package.json } }
  ok:   { type: choice, on: { result.failed: "false" } }
  tell: { type: dispatch, prompt: "summarise the project" }
edges:
  - [read, ok]
  - [ok, tell, "true"]
  - [ok, done, "false"]
  - [tell, done]
`

func TestADispatchNodeDeclaresWhatProvesItWorked(t *testing.T) {
	graph, err := Parse([]byte(provingGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	expect := graph.Nodes["read"].Expect
	if expect == nil {
		t.Fatal("the node expects nothing, so nothing would be checked")
	}
	if expect.File != "package.json" {
		t.Fatalf("the node expects %q, want package.json", expect.File)
	}
}

// The one that matters. A task that could not do the work is not a failed task, so before this the
// run took the success edge and finished at done with the model's account of work that never
// happened. It stops instead, and says what it was looking for.
//
// What a claim is checked against lives on the job the step goes out as, and the job
// controller reads it after the task: TestAnAnswerThatDoesNotCarryWhatWasClaimedStopsTheJob and
// TestAFileTheJobClaimedIsCheckedAndItsAbsenceStopsTheJob in internal/job. This is the half the
// reducer owns, which is what a run does about it.
func TestAnUnmetExpectationStopsTheRun(t *testing.T) {
	graph, err := Parse([]byte(provingGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	run := Run{ID: "r1", Node: "read", Status: StatusRunning, State: map[string]string{}, Attempts: map[string]int{}}

	next, commands, err := Advance(graph, run, Event{
		Kind:  EventTaskFinished,
		Node:  "read",
		Reply: "the working directory is empty, so I summarised the project from memory",
		Unmet: "package.json is not in the session that did the job",
	})
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if next.Status != StatusStopped {
		t.Fatalf("the run is %s at %s, want it stopped", next.Status, next.Node)
	}
	if !strings.Contains(next.Reason, "package.json") || !strings.Contains(next.Reason, "read") {
		t.Errorf("the run stopped saying %q, want it to name the file and the node", next.Reason)
	}
	if next.State["result.expected"] == "" {
		t.Error("the run carries nothing about what was expected, so reading it back says only that it stopped")
	}
	// No archive. A run that stopped is one somebody has to look into, and its session is where the
	// evidence is; a finished run is the one that puts itself away.
	if len(commands) != 0 {
		t.Errorf("the stopped run asked for %d commands, want none", len(commands))
	}
}

// The other direction, or a check that stops every run would pass the case above and be useless.
func TestAMetExpectationCarriesTheRunOn(t *testing.T) {
	graph, err := Parse([]byte(provingGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	run := Run{ID: "r1", Node: "read", Status: StatusRunning, State: map[string]string{}, Attempts: map[string]int{}}

	next, commands, err := Advance(graph, run, Event{
		Kind: EventTaskFinished, Node: "read", Reply: "the tests run with npm test",
	})
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if next.Status != StatusRunning || next.Node != "tell" {
		t.Fatalf("the run is %s at %s, want it running at tell", next.Status, next.Node)
	}
	if len(commands) != 1 || commands[0].Kind != CommandDispatch {
		t.Fatalf("the run asked for %v, want the next dispatch", commands)
	}
}

func TestANodeThatDoesNoJobCannotSayWhatProvesItWorked(t *testing.T) {
	_, err := Parse([]byte(`
name: confused
version: 1
mode: edits
nodes:
  go:   { type: dispatch, prompt: "go" }
  next: { type: choice, on: { result.failed: "false" }, expect: { file: package.json } }
edges:
  - [go, next]
  - [next, done, "true"]
  - [next, done, "false"]
`))
	if err == nil {
		t.Fatal("a choice node declared what proves it worked")
	}
	if !strings.Contains(err.Error(), "next") {
		t.Errorf("the refusal says %q, want it to name the node", err)
	}
}

func TestAnExpectationOfNothingIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: empty
version: 1
mode: edits
nodes:
  go: { type: dispatch, prompt: "go", expect: {} }
edges:
  - [go, done]
`))
	if err == nil {
		t.Fatal("a node expecting nothing at all parsed")
	}
	if !strings.Contains(err.Error(), "file") || !strings.Contains(err.Error(), "contains") {
		t.Errorf("the refusal says %q, want it to name both ways of saying it", err)
	}
}

// The path is read inside the session's own working directory, so a graph cannot point the check at
// the machine the system runs on.
func TestAnExpectedFileOutsideTheSessionIsRefused(t *testing.T) {
	for _, path := range []string{"/etc/passwd", "../../etc/passwd", "up/../../out"} {
		_, err := Parse([]byte(`
name: nosy
version: 1
mode: edits
nodes:
  go: { type: dispatch, prompt: "go", expect: { file: "` + path + `" } }
edges:
  - [go, done]
`))
		if err == nil {
			t.Errorf("a node expecting %q parsed", path)
		}
	}
}

// The second half of quay-crew#461. A run that stopped over an unmet claim recorded the finding under
// `result.expected` and nothing under any other key, so `quay flow show` printed the same sentence
// twice and never said what the graph had asked for. A reader could not tell whether the graph wanted
// pr-state.md or wanted something else and got pr-state.md wrong.
func TestAStoppedRunSaysWhatItWantedApartFromWhatItFound(t *testing.T) {
	graph, err := Parse([]byte(provingGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	run := Run{ID: "r1", Node: "read", Status: StatusRunning, State: map[string]string{}, Attempts: map[string]int{}}

	found := "package.json is not in the session that did the job"
	next, _, err := Advance(graph, run, Event{
		Kind: EventTaskFinished, Node: "read",
		Reply: "the working directory is empty, so I summarised the project from memory",
		Unmet: found,
	})
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if want := "the file package.json"; next.State["result.expected"] != want {
		t.Errorf("the run expected %q, want %q: the claim the graph declared, not the finding",
			next.State["result.expected"], want)
	}
	if next.State["result.unmet"] != found {
		t.Errorf("the run found %q, want %q", next.State["result.unmet"], found)
	}
	// The two must not be the same sentence, which is the whole defect: one field held both jobs and
	// so did neither.
	if next.State["result.expected"] == next.State["result.unmet"] {
		t.Error("the claim and the finding read identically, so the reader learns nothing from having both")
	}
	// A run that stopped because its step did not do its job reads failed, because that is what the
	// word means. It read false.
	if next.State["result.failed"] != "true" {
		t.Errorf("the run reads result.failed %q on a step that did not do its job, want true",
			next.State["result.failed"])
	}
}

// A claim of both kinds says both, or a graph whose file arrived and whose sentence did not would be
// reported as wanting only the file.
func TestAClaimOfBothKindsIsDeclaredInFull(t *testing.T) {
	declared := Expect{File: "pr-state.md", Contains: "all green"}.Declared()
	if !strings.Contains(declared, "pr-state.md") || !strings.Contains(declared, "all green") {
		t.Errorf("a node claiming a file and a phrase declares %q, want both", declared)
	}
}

// A step that errored still reads failed, or the field would have swapped one narrow meaning for
// another rather than covering the step failing to do its job.
func TestAStepTheModelErroredOnStillReadsFailed(t *testing.T) {
	graph, err := Parse([]byte(provingGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	run := Run{ID: "r1", Node: "read", Status: StatusRunning, State: map[string]string{}, Attempts: map[string]int{}}
	next, _, err := Advance(graph, run, Event{
		Kind: EventTaskFinished, Node: "read", Reply: "the sandbox died", Failed: true,
	})
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if next.State["result.failed"] != "true" {
		t.Errorf("a step the model errored on reads result.failed %q, want true", next.State["result.failed"])
	}
	if next.State["result.unmet"] != "" {
		t.Errorf("a step that errored records the unmet claim %q, and it met no claim either way",
			next.State["result.unmet"])
	}
}
