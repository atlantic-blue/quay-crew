package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// Dispatching used to hold the task in the client for as long as the work took, so the terminal was
// the weakest part of the crew: a dispatch killed at seventeen minutes recorded "failed: model: run
// exited: signal: killed", said nothing about why, and the work was gone. The control plane could
// always run a task behind the answer. Only the console could ask for it.
//
// The tasks below hold the model open rather than timing it. What is being tested is whether a
// command comes back before its task does, and a test that waits a duration for that passes on a
// fast machine by accident.

// aHeldCrew is a crew whose model will not answer until the returned func lets it, and which says
// when a task has genuinely reached it.
func aHeldCrew(t *testing.T) (quaycrewv1.ControlPlaneServiceClient, *model.FakeRunner) {
	t.Helper()
	runner := &model.FakeRunner{
		Reply: "the electricity bill is due on the ninth",
		Gate:  make(chan struct{}), Started: make(chan struct{}),
	}
	client := testClientWith(t, controlplane.Config{
		Store: store.NewMemory(), Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	return client, runner
}

// The whole point of the change: the command comes back while the model is still working.
func TestDispatchLetsGoOfTheTask(t *testing.T) {
	client, runner := aHeldCrew(t)

	said := mustRun(t, client, "dispatch", "when is the electricity bill due")

	select {
	case <-runner.Started:
	case <-time.After(5 * time.Second):
		t.Fatal("the task never reached the model")
	}
	// The model has not answered and cannot have: nothing has let it go.
	if strings.Contains(said, "the electricity bill is due on the ninth") {
		t.Fatalf("dispatch waited for the model after all: %q", said)
	}
	// And it says where the answer will be, because a blank where a reply used to be reads as a task
	// that answered nothing.
	for _, want := range []string{"quay tasks", "quay attach", "handle "} {
		if !strings.Contains(said, want) {
			t.Fatalf("dispatch does not say %q: %q", want, said)
		}
	}

	close(runner.Gate)
	handle := handleFrom(t, said)
	waitForTheAnswer(t, client, handle)
	if history := mustRun(t, client, "tasks", handle); !strings.Contains(history, "the electricity bill is due on the ninth") {
		t.Fatalf("the answer never landed in the history:\n%s", history)
	}
}

// Somebody typing a short question is looking at the terminal, so one command still answers there.
func TestAskWaitsForTheAnswer(t *testing.T) {
	client, runner := aHeldCrew(t)

	answered := make(chan string, 1)
	go func() {
		var out bytes.Buffer
		if err := run(context.Background(), client,
			[]string{"ask", "when is the electricity bill due"}, &out, ""); err != nil {
			answered <- "failed: " + err.Error()
			return
		}
		answered <- out.String()
	}()

	select {
	case <-runner.Started:
	case <-time.After(5 * time.Second):
		t.Fatal("the task never reached the model")
	}
	// Held open, so anything arriving here would be an answer nobody has given yet.
	select {
	case early := <-answered:
		t.Fatalf("ask came back before the model did: %q", early)
	case <-time.After(50 * time.Millisecond):
	}

	close(runner.Gate)
	select {
	case said := <-answered:
		if !strings.Contains(said, "the electricity bill is due on the ninth") {
			t.Fatalf("ask does not print the answer: %q", said)
		}
		if !strings.Contains(said, "handle ") {
			t.Fatalf("ask does not say which session answered: %q", said)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ask never came back")
	}
}

// The work is the crew's now, so a caller that goes away does not take it. This is the failure that
// started it: seventeen minutes of work lost with the terminal that asked for it.
func TestATaskOutlivesTheCommandThatStartedIt(t *testing.T) {
	client, runner := aHeldCrew(t)

	said := mustRun(t, client, "dispatch", "read the repository")
	handle := handleFrom(t, said)
	select {
	case <-runner.Started:
	case <-time.After(5 * time.Second):
		t.Fatal("the task never reached the model")
	}

	// The caller is gone. Nothing here is holding the task open any more.
	close(runner.Gate)
	waitForTheAnswer(t, client, handle)

	session := onlySession(t, client)
	if session.GetStatus() != "idle" {
		t.Fatalf("the session reads %q after its task landed, want idle", session.GetStatus())
	}
	if history := mustRun(t, client, "tasks", handle); strings.Contains(history, "failed") {
		t.Fatalf("the task failed once nobody was waiting for it:\n%s", history)
	}
}

// The way off the shapes somebody's fingers reach for. A flag that is quietly ignored is worse than
// one that never existed, so each of these names the word to type instead.
func TestTheFlagsForWaitingAndLettingGoAreRefusedByName(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	for _, testCase := range []struct{ flag, names string }{
		{"--detach", "quay tasks"},
		{"--wait", "quay ask"},
		{"--no-wait", "quay dispatch"},
	} {
		err := refused(t, client, "dispatch", testCase.flag, "hello")
		if !strings.Contains(err.Error(), testCase.names) {
			t.Errorf("%s is refused with %q, which does not name what to type instead", testCase.flag, err)
		}
		// And the flag is never swallowed into the message, which is the defect this shape avoids.
		if strings.Contains(err.Error(), "hello") {
			t.Errorf("%s took the message with it: %q", testCase.flag, err)
		}
	}
}

// Each command names itself when it is typed with nothing to say, or the usage line sends the
// operator to the other one.
func TestEachWayOfTalkingNamesItselfInItsUsage(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	for command, want := range map[string]string{
		"dispatch": "usage: quay dispatch",
		"ask":      "usage: quay ask",
	} {
		err := refused(t, client, command)
		if !strings.Contains(err.Error(), want) {
			t.Errorf("quay %s with nothing to say answers %q, want %q", command, err, want)
		}
	}
}

// handleFrom reads the handle out of what a command printed, the way the operator copies it.
func handleFrom(t *testing.T, said string) string {
	t.Helper()
	_, after, found := strings.Cut(said, "handle ")
	if !found {
		t.Fatalf("nothing printed a handle: %q", said)
	}
	return strings.TrimSuffix(strings.TrimSpace(after), ")")
}

// waitForTheAnswer waits until the crew has finished with a task it was let go of, so an assertion
// about what landed is never made against a task still in flight.
func waitForTheAnswer(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, handle string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		history, err := runQuay(t, client, "tasks", handle)
		// A history with nothing in it yet, or with a task still in flight, is not a landed task.
		if err == nil && strings.TrimSpace(history) != "" &&
			!strings.Contains(history, "no tasks recorded") && !strings.Contains(history, "still running") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the task never landed")
}
