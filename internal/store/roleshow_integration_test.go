//go:build integration

package store_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/role"
)

// Reading a role back out of the crew, against the real database.
//
// The unit tier proves the rendering and the refusal against a store in memory. What only this tier
// reaches is the crossing that the command exists for: a brief goes into a column as one process
// writes it and comes out as another reads it, and the answer an operator audits a run against is
// whatever came out of that column. A truncation, an encoding or a column that silently holds the
// first few thousand bytes shows here and nowhere else, and each of them would make this command
// agree with a role the crew does not hold.
//
// What a role may call is compared against the file too, and it is the reason this file exists in
// the shape it does. The roles table carried no may column until quay-crew#459, so the verbs a
// manifest declared were dropped on the way into the database and every role read back allowed to
// call nothing, while the in memory store kept the whole struct and agreed with itself. Reading a
// role back is what surfaced it, so the assertion stays here: the next column to go missing fails a
// test rather than a run.

// TestEveryShippedBriefComesBackByteForByteThroughGetRole imports the roles this build ships and
// reads each one back, comparing against the file on disk rather than against the import's own
// answer, which could hand back what it was sent while the database held none of it.
func TestEveryShippedBriefComesBackByteForByteThroughGetRole(t *testing.T) {
	onDisk, err := role.All(shippedRoles)
	if err != nil {
		t.Fatalf("loading the roles this build ships: %v", err)
	}
	if len(onDisk) == 0 {
		t.Fatal("roles/ holds none, so this test would sweep nothing and report a pass")
	}
	s, _ := aCrewWithRoles(t, &model.FakeRunner{})
	ctx := context.Background()

	read := 0
	for _, one := range onDisk {
		if _, err := s.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{Files: filesOfRole(t, one.Dir)}); err != nil {
			t.Fatalf("the crew refused the %s role, which ships with it: %v", one.Name, err)
		}
		got, err := s.GetRole(ctx, &quaycrewv1.GetRoleRequest{Name: one.Name})
		if err != nil {
			t.Fatalf("GetRole %s: %v", one.Name, err)
		}
		if got.GetBrief() != one.Brief {
			t.Errorf("the %s brief came back %d bytes and its file is %d",
				one.Name, len(got.GetBrief()), len(one.Brief))
			continue
		}
		// Held to the file first and to a length after, so a column that came back empty cannot
		// satisfy this by matching an equally empty expectation.
		if len(got.GetBrief()) == 0 {
			t.Errorf("the %s brief came back empty, and a brief is the whole instruction", one.Name)
			continue
		}
		if got.GetRole().GetVersion() != int32(one.Version) || got.GetRole().GetModel() != one.Model {
			t.Errorf("%s came back as version %d on %s, and its file says version %d on %s",
				one.Name, got.GetRole().GetVersion(), got.GetRole().GetModel(), one.Version, one.Model)
		}
		if strings.Join(got.GetRole().GetReceives(), ",") != strings.Join(one.Receives, ",") {
			t.Errorf("%s came back receiving %v and its file says %v",
				one.Name, got.GetRole().GetReceives(), one.Receives)
		}
		if strings.Join(got.GetVerbs(), ",") != strings.Join(one.Verbs, ",") {
			t.Errorf("%s came back allowed to call %v and its file says %v",
				one.Name, got.GetVerbs(), one.Verbs)
		}
		read++
	}
	if read != len(onDisk) {
		t.Fatalf("%d briefs came back whole and roles/ ships %d", read, len(onDisk))
	}
	t.Logf("read %d briefs back through GetRole", read)
}

// A workspace pins the version it attached, so reading at that address has to give the pinned
// version even after a newer one is imported. Getting this wrong is the failure the command exists
// to end: an operator diffing a brief against a run that was never given it.
func TestAWorkspaceReadsThePinnedBriefAfterANewerOneIsImported(t *testing.T) {
	s, _ := aCrewWithRoles(t, &model.FakeRunner{})
	ctx := context.Background()
	workspace, _ := aProjectOnPostgres(t, s)

	importBriefOnPostgres(t, s, "test-writer", 1, "Version one says this.")
	if _, err := s.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
		Workspace: workspace, Name: "test-writer",
	}); err != nil {
		t.Fatalf("AttachRole: %v", err)
	}
	importBriefOnPostgres(t, s, "test-writer", 2, "Version two says something else.")

	pinned, err := s.GetRole(ctx, &quaycrewv1.GetRoleRequest{Workspace: workspace, Name: "test-writer"})
	if err != nil {
		t.Fatalf("GetRole at the workspace: %v", err)
	}
	if pinned.GetBrief() != "Version one says this." {
		t.Errorf("the workspace read %q, and it pinned version 1", pinned.GetBrief())
	}
	if names := pinned.GetHeldBy(); len(names) != 1 || names[0] != "acme" {
		t.Errorf("it came back held by %v, and acme attached it", names)
	}

	newest, err := s.GetRole(ctx, &quaycrewv1.GetRoleRequest{Name: "test-writer"})
	if err != nil {
		t.Fatalf("GetRole at the crew: %v", err)
	}
	if newest.GetBrief() != "Version two says something else." {
		t.Errorf("the crew read %q, and it holds version 2", newest.GetBrief())
	}
}

// A name nothing holds is refused with the names that are there, off the real database, so the
// refusal cannot be a list assembled from something the process happened to still be holding.
func TestANameTheDatabaseDoesNotHoldIsRefusedWithTheOnesItDoes(t *testing.T) {
	s, _ := aCrewWithRoles(t, &model.FakeRunner{})
	ctx := context.Background()

	empty, err := s.GetRole(ctx, &quaycrewv1.GetRoleRequest{Name: "orchestrator"})
	if err == nil {
		t.Fatalf("a crew holding no roles answered with %+v", empty)
	}
	if !strings.Contains(err.Error(), "holds no roles at all") {
		t.Errorf("the refusal does not say the crew holds nothing: %v", err)
	}

	importBriefOnPostgres(t, s, "test-writer", 1, "Write the tests.")
	if _, err := s.GetRole(ctx, &quaycrewv1.GetRoleRequest{Name: "test-writter"}); err == nil {
		t.Error("a name the crew does not hold was answered")
	} else if !strings.Contains(err.Error(), "test-writer") {
		t.Errorf("the refusal does not name the role that is there: %v", err)
	}
}

// filesOfRole reads a role off disk into the shape the wire carries, which is what the command line
// does before it sends one.
func filesOfRole(t *testing.T, dir string) []*quaycrewv1.RoleFile {
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

// importBriefOnPostgres imports one version of a role carrying the brief it is given, which is the
// text these tests then look for on the way back out.
func importBriefOnPostgres(t *testing.T, s *controlplane.Server, name string, version int, brief string) {
	t.Helper()
	manifest := "name: " + name + "\nversion: " + strconv.Itoa(version) +
		"\nsummary: writes the tests for a job, from the job alone\nmodel: opus\nreceives:\n  - job\n  - context\n"
	if _, err := s.ImportRole(context.Background(), &quaycrewv1.ImportRoleRequest{
		Files: []*quaycrewv1.RoleFile{
			{Path: role.ManifestFile, Body: []byte(manifest)},
			{Path: role.BriefFile, Body: []byte(brief)},
		},
	}); err != nil {
		t.Fatalf("ImportRole %s version %d: %v", name, version, err)
	}
}
