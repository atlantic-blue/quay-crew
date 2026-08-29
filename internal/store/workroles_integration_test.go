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
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/atlantic-blue/quay-crew/internal/work"
)

// Work that names a role, over the real database and the real control plane.
//
// The unit tier proves the controller's decisions against doubles. What only this tier reaches is
// the crossing: the role and what the work requires are columns, they are read back by a different
// process than wrote them, and the session the crew builds either carries the role or does not.

// aCrewWithRoles stands the control plane up on a real database, with a provider that records every
// sandbox it was asked for, which is how a test says no container started.
func aCrewWithRoles(t *testing.T, runner model.Runner) (*controlplane.Server, *sandbox.FakeProvider) {
	t.Helper()
	truncate(t)
	kept, err := store.NewPostgres(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(kept.Close)
	boxes := &sandbox.FakeProvider{}
	return controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: runner, Provider: boxes, Secrets: secrets.NewMemory(),
	}), boxes
}

// importRoleOnPostgres imports a role receiving exactly what it is given, and attaches it.
func importRoleOnPostgres(t *testing.T, s *controlplane.Server, workspace, name string, version int, material ...string) {
	t.Helper()
	ctx := context.Background()
	receives := ""
	for _, one := range material {
		receives += "  - " + one + "\n"
	}
	manifest := "name: " + name + "\nversion: " + strconv.Itoa(version) +
		"\nsummary: clears the open pull request backlog\nmodel: opus\nreceives:\n" + receives
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

// The whole of what this slice buys, against the database that holds it: work names a role and the
// session that runs it runs as that role.
func TestWorkInARoleRunsInASessionRunningAsThatRoleInPostgres(t *testing.T) {
	s, _ := aCrewWithRoles(t, &model.FakeRunner{Reply: "nine pull requests are open"})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	importRoleOnPostgres(t, s, workspace, "backlog-clearer", 1, "work", "context")

	declared, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "clear the backlog", Brief: "read the open pull requests",
		Role: "backlog-clearer", Requires: []string{"context"},
	})
	if err != nil {
		t.Fatalf("CreateWork: %v", err)
	}

	done := waitForWork(t, s, declared.GetWork().GetId(), work.PhaseDone)

	if done.GetAnswer() != "nine pull requests are open" {
		t.Fatalf("the answer on the row is %q", done.GetAnswer())
	}
	// The session, not the row: what decides whether the boundary is real is the conversation the
	// crew actually built, and the row saying "backlog-clearer" proves nothing about it.
	listed, err := s.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Project: project})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	found := false
	for _, session := range listed.GetSessions() {
		if session.GetId() != done.GetSession() {
			continue
		}
		found = true
		if session.GetRole() != "backlog-clearer" {
			t.Fatalf("the session that did the work runs as %q, want backlog-clearer", session.GetRole())
		}
	}
	if !found {
		t.Fatalf("the crew holds no session %s, so nothing can reach the conversation", done.GetSession())
	}
}

// The credential that task runs under carries what the role declared it may call. Read here rather
// than inside a sandbox, because what a credential holds is the whole boundary.
func TestTheCredentialForWorkInARoleCarriesThatRolesVerbsInPostgres(t *testing.T) {
	s, _ := aCrewWithRoles(t, &model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	importRoleOnPostgres(t, s, workspace, "backlog-clearer", 1, "work")

	declared, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "clear the backlog", Brief: "read the open pull requests",
		Role: "backlog-clearer",
	})
	if err != nil {
		t.Fatalf("CreateWork: %v", err)
	}
	token, minted := s.WorkCredentialForTest(ctx, declared.GetWork().GetId())
	if !minted {
		t.Fatal("no credential was minted for work that names a role")
	}
	grant, held := s.Grants().Grant(token)
	if !held {
		t.Fatal("the crew does not recognise the credential it minted")
	}
	// This role declared no may list, so it may call nothing. Default deny, the same direction the
	// capability model already took.
	if len(grant.Verbs) != 0 {
		t.Fatalf("the credential may %v, and a role that declared no may list may call nothing", grant.Verbs)
	}
	if grant.Work != declared.GetWork().GetId() {
		t.Fatalf("the credential is bound to %q, want the work it was minted for", grant.Work)
	}
}

// The refusal, at the moment the material would be handed over, and the reason it lives there: the
// role was attached receiving context when the work was declared, and a version that receives less
// was attached while the work sat pending.
func TestWorkRequiringWhatItsRoleStoppedReceivingIsRefusedBeforeAnyContainerInPostgres(t *testing.T) {
	s, boxes := aCrewWithRoles(t, &model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	importRoleOnPostgres(t, s, workspace, "test-writer", 1, "work", "context")

	declared, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "write the tests", Brief: "from the work alone",
		Role: "test-writer", Requires: []string{"context"},
	})
	if err != nil {
		t.Fatalf("CreateWork: %v", err)
	}
	// The role narrows underneath the work that is already declared.
	importRoleOnPostgres(t, s, workspace, "test-writer", 2, "work")

	stopped := waitForWork(t, s, declared.GetWork().GetId(), work.PhaseStopped)

	for _, want := range []string{"test-writer", "context", "declare the work without"} {
		if !strings.Contains(stopped.GetReason(), want) {
			t.Fatalf("the refusal says %q, want it to name %q", stopped.GetReason(), want)
		}
	}
	if boxes.Asked() != 0 {
		t.Fatalf("the crew was asked for %d sandboxes, want none: work is refused before a container starts",
			boxes.Asked())
	}
	if stopped.GetSession() != "" {
		t.Fatalf("the refused work ran in session %q, and no session should exist", stopped.GetSession())
	}
}

// Work with no role is untouched by any of this, over the same database, in the same crew that holds
// a role.
func TestWorkWithNoRoleStillRunsAndBuildsItsContainerInPostgres(t *testing.T) {
	s, boxes := aCrewWithRoles(t, &model.FakeRunner{Reply: "the bill is due on the 14th"})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	importRoleOnPostgres(t, s, workspace, "test-writer", 1, "work")

	declared, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateWork: %v", err)
	}

	done := waitForWork(t, s, declared.GetWork().GetId(), work.PhaseDone)

	if done.GetAnswer() != "the bill is due on the 14th" {
		t.Fatalf("the answer on the row is %q", done.GetAnswer())
	}
	if done.GetRole() != "" {
		t.Fatalf("the work runs as %q, want as nobody", done.GetRole())
	}
	if boxes.Asked() == 0 {
		t.Fatal("the crew was asked for no sandbox, so the work never ran")
	}
}

// The record of a refusal reads the way it happened: claimed, then stopped, and no line saying a
// task was started that never was.
func TestRefusedWorkIsClaimedAndStoppedAndNeverStartedInPostgres(t *testing.T) {
	s, _ := aCrewWithRoles(t, &model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	kept, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(kept.Close)
	workspace, project := aProjectOnPostgres(t, s)
	importRoleOnPostgres(t, s, workspace, "test-writer", 1, "work", "skills")

	declared, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "write the tests", Brief: "from the work alone",
		Role: "test-writer", Requires: []string{"skills"},
	})
	if err != nil {
		t.Fatalf("CreateWork: %v", err)
	}
	importRoleOnPostgres(t, s, workspace, "test-writer", 2, "work")

	waitForWork(t, s, declared.GetWork().GetId(), work.PhaseStopped)

	events, err := kept.ListWorkEvents(ctx, declared.GetWork().GetId())
	if err != nil {
		t.Fatalf("ListWorkEvents: %v", err)
	}
	want := []string{work.EventDeclared, work.EventClaimed, work.EventStopped}
	if len(events) != len(want) {
		t.Fatalf("%d records exist, want %v", len(events), want)
	}
	for i, kind := range want {
		if events[i].Kind != kind {
			t.Fatalf("record %d is %q, want %q", i, events[i].Kind, kind)
		}
	}
}
