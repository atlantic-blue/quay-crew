package flow

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/model"
)

// A graph that has to clone something before it can read it, which is the shape that found this: a
// run's thread starts empty, and cloning needs more room than a thread is born with.
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
// a run that exists, has a thread of its own, and fails on its first dispatch.
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

// A graph that says nothing keeps saying nothing, so the thread's own birth mode decides and this
// change moves no automation that already exists.
func TestAGraphThatDeclaresNoModeCarriesNone(t *testing.T) {
	graph, err := Parse([]byte(`
name: plain
version: 1
nodes:
  go: { type: dispatch, prompt: "go" }
edges:
  - [go, done]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if graph.Mode != "" {
		t.Fatalf("a graph declaring no mode carries %q", graph.Mode)
	}
}
