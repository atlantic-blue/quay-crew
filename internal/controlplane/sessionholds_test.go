package controlplane_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// What a graph's claim about its own job is checked against: the session's working directory as the
// system keeps it, read without starting a container.
func aSystemWithASession(t *testing.T) (*controlplane.Server, sandbox.Storage, string) {
	t.Helper()
	dir := t.TempDir()
	storage := sandbox.Storage{Dir: dir, Host: dir}
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(), Storage: storage,
	})
	ctx := context.Background()
	workspace, err := server.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("create the workspace: %v", err)
	}
	project, err := server.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("create the project: %v", err)
	}
	dispatched, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project.GetProject().GetId(), Text: "hello",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	return server, storage, dispatched.GetId()
}

func TestASessionHoldsWhatTheTaskLeftInIt(t *testing.T) {
	server, storage, session := aSystemWithASession(t)
	ctx := context.Background()

	held, err := server.SessionHolds(ctx, session, "package.json")
	if err != nil {
		t.Fatalf("looking for a file that is not there: %v", err)
	}
	if held {
		t.Fatal("a file nobody wrote was found")
	}

	room, err := server.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: session})
	if err != nil {
		t.Fatalf("read the session: %v", err)
	}
	dir, kept := storage.WorkingDir(sandbox.Config{
		ID: session, Workspace: room.GetSession().GetWorkspace(), Project: room.GetSession().GetProject(),
	})
	if !kept {
		t.Fatal("the system keeps no working directory for a session it just ran a task in")
	}
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatalf("make the working directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}

	held, err = server.SessionHolds(ctx, session, "package.json")
	if err != nil {
		t.Fatalf("looking for a file that is there: %v", err)
	}
	if !held {
		t.Fatal("a file in the session's own working directory was not found")
	}
}

// A question that cannot be answered is an error rather than a false, because the run stops on an
// error and carries on past a false. A check that quietly passes when nothing could look is the same
// false green as no check at all.
func TestASessionNobodyHasCannotSatisfyACheck(t *testing.T) {
	server, _, _ := aSystemWithASession(t)

	if _, err := server.SessionHolds(context.Background(), "no-such-session", "package.json"); err == nil {
		t.Fatal("a session the system does not have answered a question about its files")
	}
}

func TestASystemKeepingNothingOnDiskSaysSoRatherThanAnsweringNo(t *testing.T) {
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	ctx := context.Background()
	workspace, err := server.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("create the workspace: %v", err)
	}
	project, err := server.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("create the project: %v", err)
	}
	dispatched, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project.GetProject().GetId(), Text: "hello",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	_, err = server.SessionHolds(ctx, dispatched.GetId(), "package.json")
	if err == nil {
		t.Fatal("a system with nowhere to look answered a question about a session's files")
	}
	if !strings.Contains(err.Error(), "working directory") {
		t.Errorf("it says %q, want it to say there is nowhere to look", err)
	}
}
