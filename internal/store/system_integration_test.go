//go:build integration

package store_test

import (
	"context"
	"log/slog"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// The control plane over a real database.
//
// The conformance suite proves the store keeps its contract, and the unit tier proves the calls
// refuse what they should. Neither reaches the shapes only Postgres has: a text array, a jsonb
// document, a nullable parent that a foreign key holds, and a transaction that has to carry the row
// and the record of it together. Those either work against the real engine or they do not.

// aSystemOnPostgres stands the control plane up on a real database, empty of rows.
func aSystemOnPostgres(t *testing.T) (*controlplane.Server, store.Store) {
	t.Helper()
	truncate(t)
	kept, err := store.NewPostgres(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(kept.Close)
	return controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: &model.FakeRunner{}, Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	}), kept
}

// aSystemOver is the control plane over a store already opened, for a case that needs to hold the
// store itself as well as the system.
func aSystemOver(t *testing.T, kept store.Store, runner model.Runner) *controlplane.Server {
	t.Helper()
	return controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: runner, Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
}

// openPostgres opens the shared database, empty of rows.
func openPostgres(t *testing.T) store.Store {
	t.Helper()
	truncate(t)
	kept, err := store.NewPostgres(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(kept.Close)
	return kept
}

func aProjectOnPostgres(t *testing.T, s *controlplane.Server) (workspace, project string) {
	t.Helper()
	ctx := context.Background()
	made, err := s.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	inside, err := s.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: made.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return made.GetWorkspace().GetId(), inside.GetProject().GetId()
}

// aFreshSystemSeededFromDisk is the control plane a first run gets: a real database holding nothing,
// and the skills this build ships offered to it the way the image offers them.
//
// It is given no skills directory of its own, deliberately. A system in a container has none, so a
// skill that reached a session only that way would reach nobody in production.
func aFreshSystemSeededFromDisk(t *testing.T) (*controlplane.Server, *sandbox.FakeProvider, string) {
	t.Helper()
	truncate(t)
	kept, err := store.NewPostgres(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(kept.Close)
	dir := t.TempDir()
	boxes := &sandbox.FakeProvider{}
	s := controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: &model.FakeRunner{Reply: "done"}, Provider: boxes,
		Secrets: secrets.NewMemory(), Storage: sandbox.Storage{Dir: dir, Host: dir},
		SandboxImage: "quaycrew-sandbox:test",
	})
	s.Seed(context.Background(), "../../skills", slog.New(slog.DiscardHandler))
	return s, boxes, dir
}

// sandboxFor is the configuration the system asked for on behalf of one session.
func sandboxFor(boxes *sandbox.FakeProvider, session string) (sandbox.Config, bool) {
	for _, made := range boxes.Configurations() {
		if made.ID == session {
			return made, true
		}
	}
	return sandbox.Config{}, false
}
