package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// The graph these tests import: one dispatch and the end, so a run finishes with nothing to branch
// on and the assertions are about the command line rather than about the reducer.
const oneStepGraph = `
name: greet
version: 1
nodes:
  say: { type: dispatch, prompt: "hello {{who}}" }
edges:
  - [say, done]
`

func graphFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "graph.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write the graph: %v", err)
	}
	return path
}

// The whole operator surface in one pass: import a file, start a run in a project, and read it
// back. A run advances behind the answer that started it, so the listing is polled rather than read
// once.
func TestQuayFlowImportsStartsAndShows(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	imported := mustRun(t, client, "flow", "import", graphFile(t, oneStepGraph))
	if !strings.Contains(imported, "greet") || !strings.Contains(imported, "version 1") {
		t.Fatalf("importing said %q, want the graph and the version it landed at", imported)
	}

	started := mustRun(t, client, "flow", "start", "greet")
	if !strings.Contains(started, "greet") {
		t.Fatalf("starting said %q, want the run it started", started)
	}

	// The run is listed and reaches done. Polled, because starting answers before the run finishes.
	deadline := time.Now().Add(10 * time.Second)
	for {
		listed := mustRun(t, client, "flow", "list")
		if strings.Contains(listed, "greet") && strings.Contains(listed, "done") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the listing never showed the run as done: %q", listed)
		}
		time.Sleep(10 * time.Millisecond)
	}

	listed := mustRun(t, client, "flow", "list")
	fields := strings.Fields(listed)
	if len(fields) == 0 {
		t.Fatalf("the listing is empty: %q", listed)
	}
	shown := mustRun(t, client, "flow", "show", fields[0])
	if !strings.Contains(shown, "greet") || !strings.Contains(shown, "done") {
		t.Fatalf("showing the run said %q, want the graph and where it ended", shown)
	}
	if !strings.Contains(shown, "result.reply") {
		t.Fatalf("showing the run does not carry what the turn replied: %q", shown)
	}
}

// A run that was halted and a run that went quiet must not read the same, so showing a stopped run
// says why on its own line.
func TestQuayFlowShowSaysWhyARunStopped(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	mustRun(t, client, "flow", "import", graphFile(t, `
name: loop
version: 1
limits:
  transitions: 3
nodes:
  begin: { type: dispatch, prompt: "begin" }
  more:  { type: choice, on: { result.failed: "false" } }
  again: { type: dispatch, prompt: "again" }
edges:
  - [begin, more]
  - [more, again, "true"]
  - [more, done, "false"]
  - [again, more]
`))
	mustRun(t, client, "flow", "start", "loop")

	deadline := time.Now().Add(10 * time.Second)
	var shown string
	for {
		listed := mustRun(t, client, "flow", "list")
		fields := strings.Fields(listed)
		if len(fields) > 0 {
			shown = mustRun(t, client, "flow", "show", fields[0])
			if strings.Contains(shown, "stopped") {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run never stopped at its cap: %q", shown)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// On the guidance the reason carries, not on the word "transitions": the status line prints a
	// transition count too, so matching that would stay green with the reason gone entirely.
	if !strings.Contains(shown, "raise limits.transitions") {
		t.Fatalf("showing the stopped run said %q, want the reason it stopped and what to do about it", shown)
	}
}

// Stopping a run from the command line: it says what it stopped, why, and what happens to the turn
// already under way, because that last part is the thing an operator will wonder about.
func TestQuayFlowStopHaltsARunAndSaysWhat(t *testing.T) {
	// A model that takes a moment, so the run is genuinely still working when the stop lands.
	// With an instant one the automation reaches its cap before a second command can be typed, and
	// this would be racing rather than testing.
	client := testClientWith(t, controlplane.Config{
		Store:    store.NewMemory(),
		Runner:   &model.FakeRunner{Reply: "ok", Takes: 200 * time.Millisecond},
		Provider: &sandbox.FakeProvider{},
		Secrets:  secrets.NewMemory(),
	})
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	mustRun(t, client, "flow", "import", graphFile(t, `
name: loop
version: 1
limits:
  transitions: 50
nodes:
  begin: { type: dispatch, prompt: "begin" }
  more:  { type: choice, on: { result.failed: "false" } }
  again: { type: dispatch, prompt: "again" }
edges:
  - [begin, more]
  - [more, again, "true"]
  - [more, done, "false"]
  - [again, more]
`))
	mustRun(t, client, "flow", "start", "loop")

	fields := strings.Fields(mustRun(t, client, "flow", "list"))
	if len(fields) == 0 {
		t.Fatal("the listing is empty")
	}
	stopped := mustRun(t, client, "flow", "stop", fields[0], "it is fixing the wrong thing")
	if !strings.Contains(stopped, "it is fixing the wrong thing") {
		t.Fatalf("stopping said %q, want the reason it was given", stopped)
	}
	if !strings.Contains(stopped, "already under way finishes") {
		t.Fatalf("stopping said %q, want it to say what happens to the turn in flight", stopped)
	}

	shown := mustRun(t, client, "flow", "show", fields[0])
	if !strings.Contains(shown, "stopped") || !strings.Contains(shown, "it is fixing the wrong thing") {
		t.Fatalf("showing the run said %q, want it stopped with its reason", shown)
	}
}

// A graph that could not run is refused before it is sent anywhere, so the operator reads the
// reason at the moment they wrote the file.
func TestQuayFlowRefusesAGraphARunCouldFallOff(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	_, err := runQuay(t, client, "flow", "import", graphFile(t, `
name: broken
version: 1
nodes:
  a: { type: dispatch, prompt: "a" }
edges:
  - [a, nowhere]
`))
	if err == nil {
		t.Fatal("a graph whose edge leads nowhere was imported")
	}
	if !strings.Contains(err.Error(), "nowhere") {
		t.Errorf("the refusal says %q, want it to name the undeclared node", err)
	}
}

// A run needs a project, because a dispatch does. Said plainly rather than failing inside the
// control plane with an empty identifier.
func TestQuayFlowStartNeedsAProject(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")

	_, err := runQuay(t, client, "flow", "start", "greet")
	if err == nil {
		t.Fatal("a flow started with no project")
	}
	// The command line's own guidance, not the control plane's "project not found": both say the
	// word project, so asserting on that alone would pass however deep the failure happened.
	if !strings.Contains(err.Error(), "quay flow start <workspace>/<project>") {
		t.Errorf("the refusal says %q, want it to show the address a flow needs", err)
	}
}

// The usage line names every subcommand, so a typo does not read as a missing feature.
func TestQuayFlowNamesWhatItCanDo(t *testing.T) {
	client := testClient(t)
	for _, args := range [][]string{{"flow"}, {"flow", "wat"}} {
		_, err := runQuay(t, client, args...)
		if err == nil {
			t.Fatalf("quay %s was accepted", strings.Join(args, " "))
		}
		for _, verb := range []string{"import", "start", "list", "show", "stop"} {
			if !strings.Contains(err.Error(), verb) {
				t.Errorf("quay %s says %q, want it to name %s", strings.Join(args, " "), err, verb)
			}
		}
	}
}
