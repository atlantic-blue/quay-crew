package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/role"
)

// shippedRoles is where the roles this build ships live, from this package's directory.
const shippedRoles = "../../roles"

// filesOf reads a role off disk into the shape the wire carries, which is what the command line does
// before it sends one.
func filesOf(t *testing.T, dir string) []*quaycrewv1.RoleFile {
	t.Helper()
	read, err := role.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	files := make([]*quaycrewv1.RoleFile, 0, len(read))
	for _, file := range read {
		files = append(files, &quaycrewv1.RoleFile{Path: file.Path, Body: file.Body})
	}
	return files
}

// The check run against the real store and the real control plane rather than against the reader
// alone. A role that loads in this process and is refused on the other side of ImportRole does not
// ship, and this is the only place that difference shows.
func TestEveryShippedRoleImportsIntoTheCrew(t *testing.T) {
	roles, err := role.All(shippedRoles)
	if err != nil {
		t.Fatalf("loading the roles this build ships: %v", err)
	}
	s := newServer(&model.FakeRunner{})
	ctx := context.Background()

	for _, one := range roles {
		resp, err := s.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{Files: filesOf(t, one.Dir)})
		if err != nil {
			t.Errorf("the crew refused the %s role: %v", one.Name, err)
			continue
		}
		imported := resp.GetRole()
		if imported.GetName() != one.Name || imported.GetVersion() != int32(one.Version) {
			t.Errorf("imported %s v%d and sent %s v%d",
				imported.GetName(), imported.GetVersion(), one.Name, one.Version)
		}
		if imported.GetModel() != one.Model {
			t.Errorf("%s came back running on %q and was sent %q", one.Name, imported.GetModel(), one.Model)
		}
		if strings.Join(imported.GetReceives(), ",") != strings.Join(one.Receives, ",") {
			t.Errorf("%s came back receiving %v and was sent %v", one.Name, imported.GetReceives(), one.Receives)
		}
	}

	held, err := s.ListRoles(ctx, &quaycrewv1.ListRolesRequest{})
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	// The count is asserted against what was read off disk, so a run that imported none would fail
	// here rather than pass a loop that ran zero times.
	if len(held.GetRoles()) != len(roles) {
		t.Fatalf("the crew holds %d roles and roles/ ships %d", len(held.GetRoles()), len(roles))
	}
	t.Logf("the crew holds %d shipped roles", len(held.GetRoles()))
}

// Importing is half of it. A role a workspace cannot be given is a row in the crew and nothing else,
// so this carries one through to the state the operator is left in.
func TestAShippedRoleAttachesToAWorkspaceAndIsHeldThere(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	ctx := context.Background()
	workspace, _ := newProject(t, s)

	if _, err := s.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
		Files: filesOf(t, shippedRoles+"/test-writer"),
	}); err != nil {
		t.Fatalf("importing the test-writer role: %v", err)
	}
	if _, err := s.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
		Workspace: workspace, Name: "test-writer",
	}); err != nil {
		t.Fatalf("attaching the test-writer role: %v", err)
	}

	held, err := s.ListRoles(ctx, &quaycrewv1.ListRolesRequest{Workspace: workspace})
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(held.GetRoles()) != 1 || held.GetRoles()[0].GetName() != "test-writer" {
		t.Fatalf("the workspace does not hold the test-writer role: %+v", held.GetRoles())
	}
	if held.GetRoles()[0].GetModel() != "sonnet" {
		t.Errorf("the workspace holds it running on %q and it ships on sonnet", held.GetRoles()[0].GetModel())
	}
}

// The sad path an edit to a shipped role is most likely to reach: a manifest naming material or a
// verb the crew does not hand out. It is refused by name on the far side too, not only by the reader.
func TestARoleCarryingAWordTheCrewDoesNotKnowIsRefusedOnImport(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	ctx := context.Background()
	files := filesOf(t, shippedRoles+"/test-writer")
	for _, file := range files {
		if file.GetPath() == role.ManifestFile {
			file.Body = []byte(strings.Replace(string(file.GetBody()),
				"  - skills", "  - the whole repository", 1))
		}
	}

	_, err := s.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{Files: files})
	if err == nil {
		t.Fatal("the crew accepted a role receiving material it does not hand out")
	}
	if !strings.Contains(err.Error(), "the whole repository") {
		t.Errorf("the refusal does not name what was wrong: %v", err)
	}
	held, _ := s.ListRoles(ctx, &quaycrewv1.ListRolesRequest{})
	if len(held.GetRoles()) != 0 {
		t.Errorf("a refused role is on the record anyway: %+v", held.GetRoles())
	}
}
