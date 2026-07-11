package model_test

import (
	"context"
	"errors"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

func TestEchoRunnerExecsInsideTheSandbox(t *testing.T) {
	box := &sandbox.FakeSandbox{Output: "hello there\n"}

	resp, err := model.EchoRunner{}.Run(context.Background(), box, model.Request{Text: "hello there", Workdir: "/work"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if resp.Reply != "hello there" {
		t.Fatalf("Reply = %q, want %q", resp.Reply, "hello there")
	}
	if resp.ModelSessionID == "" {
		t.Fatal("ModelSessionID is empty, want a thread id so a turn can be resumed")
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
	if box.LastSpec.Workdir != "/work" {
		t.Fatalf("Workdir = %q, want %q", box.LastSpec.Workdir, "/work")
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
	runner, err := model.NewRunner("echo", "")
	if err != nil {
		t.Fatalf("NewRunner(echo): %v", err)
	}
	if _, ok := runner.(model.EchoRunner); !ok {
		t.Fatalf("NewRunner(echo) = %T, want model.EchoRunner", runner)
	}
}

func TestNewRunnerRejectsUnknownKinds(t *testing.T) {
	if _, err := model.NewRunner("gpt", ""); err == nil {
		t.Fatal("NewRunner(gpt) = nil error, want error")
	}
}
