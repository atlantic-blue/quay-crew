package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/atlantic-blue/krewe/internal/secrets"
	"github.com/atlantic-blue/krewe/internal/store"
	"github.com/atlantic-blue/krewe/internal/telemetry"
)

// The trace context rides the task and never the sandbox.
//
// A sandbox is born with its environment and is then reused across every later task, so a value
// written at birth labels the tenth task with the first task's span for as long as the container
// lives. This is the test that says so: the container's own environment never carries it and the
// command's does.
func TestTheTraceContextRidesTheTaskAndNotTheSandbox(t *testing.T) {
	runner := &model.FakeRunner{Reply: "done"}
	provider := &sandbox.FakeProvider{}
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: runner, Provider: provider, Secrets: secrets.NewMemory(),
	})
	_, project := newProject(t, server)

	// Under a trace, the way a controller runs a job: the context comes off the row.
	ctx := telemetry.Under(context.Background(),
		"4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7")
	if _, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Text: "read the repository",
	}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// The command carries it.
	carried := runner.LastReq.Env[telemetry.TraceparentEnv]
	if carried == "" {
		t.Fatal("the task carries no trace context, so nothing inside the container could join the trace")
	}
	if !strings.Contains(carried, "4bf92f3577b34da6a3ce929d0e0e4736") {
		t.Fatalf("the task carries %q, which is not the trace it ran under", carried)
	}

	// The container does not, and this is the half that matters: a sandbox keeps what it was made
	// with, so a value written here would still be the first task's an hour later.
	if len(provider.Created) == 0 {
		t.Fatal("no sandbox was made, so there is nothing to say about what it was born with")
	}
	for _, born := range provider.Created {
		for _, entry := range born.Env {
			if strings.HasPrefix(entry, telemetry.TraceparentEnv+"=") {
				t.Fatalf("the sandbox was born carrying %q, and it keeps that for every later task", entry)
			}
		}
	}
}

// A task nothing was tracing carries no trace context rather than a header with nothing behind it.
func TestATaskNothingIsTracingCarriesNoTraceContext(t *testing.T) {
	runner := &model.FakeRunner{Reply: "done"}
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	_, project := newProject(t, server)

	if _, err := server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Text: "read the repository",
	}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if carried, held := runner.LastReq.Env[telemetry.TraceparentEnv]; held {
		t.Fatalf("an untraced task was handed %q", carried)
	}
}

// Issue 346: the durable record of what the system did joins to the trace, because weeks later the
// logs are gone and this row is all that is left.
func TestATaskRowCarriesTheTraceOfTheCallThatRanIt(t *testing.T) {
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "done"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	_, project := newProject(t, server)

	const trace = "4bf92f3577b34da6a3ce929d0e0e4736"
	ctx := telemetry.Under(context.Background(), trace, "00f067aa0ba902b7")
	dispatched, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Text: "read the repository",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	tasks, err := server.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: dispatched.GetId()})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks.GetTasks()) == 0 {
		t.Fatal("no task was recorded")
	}
	for _, task := range tasks.GetTasks() {
		if task.GetTraceId() != trace {
			t.Fatalf("the task row traces %q, want the call's own trace", task.GetTraceId())
		}
	}
}

// A task recorded outside any call, which is what a poller starts, leaves the field empty rather
// than inventing a trace nothing can be opened with.
func TestATaskNothingWasTracingLeavesTheTraceEmpty(t *testing.T) {
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "done"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	_, project := newProject(t, server)

	dispatched, err := server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Text: "read the repository",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	tasks, err := server.ListTasks(context.Background(),
		&quaycrewv1.ListTasksRequest{Session: dispatched.GetId()})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	for _, task := range tasks.GetTasks() {
		if task.GetTraceId() != "" {
			t.Fatalf("an untraced task was given the trace %q", task.GetTraceId())
		}
	}
}
