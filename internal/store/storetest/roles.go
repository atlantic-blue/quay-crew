package storetest

import (
	"context"
	"errors"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/origin"
	"github.com/atlantic-blue/quay-crew/internal/role"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// The role half of the contract, held against both implementations.
//
// A double whose behaviour is looser than the real store manufactures a green suite over a broken
// system, and what a role declares is a boundary: a role that does not survive the round trip is a
// session receiving material nobody meant it to have.
func runRoleConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a role is imported and comes back whole", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		if err := s.ImportRole(ctx, aRole("test-writer", 1)); err != nil {
			t.Fatalf("ImportRole: %v", err)
		}
		got, err := s.GetRole(ctx, "test-writer", 1)
		if err != nil {
			t.Fatalf("GetRole: %v", err)
		}
		if got.Name != "test-writer" || got.Version != 1 || got.Summary == "" {
			t.Fatalf("bad role: %+v", got.Role)
		}
		if got.ImportedAt.IsZero() {
			t.Error("the role came back with no import time, so a listing cannot say when it arrived")
		}
		if got.Model != "opus" {
			t.Errorf("the declared model did not survive: %q", got.Model)
		}
		if got.Brief == "" {
			t.Error("the brief did not survive, and the brief is the whole instruction")
		}
		// What it receives is the boundary. A role whose receives list is dropped somewhere in the
		// round trip is a role the system would give everything to.
		if len(got.Receives) != 2 || !got.Gets(role.MaterialJob) || !got.Gets(role.MaterialContext) {
			t.Errorf("what it receives did not survive: %+v", got.Receives)
		}
		if got.Gets(role.MaterialSkills) {
			t.Errorf("it came back receiving skills, which it never declared: %+v", got.Receives)
		}
	})

	// What a role may call, read back out. It is a separate case from the one above because aRole
	// grants nothing, so a store that never wrote the column would agree with it in every run.
	//
	// This is the whole of a role's grant. The credential a job runs under is minted from the role
	// as the store gives it back, so a verb dropped here is a session refused at its first call, and
	// the refusal reads as an authentication failure rather than as a missing column.
	t.Run("what a role may call survives the round trip", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		if err := s.ImportRole(ctx, aRoleThatMay("orchestrator", 1,
			role.VerbJobCreate, role.VerbJobRead, role.VerbJobStop)); err != nil {
			t.Fatalf("ImportRole: %v", err)
		}
		got, err := s.GetRole(ctx, "orchestrator", 1)
		if err != nil {
			t.Fatalf("GetRole: %v", err)
		}
		for _, verb := range []string{role.VerbJobCreate, role.VerbJobRead, role.VerbJobStop} {
			if !got.May(verb) {
				t.Errorf("it came back unable to %s, and it was imported declaring it: %+v", verb, got.Verbs)
			}
		}
		// The other direction, so a store answering "every verb" passes nothing.
		if got.May(role.VerbJobAnswer) {
			t.Errorf("it came back able to %s, which it never declared: %+v", role.VerbJobAnswer, got.Verbs)
		}

		// Every read a credential could be minted from, because a job's role is looked up through
		// the workspace rather than through GetRole.
		workspace, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		if _, err := s.AttachRole(ctx, workspace.GetId(), "orchestrator"); err != nil {
			t.Fatalf("AttachRole: %v", err)
		}
		if _, err := s.AttachSystemRole(ctx, "orchestrator"); err != nil {
			t.Fatalf("AttachSystemRole: %v", err)
		}
		listed, err := s.ListRoles(ctx)
		if err != nil {
			t.Fatalf("ListRoles: %v", err)
		}
		held, err := s.WorkspaceRoles(ctx, workspace.GetId())
		if err != nil {
			t.Fatalf("WorkspaceRoles: %v", err)
		}
		system, err := s.SystemRoles(ctx)
		if err != nil {
			t.Fatalf("SystemRoles: %v", err)
		}
		for what, from := range map[string][]store.ImportedRole{
			"the listing": listed, "the workspace": held, "the system": system,
		} {
			if len(from) != 1 {
				t.Fatalf("%s holds %d roles, want the one that was imported", what, len(from))
			}
			if !from[0].May(role.VerbJobCreate) {
				t.Errorf("%s gives back a role that may %v, and it declared %s",
					what, from[0].Verbs, role.VerbJobCreate)
			}
		}
	})

	// Where a role came from, read back out of every listing a person or a credential reads.
	//
	// It is the answer to "can anybody but the importer read this role", and a column that goes
	// missing takes the answer with it: a role imported from a repository comes back looking exactly
	// like one somebody kept in a folder on their laptop, which is the failure this recorded in the
	// first place. The `may` column was dropped in exactly this shape and only Postgres knew.
	t.Run("where a role came from survives the round trip", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		if err := s.ImportRole(ctx, aRoleFrom("test-writer", 1, origin.Origin{
			Repository: "github.com/atlantic-blue/quay-crew",
			Commit:     "0f1e2d3c4b5a69788796a5b4c3d2e1f001234567",
			Path:       "roles/test-writer",
		})); err != nil {
			t.Fatalf("ImportRole: %v", err)
		}
		got, err := s.GetRole(ctx, "test-writer", 1)
		if err != nil {
			t.Fatalf("GetRole: %v", err)
		}
		if got.Origin.Repository != "github.com/atlantic-blue/quay-crew" ||
			got.Origin.Commit != "0f1e2d3c4b5a69788796a5b4c3d2e1f001234567" ||
			got.Origin.Path != "roles/test-writer" {
			t.Errorf("it came back from %+v, and it was imported from the repository", got.Origin)
		}
		if !got.Origin.Reviewable() {
			t.Errorf("a role imported from a pushed commit came back unreviewable: %s", got.Origin.Line())
		}

		// Every read the console and the command line print from, because a listing is where an
		// operator would see this and GetRole is not.
		workspace, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		if _, err := s.AttachRole(ctx, workspace.GetId(), "test-writer"); err != nil {
			t.Fatalf("AttachRole: %v", err)
		}
		if _, err := s.AttachSystemRole(ctx, "test-writer"); err != nil {
			t.Fatalf("AttachSystemRole: %v", err)
		}
		listed, err := s.ListRoles(ctx)
		if err != nil {
			t.Fatalf("ListRoles: %v", err)
		}
		held, err := s.WorkspaceRoles(ctx, workspace.GetId())
		if err != nil {
			t.Fatalf("WorkspaceRoles: %v", err)
		}
		system, err := s.SystemRoles(ctx)
		if err != nil {
			t.Fatalf("SystemRoles: %v", err)
		}
		for what, from := range map[string][]store.ImportedRole{
			"the listing": listed, "the workspace": held, "the system": system,
		} {
			if len(from) != 1 {
				t.Fatalf("%s holds %d roles, want the one that was imported", what, len(from))
			}
			if from[0].Origin.Repository != "github.com/atlantic-blue/quay-crew" {
				t.Errorf("%s says the role came from %q, and it came from the repository",
					what, from[0].Origin.Repository)
			}
		}
	})

	// A role kept in a folder is imported all the same, and the store keeps what it was told rather
	// than an empty origin that would read as a role imported before any of this was recorded.
	t.Run("a role from a loose directory keeps the path it came from", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		if err := s.ImportRole(ctx, aRoleFrom("orchestrator", 1, origin.Origin{
			Path: "/home/someone/roles/orchestrator",
		})); err != nil {
			t.Fatalf("ImportRole: %v", err)
		}
		got, err := s.GetRole(ctx, "orchestrator", 1)
		if err != nil {
			t.Fatalf("GetRole: %v", err)
		}
		if got.Origin.Path != "/home/someone/roles/orchestrator" {
			t.Errorf("it came back from %q, and it was read from a folder", got.Origin.Path)
		}
		if got.Origin.Reviewable() {
			t.Errorf("a role from a folder came back reviewable: %s", got.Origin.Line())
		}
	})

	// The same bytes imported again from somewhere else. The import is the system's only sight of a
	// role, so it says where the role was last seen: committing a loose role and importing it again
	// is how an operator clears the warning, and a store that kept the first answer would leave them
	// fixing it and watching nothing change.
	t.Run("importing the same role again records where it was read this time", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		if err := s.ImportRole(ctx, aRoleFrom("orchestrator", 1, origin.Origin{
			Path: "/home/someone/roles/orchestrator", Dirty: true, Unpushed: true,
		})); err != nil {
			t.Fatalf("ImportRole: %v", err)
		}
		if err := s.ImportRole(ctx, aRoleFrom("orchestrator", 1, origin.Origin{
			Repository: "github.com/atlantic-blue/quay-crew",
			Commit:     "0f1e2d3c4b5a69788796a5b4c3d2e1f001234567",
			Path:       "roles/orchestrator",
		})); err != nil {
			t.Fatalf("importing the same role from the repository: %v", err)
		}
		got, err := s.GetRole(ctx, "orchestrator", 1)
		if err != nil {
			t.Fatalf("GetRole: %v", err)
		}
		if got.Origin.Repository != "github.com/atlantic-blue/quay-crew" || got.Origin.Dirty || got.Origin.Unpushed {
			t.Errorf("it still says %s, and it was imported again from the repository", got.Origin.Line())
		}
	})

	t.Run("importing the same version twice is harmless, and importing a different role as it is refused", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		if err := s.ImportRole(ctx, aRole("test-writer", 1)); err != nil {
			t.Fatalf("ImportRole: %v", err)
		}
		if err := s.ImportRole(ctx, aRole("test-writer", 1)); err != nil {
			t.Errorf("importing the same role again was refused: %v", err)
		}

		changed := aRole("test-writer", 1)
		changed.Brief = "write the code instead"
		if err := s.ImportRole(ctx, changed); !errors.Is(err, store.ErrRoleChanged) {
			t.Errorf("importing a different role at the same version returned %v, want ErrRoleChanged", err)
		}
		// The refusal leaves what was there, because a workspace pins the version it holds.
		if got, err := s.GetRole(ctx, "test-writer", 1); err != nil || got.Brief == "write the code instead" {
			t.Errorf("the refused import overwrote the stored role: %+v (%v)", got.Role, err)
		}
	})

	t.Run("a listing gives the newest revision of each role", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		for _, one := range []store.ImportedRole{
			aRole("test-writer", 1), aRole("test-writer", 2), aRole("architect", 1),
		} {
			if err := s.ImportRole(ctx, one); err != nil {
				t.Fatalf("ImportRole %s v%d: %v", one.Name, one.Version, err)
			}
		}

		listed, err := s.ListRoles(ctx)
		if err != nil {
			t.Fatalf("ListRoles: %v", err)
		}
		if len(listed) != 2 {
			t.Fatalf("the listing holds %d roles, want 2: %+v", len(listed), listed)
		}
		// Sorted by name, so a listing does not shuffle between reads.
		if listed[0].Name != "architect" || listed[1].Name != "test-writer" {
			t.Fatalf("the listing came back as %q then %q", listed[0].Name, listed[1].Name)
		}
		if listed[1].Version != 2 {
			t.Errorf("the listing gives version %d of test-writer, want the newest", listed[1].Version)
		}
	})

	t.Run("a workspace holds the roles attached to it, pinned to a version", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}

		if held, err := s.WorkspaceRoles(ctx, workspace.GetId()); err != nil || len(held) != 0 {
			t.Fatalf("a fresh workspace holds %d roles (%v), want none", len(held), err)
		}
		if err := s.ImportRole(ctx, aRole("test-writer", 1)); err != nil {
			t.Fatalf("ImportRole: %v", err)
		}
		if _, err := s.AttachRole(ctx, workspace.GetId(), "test-writer"); err != nil {
			t.Fatalf("AttachRole: %v", err)
		}

		held, err := s.WorkspaceRoles(ctx, workspace.GetId())
		if err != nil {
			t.Fatalf("WorkspaceRoles: %v", err)
		}
		if len(held) != 1 || held[0].Name != "test-writer" || held[0].Version != 1 {
			t.Fatalf("the workspace holds %+v, want test-writer version 1", held)
		}

		// A newer revision does not move the workspace on its own. Pinning is the whole reason a
		// role can be edited without changing a session already running as it.
		if err := s.ImportRole(ctx, aRole("test-writer", 2)); err != nil {
			t.Fatalf("ImportRole v2: %v", err)
		}
		if held, err := s.WorkspaceRoles(ctx, workspace.GetId()); err != nil || held[0].Version != 1 {
			t.Fatalf("the workspace moved to %+v on its own (%v), want it pinned at version 1", held, err)
		}
		if _, err := s.AttachRole(ctx, workspace.GetId(), "test-writer"); err != nil {
			t.Fatalf("AttachRole again: %v", err)
		}
		if held, err := s.WorkspaceRoles(ctx, workspace.GetId()); err != nil || held[0].Version != 2 {
			t.Fatalf("re-attaching left the workspace at %+v (%v), want version 2", held, err)
		}

		if err := s.DetachRole(ctx, workspace.GetId(), "test-writer"); err != nil {
			t.Fatalf("DetachRole: %v", err)
		}
		if held, err := s.WorkspaceRoles(ctx, workspace.GetId()); err != nil || len(held) != 0 {
			t.Fatalf("the workspace still holds %d roles (%v) after detaching", len(held), err)
		}
		// Detaching leaves the role imported, because another workspace may hold it and because
		// importing it again should not be the price of a change of mind.
		if _, err := s.GetRole(ctx, "test-writer", 2); err != nil {
			t.Errorf("detaching removed the role from the catalogue: %v", err)
		}
	})

	t.Run("one workspace's roles are not another's", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		mine, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		theirs, err := s.CreateWorkspace(ctx, "widgets")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		if err := s.ImportRole(ctx, aRole("test-writer", 1)); err != nil {
			t.Fatalf("ImportRole: %v", err)
		}
		if _, err := s.AttachRole(ctx, mine.GetId(), "test-writer"); err != nil {
			t.Fatalf("AttachRole: %v", err)
		}
		if held, err := s.WorkspaceRoles(ctx, theirs.GetId()); err != nil || len(held) != 0 {
			t.Fatalf("the other workspace holds %d roles (%v), want none", len(held), err)
		}
	})

	t.Run("attaching a role that does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		if _, err := s.AttachRole(ctx, workspace.GetId(), "architect"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("attaching a role the system has not imported returned %v, want ErrNotFound", err)
		}
		if err := s.DetachRole(ctx, workspace.GetId(), "architect"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("detaching a role the workspace does not hold returned %v, want ErrNotFound", err)
		}
		if err := s.ImportRole(ctx, aRole("architect", 1)); err != nil {
			t.Fatalf("ImportRole: %v", err)
		}
		if _, err := s.AttachRole(ctx, "no-such-workspace", "architect"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("attaching to a workspace that does not exist returned %v, want ErrNotFound", err)
		}
	})

	t.Run("the system holds a role for every workspace, pinned to a version", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		if held, err := s.SystemRoles(ctx); err != nil || len(held) != 0 {
			t.Fatalf("a fresh system holds %d roles (%v), want none", len(held), err)
		}
		if err := s.ImportRole(ctx, aRole("test-writer", 1)); err != nil {
			t.Fatalf("ImportRole: %v", err)
		}
		if _, err := s.AttachSystemRole(ctx, "test-writer"); err != nil {
			t.Fatalf("AttachSystemRole: %v", err)
		}

		held, err := s.SystemRoles(ctx)
		if err != nil {
			t.Fatalf("SystemRoles: %v", err)
		}
		if len(held) != 1 || held[0].Name != "test-writer" || held[0].Brief == "" {
			t.Fatalf("the system holds %+v, want the test-writer role with its brief", held)
		}

		if err := s.ImportRole(ctx, aRole("test-writer", 2)); err != nil {
			t.Fatalf("ImportRole v2: %v", err)
		}
		if held, err := s.SystemRoles(ctx); err != nil || held[0].Version != 1 {
			t.Fatalf("the system moved to %+v on its own (%v), want it pinned at version 1", held, err)
		}
		if _, err := s.AttachSystemRole(ctx, "test-writer"); err != nil {
			t.Fatalf("AttachSystemRole again: %v", err)
		}
		if held, err := s.SystemRoles(ctx); err != nil || held[0].Version != 2 {
			t.Fatalf("re-attaching left the system at %+v (%v), want version 2", held, err)
		}

		if err := s.DetachSystemRole(ctx, "test-writer"); err != nil {
			t.Fatalf("DetachSystemRole: %v", err)
		}
		if held, err := s.SystemRoles(ctx); err != nil || len(held) != 0 {
			t.Fatalf("the system still holds %d roles (%v) after detaching", len(held), err)
		}
		if _, err := s.GetRole(ctx, "test-writer", 2); err != nil {
			t.Errorf("detaching from the system removed the role from the catalogue: %v", err)
		}
	})

	t.Run("the system's holding of a role and a workspace's are separate statements", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		if err := s.ImportRole(ctx, aRole("test-writer", 1)); err != nil {
			t.Fatalf("ImportRole: %v", err)
		}
		if _, err := s.AttachSystemRole(ctx, "test-writer"); err != nil {
			t.Fatalf("AttachSystemRole: %v", err)
		}
		if _, err := s.AttachRole(ctx, workspace.GetId(), "test-writer"); err != nil {
			t.Fatalf("AttachRole: %v", err)
		}

		if err := s.DetachSystemRole(ctx, "test-writer"); err != nil {
			t.Fatalf("DetachSystemRole: %v", err)
		}
		if held, err := s.WorkspaceRoles(ctx, workspace.GetId()); err != nil || len(held) != 1 {
			t.Fatalf("the workspace holds %d roles (%v) after the system let go, want 1", len(held), err)
		}
	})

	t.Run("attaching to the system a role that does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		if _, err := s.AttachSystemRole(ctx, "architect"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("attaching a role the system has not imported returned %v, want ErrNotFound", err)
		}
		if err := s.DetachSystemRole(ctx, "architect"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("detaching a role the system does not hold returned %v, want ErrNotFound", err)
		}
	})
}

// aRole is a role to put in the store, whole enough that the round trip is worth asserting on: a
// model, a brief, and a boundary that leaves one kind of material out.
func aRole(name string, version int) store.ImportedRole {
	return store.ImportedRole{Role: role.Role{
		Name:     name,
		Version:  version,
		Summary:  "Writes the tests for a job, from the job alone.",
		Model:    "opus",
		Receives: []string{role.MaterialContext, role.MaterialJob},
		Brief:    "Write the tests. Do not write the code.",
	}}
}

// aRoleThatMay is a role with a verb list, so the round trip is asserted on a role that grants
// something. aRole grants nothing, and a store that dropped the column would agree with it forever.
func aRoleThatMay(name string, version int, verbs ...string) store.ImportedRole {
	one := aRole(name, version)
	one.Verbs = verbs
	return one
}

// aRoleFrom is a role that came from somewhere, so the round trip is asserted on an origin with
// something in it. aRole records none, and a store that dropped the columns would agree with it.
func aRoleFrom(name string, version int, from origin.Origin) store.ImportedRole {
	one := aRole(name, version)
	one.Origin = from
	return one
}
