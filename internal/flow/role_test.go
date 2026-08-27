package flow

import (
	"strings"
	"testing"
)

// The graph these tests drive: one step in the run's own session, one step as a role. A role is
// what makes a step somebody else's work rather than the next thing this conversation does.
const teamGraph = `
name: write-tests
version: 1
nodes:
  plan:  { type: dispatch, prompt: "say what needs testing" }
  tests: { type: dispatch, role: test-writer, prompt: "write the tests for {{plan}}" }
edges:
  - [plan, tests]
  - [tests, done]
`

func TestADispatchCanNameARole(t *testing.T) {
	graph, err := Parse([]byte(teamGraph))
	if err != nil {
		t.Fatalf("parsing the graph: %v", err)
	}
	if got := graph.Nodes["tests"].Role; got != "test-writer" {
		t.Errorf("the step runs as %q, want test-writer", got)
	}
	if got := graph.Nodes["plan"].Role; got != "" {
		t.Errorf("a step naming no role runs as %q, want nobody in particular", got)
	}
}

// A role on anything but a dispatch is refused rather than dropped. Dropped, it reads as a boundary
// that is in force, and a boundary that is not in force looks exactly like one that is.
func TestOnlyAStepThatDoesWorkMayNameARole(t *testing.T) {
	_, err := Parse([]byte(`
name: confused
version: 1
nodes:
  first:  { type: dispatch, prompt: "do it" }
  branch: { type: choice, role: test-writer, on: { result.failed: "false" } }
edges:
  - [first, branch]
  - [branch, done, "true"]
  - [branch, done, "false"]
`))
	if err == nil {
		t.Fatal("a choice naming a role parsed, and the role would do nothing")
	}
	if !strings.Contains(err.Error(), "test-writer") {
		t.Errorf("the refusal says %q, want it to name the role", err)
	}
}

func TestARoleNameThatCouldNeverMatchARoleIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: shouting
version: 1
nodes:
  tests: { type: dispatch, role: "Test Writer", prompt: "write them" }
edges:
  - [tests, done]
`))
	if err == nil {
		t.Fatal("a role name no role could carry parsed")
	}
	if !strings.Contains(err.Error(), "Test Writer") {
		t.Errorf("the refusal says %q, want it to name what was wrong", err)
	}
}

// A run has to be able to reach every conversation it started, because what it archives and what it
// counts as spent both come from this list.
func TestARunKnowsEverySessionItStarted(t *testing.T) {
	run := Run{State: map[string]string{
		// A run made before its steps were work kept its own session under this key, and reading one
		// back has to still reach it.
		SessionKey:      "own-session",
		"session.tests": "tests-session",
		"session.docs":  "docs-session",
		"result.reply":  "not a session at all",
	}}

	got := run.Sessions()
	want := []string{"own-session", "docs-session", "tests-session"}
	if len(got) != len(want) {
		t.Fatalf("the run knows %v, want %v", got, want)
	}
	for at := range want {
		if got[at] != want[at] {
			t.Fatalf("the run knows %v, want %v", got, want)
		}
	}
}

func TestARunThatDispatchedNothingKnowsNoSessions(t *testing.T) {
	if got := (Run{State: map[string]string{"result.reply": "hello"}}).Sessions(); len(got) != 0 {
		t.Errorf("a run that never dispatched knows %v", got)
	}
}
