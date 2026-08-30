package controlplane_test

import (
	"context"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// What a session may do when it is born was a constant in this file. Every session that did not come
// through the console's wizard arrived in acceptEdits, and the only way to change that was to edit
// the binary. It comes from the system's configuration now.

// aSystemBornIn is a system whose configuration says what a session may do, and the runner it dispatches
// to, because what the store recorded is not the question: what the model was run with is.
func aSystemBornIn(t *testing.T, mode string) (quaycrewv1.ControlPlaneServiceServer, *model.FakeRunner) {
	t.Helper()
	runner := &model.FakeRunner{Reply: "ok"}
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		BirthPermissionMode: mode,
	})
	return server, runner
}

// dispatched starts a session the ordinary way, which is the path the wizard does not touch.
func dispatched(t *testing.T, server quaycrewv1.ControlPlaneServiceServer) {
	t.Helper()
	ctx := context.Background()
	workspace, err := server.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "me"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	project, err := server.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project.GetProject().GetId(), Text: "hello",
	}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
}

// The one that matters. A system that recorded the mode on the session and ran the task in something
// else would pass any assertion made against the listing, and the operator would find out when the
// model asked for an approval nobody was there to give.
func TestATaskRunsInTheModeTheSystemIsConfiguredToBornSessionsIn(t *testing.T) {
	for _, mode := range []string{model.PermissionPlan, model.PermissionBypass, model.PermissionAcceptEdits} {
		t.Run(mode, func(t *testing.T) {
			server, runner := aSystemBornIn(t, mode)

			dispatched(t, server)

			if was := runner.LastReq.PermissionMode; was != mode {
				t.Fatalf("the first task ran in %q, want %q, which is what the system is configured for", was, mode)
			}
		})
	}
}

// A system that says nothing keeps what every system has had until now, because an upgrade that quietly
// widens what a session may do is the worst way to find out this setting exists.
func TestASystemThatSaysNothingKeepsTheModeItAlwaysHad(t *testing.T) {
	server, runner := aSystemBornIn(t, "")

	dispatched(t, server)

	if was := runner.LastReq.PermissionMode; was != model.PermissionAcceptEdits {
		t.Fatalf("a system with nothing configured ran its first task in %q, want %q", was, model.PermissionAcceptEdits)
	}
}
