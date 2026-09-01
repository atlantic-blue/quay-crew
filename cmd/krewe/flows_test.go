package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// The graph these tests import: one dispatch and the end, so a run finishes with nothing to branch
// on and the assertions are about the command line rather than about the reducer.
const oneStepGraph = `
name: greet
version: 1
mode: edits
nodes:
  say: { type: dispatch, prompt: "hello {{who}}" }
edges:
  - [say, done]
`

// flowSystem is a system with its two loops running, which is what a flow needs now: a run declares its
// step as a job and returns, the job controller sends the task, and the poller carries the
// run on when the job ends. A system with the loops stopped holds a run at its first step forever.
func flowSystem(t *testing.T) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	return flowSystemWith(t, controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
}

func flowSystemWith(t *testing.T, cfg controlplane.Config) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	// Faster than the system ticks in production, because these tests poll for an answer and the real
	// five seconds would be five seconds of sleeping per step.
	cfg.JobTickEvery, cfg.FlowPollEvery = 5*time.Millisecond, 5*time.Millisecond
	srv := controlplane.NewServer(cfg)
	client := testClientFor(t, srv)
	running, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	go srv.RunJobController(running)
	go srv.RunFlowPoller(running)
	return client
}

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
func TestKreweFlowImportsStartsAndShows(t *testing.T) {
	client := flowSystem(t)
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
		t.Fatalf("showing the run does not carry what the task replied: %q", shown)
	}
}

// A run that was halted and a run that went quiet must not read the same, so showing a stopped run
// says why on its own line.
func TestKreweFlowShowSaysWhyARunStopped(t *testing.T) {
	client := flowSystem(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	mustRun(t, client, "flow", "import", graphFile(t, `
name: loop
version: 1
mode: edits
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

// Stopping a run from the command line: it says what it stopped, why, and what happens to the task
// already under way, because that last part is the thing an operator will wonder about.
func TestKreweFlowStopHaltsARunAndSaysWhat(t *testing.T) {
	// A model that takes a moment, so the run is genuinely still working when the stop lands.
	// With an instant one the automation reaches its cap before a second command can be typed, and
	// this would be racing rather than testing.
	client := flowSystemWith(t, controlplane.Config{
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
mode: edits
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
		t.Fatalf("stopping said %q, want it to say what happens to the task in flight", stopped)
	}

	shown := mustRun(t, client, "flow", "show", fields[0])
	if !strings.Contains(shown, "stopped") || !strings.Contains(shown, "it is fixing the wrong thing") {
		t.Fatalf("showing the run said %q, want it stopped with its reason", shown)
	}
}

// A run waiting on a person is the one state the operator has to act on, so showing it says the
// question and how to answer, and answering it carries the run on.
func TestKreweFlowAnswerCarriesARunOn(t *testing.T) {
	client := flowSystem(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	mustRun(t, client, "flow", "import", graphFile(t, `
name: careful
version: 1
mode: edits
nodes:
  fix:    { type: dispatch, prompt: "fix the build" }
  permit: { type: ask, text: "fixed it locally. push?" }
  yes:    { type: choice, on: { answer: "yes" } }
  push:   { type: dispatch, prompt: "push it" }
edges:
  - [fix, permit]
  - [permit, yes]
  - [yes, push, "true"]
  - [yes, done, "false"]
  - [push, done]
`))
	mustRun(t, client, "flow", "start", "careful")

	fields := strings.Fields(mustRun(t, client, "flow", "list"))
	if len(fields) == 0 {
		t.Fatal("the listing is empty")
	}
	// Polled, because starting answers before the run has moved: a run is driven on a goroutine, so
	// reading it straight after starting it reads a run still at its first node. Under load that is
	// what happens, and the test then reports the product as broken.
	shown := showWhen(t, client, fields[0], "fixed it locally. push?")
	if !strings.Contains(shown, "fixed it locally. push?") {
		t.Fatalf("showing an asking run said %q, want the question it is waiting on", shown)
	}
	if !strings.Contains(shown, "krewe flow answer") {
		t.Fatalf("showing an asking run said %q, want how to answer it", shown)
	}

	answered := mustRun(t, client, "flow", "answer", fields[0], "yes")
	if !strings.Contains(answered, "yes") {
		t.Fatalf("answering said %q", answered)
	}
	// Polled for the same reason: the answer moves the run to its next step, and that step is a
	// a job a controller runs rather than a call the answer waits on.
	after := showWhen(t, client, fields[0], "done")
	if !strings.Contains(after, "done") {
		t.Fatalf("after the answer the run reads %q, want it carried on to the end", after)
	}
}

// A graph that could not run is refused before it is sent anywhere, so the operator reads the
// reason at the moment they wrote the file.
func TestKreweFlowRefusesAGraphARunCouldFallOff(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	_, err := runKrewe(t, client, "flow", "import", graphFile(t, `
name: broken
version: 1
mode: edits
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
func TestKreweFlowStartNeedsAProject(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")

	_, err := runKrewe(t, client, "flow", "start", "greet")
	if err == nil {
		t.Fatal("a flow started with no project")
	}
	// The command line's own guidance, not the control plane's "project not found": both say the
	// word project, so asserting on that alone would pass however deep the failure happened.
	if !strings.Contains(err.Error(), "krewe flow start <workspace>/<project>") {
		t.Errorf("the refusal says %q, want it to show the address a flow needs", err)
	}
}

// The usage line names every subcommand, so a typo does not read as a missing feature.
func TestKreweFlowNamesWhatItCanDo(t *testing.T) {
	client := testClient(t)
	for _, args := range [][]string{{"flow"}, {"flow", "wat"}} {
		_, err := runKrewe(t, client, args...)
		if err == nil {
			t.Fatalf("krewe %s was accepted", strings.Join(args, " "))
		}
		for _, verb := range []string{"import", "start", "list", "show", "stop", "answer"} {
			if !strings.Contains(err.Error(), verb) {
				t.Errorf("krewe %s says %q, want it to name %s", strings.Join(args, " "), err, verb)
			}
		}
	}
}

// showWhen shows a run repeatedly until what it says carries want, and fails with what it said last
// if it never does.
//
// A run is driven on a goroutine, so starting one answers before it has moved. Every assertion about
// where a run got to therefore has to wait for it rather than read it once, or it is a race that
// passes on an idle machine and fails on a loaded one.
func showWhen(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, run, want string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var shown string
	for {
		shown = mustRun(t, client, "flow", "show", run)
		if strings.Contains(shown, want) {
			return shown
		}
		if time.Now().After(deadline) {
			return shown
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The pointer a finished run prints has to be a command that works, which is why this runs it.
//
// A run ends by archiving its own session, and the summary `krewe flow show` prints is the model's own
// account of what happened. The tasks are the record of it, and the two can disagree: a run has
// reported four transitions and a tidy summary while every task under it said the working directory
// was empty. Showing the run printed a session identifier and the command that reads a session refused
// that exact identifier, so the record was reachable through the database alone.
func TestShowingAFinishedRunPrintsAWorkingWayToReadItsTasks(t *testing.T) {
	client := flowSystem(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "flow", "import", graphFile(t, oneStepGraph))
	mustRun(t, client, "flow", "start", "greet")

	deadline := time.Now().Add(10 * time.Second)
	var shown string
	for {
		listed := mustRun(t, client, "flow", "list")
		fields := strings.Fields(listed)
		if len(fields) > 0 {
			shown = mustRun(t, client, "flow", "show", fields[0])
			if strings.Contains(shown, "done") {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run never finished: %q", shown)
		}
		time.Sleep(10 * time.Millisecond)
	}

	typed := typedCommandIn(t, shown, "krewe task list ")
	read, err := runKrewe(t, client, typed[1:]...)
	if err != nil {
		t.Fatalf("the run said to type %q, and that was refused: %v", strings.Join(typed, " "), err)
	}
	// The prompt the graph dispatched. The reply comes from a double, so what proves the history is
	// the run's own is what the run asked.
	if !strings.Contains(read, "hello") {
		t.Fatalf("%q answered %q, want the task the run dispatched", strings.Join(typed, " "), read)
	}
}

// typedCommandIn finds the command an output tells the operator to type, so a test can type it.
func typedCommandIn(t *testing.T, output, starting string) []string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		at := strings.Index(line, starting)
		if at < 0 {
			continue
		}
		return strings.Fields(line[at:])
	}
	t.Fatalf("nothing in %q says to type %q", output, starting)
	return nil
}

// The pointer a run prints to its own job has to be a command that works, so this types it.
//
// A run is carried by a job and every step is another under it, which is where a step's
// answer is a field rather than a line of a transcript. That is worth nothing if reading a run does
// not say how to get there.
func TestShowingARunPrintsAWorkingWayToReadItsSteps(t *testing.T) {
	client := flowSystem(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "flow", "import", graphFile(t, oneStepGraph))
	mustRun(t, client, "flow", "start", "greet")

	deadline := time.Now().Add(10 * time.Second)
	var shown string
	for {
		listed := mustRun(t, client, "flow", "list")
		fields := strings.Fields(listed)
		if len(fields) > 0 {
			shown = mustRun(t, client, "flow", "show", fields[0])
			if strings.Contains(shown, "done") {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run never finished: %q", shown)
		}
		time.Sleep(10 * time.Millisecond)
	}

	typed := typedCommandIn(t, shown, "krewe job list ")
	read, err := runKrewe(t, client, typed[1:]...)
	if err != nil {
		t.Fatalf("the run said to type %q, and that was refused: %v", strings.Join(typed, " "), err)
	}
	// The run's own job and the one step it took, both under the label the run carries.
	if !strings.Contains(read, "greet") {
		t.Fatalf("%q answered %q, want the run's own job and its step", strings.Join(typed, " "), read)
	}
	if !strings.Contains(read, "step say") {
		t.Fatalf("%q answered %q, want the step the graph dispatched", strings.Join(typed, " "), read)
	}
}
