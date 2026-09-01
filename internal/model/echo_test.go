package model_test

import (
	"context"
	"errors"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
)

func TestEchoRunnerExecsInsideTheSandbox(t *testing.T) {
	box := &sandbox.FakeSandbox{Output: "hello there\n"}

	resp, err := model.EchoRunner{}.Run(context.Background(), box, model.Request{Text: "hello there", Workdir: "/job"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if resp.Reply != "hello there" {
		t.Fatalf("Reply = %q, want %q", resp.Reply, "hello there")
	}
	if resp.ModelSessionID == "" {
		t.Fatal("ModelSessionID is empty, want a session id so a task can be resumed")
	}

	// The point of this runner is that the command really goes through the sandbox.
	want := []string{"echo", "hello there"}
	if len(box.LastSpec.Argv) != len(want) {
		t.Fatalf("Argv = %v, want %v", box.LastSpec.Argv, want)
	}
	for i, arg := range want {
		if box.LastSpec.Argv[i] != arg {
			t.Fatalf("Argv = %v, want %v", box.LastSpec.Argv, want)
		}
	}
	if box.LastSpec.Workdir != "/job" {
		t.Fatalf("Workdir = %q, want %q", box.LastSpec.Workdir, "/job")
	}
}

func TestEchoRunnerRequiresASandbox(t *testing.T) {
	if _, err := (model.EchoRunner{}).Run(context.Background(), nil, model.Request{Text: "hi"}); err == nil {
		t.Fatal("Run(nil sandbox) = nil error, want error")
	}
}

func TestEchoRunnerSurfacesExecFailure(t *testing.T) {
	execErr := errors.New("sandbox is gone")
	box := &sandbox.FakeSandbox{Err: execErr}

	if _, err := (model.EchoRunner{}).Run(context.Background(), box, model.Request{Text: "hi"}); !errors.Is(err, execErr) {
		t.Fatalf("Run() = %v, want it to wrap %v", err, execErr)
	}
}

func TestNewRunnerBuildsTheEchoRunner(t *testing.T) {
	runner, err := model.NewRunner("echo", "", "")
	if err != nil {
		t.Fatalf("NewRunner(echo): %v", err)
	}
	if _, ok := runner.(model.EchoRunner); !ok {
		t.Fatalf("NewRunner(echo) = %T, want model.EchoRunner", runner)
	}
}

func TestNewRunnerRejectsUnknownKinds(t *testing.T) {
	if _, err := model.NewRunner("gpt", "", ""); err == nil {
		t.Fatal("NewRunner(gpt) = nil error, want error")
	}
}

// The echo backend stands in for a model runtime in the smoke test and in the composed stack, so it
// answers the way a runtime that honours the flag answers: with the conversation it was given. A
// double that reported a name of its own instead would read as the runtime ignoring the name on every
// task, and the check that catches a runtime ignoring the name would be crying wolf in the one place
// the system is driven end to end.
func TestEchoRunnerReportsTheConversationItWasGiven(t *testing.T) {
	box := &sandbox.FakeSandbox{Output: "hello\n"}
	const named = "1a2b3c4d-5e6f-4a8b-9c0d-1e2f3a4b5c6d"

	resp, err := model.EchoRunner{}.Run(context.Background(), box,
		model.Request{Text: "hello", ModelSessionID: named})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.ModelSessionID != named {
		t.Fatalf("it reports conversation %q, want the %q it was given", resp.ModelSessionID, named)
	}
	if said := model.ConversationCheck(named, resp.ModelSessionID); said != "" {
		t.Fatalf("the system reads this as the runtime ignoring the name: %s", said)
	}
}
