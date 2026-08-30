//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/auth"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/role"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The ceiling against a real database.
//
// The default is the one that has to hold here: a workspace with no row must read as depth zero
// rather than as a row that is missing. A store that answered "not found" would make a crew that
// grants everything until somebody configures it, which is the wrong way round to fail.

// aRoleThatMay imports and attaches a role declaring the verbs named.
func aRoleThatMay(t *testing.T, s *controlplane.Server, workspace, name string, verbs ...string) {
	t.Helper()
	manifest := "name: " + name + "\nversion: 1\nsummary: clears the backlog\nmodel: opus\nreceives:\n  - job\n"
	if len(verbs) > 0 {
		manifest += "verbs:\n"
		for _, verb := range verbs {
			manifest += "  - " + verb + "\n"
		}
	}
	ctx := context.Background()
	if _, err := s.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
		Files: []*quaycrewv1.RoleFile{
			{Path: role.ManifestFile, Body: []byte(manifest)},
			{Path: role.BriefFile, Body: []byte("Read the open pull requests.")},
		},
	}); err != nil {
		t.Fatalf("ImportRole: %v", err)
	}
	if _, err := s.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{Workspace: workspace, Name: name}); err != nil {
		t.Fatalf("AttachRole: %v", err)
	}
}

func TestAWorkspaceWithNoRowAllowsNoDepthInPostgres(t *testing.T) {
	kept := openPostgres(t)
	s := aCrewNamed(t, kept, "controller-a", 0, &model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)

	held, err := s.GetWorkspaceLimits(ctx, &quaycrewv1.GetWorkspaceLimitsRequest{Workspace: workspace})
	if err != nil {
		t.Fatalf("GetWorkspaceLimits: %v", err)
	}
	if held.GetLimits().GetMaxDepth() != 0 {
		t.Fatalf("a workspace with no row allows depth %d, want 0", held.GetLimits().GetMaxDepth())
	}

	// And a session in it declares nothing, which is what that default is for.
	root, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "clear the backlog", Brief: "read the pull requests",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	_, err = s.CreateJob(auth.WithGrant(ctx, auth.Grant{
		Job: root.GetJob().GetId(), Verbs: []string{role.VerbJobCreate},
	}), &quaycrewv1.CreateJobRequest{Project: project, Title: "pull request 341", Brief: "review it"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a session in an unconfigured workspace answered %v, want PermissionDenied", status.Code(err))
	}
}

// The whole path, against the database that holds it: an operator raises the ceiling, a session
// declares job under its own, and the row carries the parent and the depth the crew assigned.
func TestASessionDeclaresJobWithinTheCeilingInPostgres(t *testing.T) {
	kept := openPostgres(t)
	s := aCrewNamed(t, kept, "controller-a", 0, &model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	aRoleThatMay(t, s, workspace, "backlog-clearer", role.VerbJobCreate)

	if _, err := s.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{Workspace: workspace, MaxDepth: 1, MaxRunning: 4, LeaseSeconds: 90},
	}); err != nil {
		t.Fatalf("SetWorkspaceLimits: %v", err)
	}
	root, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "clear the backlog", Brief: "read the pull requests", Role: "backlog-clearer",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	child, err := s.CreateJob(auth.WithGrant(ctx, auth.Grant{
		Job: root.GetJob().GetId(), Verbs: []string{role.VerbJobCreate},
	}), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "pull request 341", Brief: "review it", Role: "backlog-clearer",
	})
	if err != nil {
		t.Fatalf("CreateJob as the session: %v", err)
	}
	if child.GetJob().GetParent() != root.GetJob().GetId() || child.GetJob().GetDepth() != 1 {
		t.Fatalf("the child is at depth %d under %q", child.GetJob().GetDepth(), child.GetJob().GetParent())
	}

	// Read back off the database rather than from the answer, because the parent is a foreign key and
	// the depth is a column.
	found, err := kept.GetJob(ctx, child.GetJob().GetId())
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if found.Parent != root.GetJob().GetId() || found.Depth != 1 {
		t.Fatalf("the row says depth %d under %q", found.Depth, found.Parent)
	}

	// One deeper is refused, and the refusal names the limit rather than the role.
	_, err = s.CreateJob(auth.WithGrant(ctx, auth.Grant{
		Job: child.GetJob().GetId(), Verbs: []string{role.VerbJobCreate},
	}), &quaycrewv1.CreateJobRequest{Project: project, Title: "write a test", Brief: "write it"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("job below the ceiling answered %v, want PermissionDenied", status.Code(err))
	}
}

// The ceiling survives the process that set it, which is the whole reason it is a row.
func TestACeilingOutlivesTheProcessThatSetItInPostgres(t *testing.T) {
	kept := openPostgres(t)
	s := aCrewNamed(t, kept, "controller-a", 0, &model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	workspace, _ := aProjectOnPostgres(t, s)

	if _, err := s.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{Workspace: workspace, MaxDepth: 3, BudgetTokens: 5000},
	}); err != nil {
		t.Fatalf("SetWorkspaceLimits: %v", err)
	}

	reopened, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatalf("reopen postgres: %v", err)
	}
	t.Cleanup(reopened.Close)

	limits, err := reopened.WorkspaceLimits(ctx, workspace)
	if err != nil {
		t.Fatalf("WorkspaceLimits: %v", err)
	}
	if limits.MaxDepth != 3 || limits.BudgetTokens != 5000 {
		t.Fatalf("the ceiling reads back as %+v from a new process", limits)
	}
}

// The lease a workspace names is what a controller holds that workspace's job for, read from the
// database on every claim rather than remembered.
func TestTheLeaseAWorkspaceNamesIsWhatAControllerUsesInPostgres(t *testing.T) {
	kept := openPostgres(t)
	s := aCrewNamed(t, kept, "controller-a", 0, &model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	if _, err := s.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{Workspace: workspace, LeaseSeconds: 3600},
	}); err != nil {
		t.Fatalf("SetWorkspaceLimits: %v", err)
	}
	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	s.TickJob(ctx)

	found, err := kept.GetJob(ctx, declared.GetJob().GetId())
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if found.LeaseUntil == nil {
		t.Fatal("the job is held with no end to the hold")
	}
	// An hour, not the crew's own minute, which is what says the workspace's number was read.
	if held := found.LeaseUntil.Sub(found.UpdatedAt); held < 50*time.Minute {
		t.Fatalf("the hold lasts %s, want the hour the workspace named", held)
	}
}
