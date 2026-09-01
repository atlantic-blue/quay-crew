package flow

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/model"
)

// A graph that has to clone something before it can read it, which is the shape that found this: a
// run's session starts empty, and cloning needs more room than a session is born with.
const cloningGraph = `
name: clone-first
version: 1
mode: dangerous
nodes:
  clone: { type: dispatch, prompt: "clone the repository into /home/agent/shared" }
edges:
  - [clone, done]
`

func TestAGraphDeclaresWhatItsRunsMayDo(t *testing.T) {
	graph, err := Parse([]byte(cloningGraph))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if graph.Mode != model.PermissionBypass {
		t.Fatalf("the graph runs in %q, want %q", graph.Mode, model.PermissionBypass)
	}
}

// The word a person types and the word the protocol spells both reach the same mode, because the
// listing prints one and the manual prints the other.
func TestAGraphTakesEitherSpellingOfAMode(t *testing.T) {
	for typed, want := range map[string]string{
		"dangerous":         model.PermissionBypass,
		"bypassPermissions": model.PermissionBypass,
		"plan":              model.PermissionPlan,
		"edits":             model.PermissionAcceptEdits,
		"acceptEdits":       model.PermissionAcceptEdits,
	} {
		graph, err := Parse([]byte(`
name: moded
version: 1
mode: ` + typed + `
nodes:
  go: { type: dispatch, prompt: "go" }
edges:
  - [go, done]
`))
		if err != nil {
			t.Fatalf("a graph in mode %q was refused: %v", typed, err)
		}
		if graph.Mode != want {
			t.Errorf("mode %q parsed as %q, want %q", typed, graph.Mode, want)
		}
	}
}

// Refused at import, which is the whole point of parsing a graph before it runs: the alternative is
// a run that exists, has a session of its own, and fails on its first dispatch.
func TestAGraphWhoseModeIsNotAModeIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: nonsense
version: 1
mode: whenever
nodes:
  go: { type: dispatch, prompt: "go" }
edges:
  - [go, done]
`))
	if err == nil {
		t.Fatal("a graph running in mode \"whenever\" parsed")
	}
	for _, offered := range model.PermissionModesOffered() {
		if !strings.Contains(err.Error(), offered) {
			t.Errorf("the refusal says %q, want it to offer %q", err, offered)
		}
	}
}

// The fault quay-crew#461 was opened for. A graph that said nothing used to parse, and its runs took
// the mode a session is born in, which is acceptEdits: file edits inside the working directory are
// approved and nothing else is. So every command a step ran, and every file it read outside that
// directory, stopped to ask a person who was not there. Run 68ffb98298125c2cb9017e4f sat on its first
// node through 532,978 tokens finding that out.
//
// Refusing is the safer of the two repairs. The other is a default wide enough to work unwatched,
// and that grants every graph already written more than its author asked for, silently. This says no
// and names the line.
func TestAGraphThatSaysNothingAboutItsModeIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: pr-sweep
version: 1
nodes:
  read: { type: dispatch, prompt: "read the open pull requests" }
edges:
  - [read, done]
`))
	if err == nil {
		t.Fatal("a graph saying nothing about its mode parsed, so a run of it would stop to ask a person who is not there")
	}
	if !strings.Contains(err.Error(), "pr-sweep") {
		t.Errorf("the refusal says %q, want it to name the graph", err)
	}
	// The line that is missing, so the refusal can be acted on without reading the manual.
	if !strings.Contains(err.Error(), "mode:") {
		t.Errorf("the refusal says %q, want it to name the line to add", err)
	}
	for _, offered := range model.PermissionModesOffered() {
		if !strings.Contains(err.Error(), offered) {
			t.Errorf("the refusal says %q, want it to offer %q", err, offered)
		}
	}
}
