//go:build integration

package store_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/role"
	"github.com/atlantic-blue/krewe/internal/store"
)

// The role that reads a plan before the build, carried through the real database and the real
// control plane.
//
// The unit tier reads the file off disk and holds the method in it to the rules. What only this tier
// reaches is the crossing: the role is written into columns by one call and read back by another,
// the job says it cannot be done without the context, and the session the system builds either runs
// as the role and holds the context or it does not.

// planCriticOnPostgres imports the role this build ships, from roles/, and attaches it to the
// workspace. The file rather than a manifest written here, because a test that imports its own role
// proves nothing about the one that ships.
func planCriticOnPostgres(t *testing.T, s *controlplane.Server, workspace string) {
	t.Helper()
	ctx := context.Background()
	read, err := role.ReadDir(shippedRoles + "/plan-critic")
	if err != nil {
		t.Fatalf("reading the plan-critic role this build ships: %v", err)
	}
	files := make([]*quaycrewv1.RoleFile, 0, len(read))
	for _, file := range read {
		files = append(files, &quaycrewv1.RoleFile{Path: file.Path, Body: file.Body})
	}
	if _, err := s.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{Files: files}); err != nil {
		t.Fatalf("the system refused the plan-critic role, which ships with it: %v", err)
	}
	if _, err := s.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{Workspace: workspace, Name: "plan-critic"}); err != nil {
		t.Fatalf("AttachRole: %v", err)
	}
}

// narrowThePlanCritic attaches a second version of the same role that no longer receives the
// context. It keeps the shipped summary, model and brief, so what changed between the two versions
// is the one thing the refusal turns on.
func narrowThePlanCritic(t *testing.T, s *controlplane.Server, workspace string) {
	t.Helper()
	ctx := context.Background()
	onDisk, err := role.One(shippedRoles + "/plan-critic")
	if err != nil {
		t.Fatalf("reading the plan-critic role this build ships: %v", err)
	}
	manifest := "name: plan-critic\nversion: 2\nsummary: " + onDisk.Summary +
		"\nmodel: " + onDisk.Model + "\nreceives:\n  - job\n  - skills\n"
	if _, err := s.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
		Files: []*quaycrewv1.RoleFile{
			{Path: role.ManifestFile, Body: []byte(manifest)},
			{Path: role.BriefFile, Body: []byte(onDisk.Brief)},
		},
	}); err != nil {
		t.Fatalf("importing the narrowed plan-critic: %v", err)
	}
	if _, err := s.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{Workspace: workspace, Name: "plan-critic"}); err != nil {
		t.Fatalf("AttachRole: %v", err)
	}
}

// The boundary first. This role declares no verbs, so the credential the system mints for a job
// running as it carries none, and a session that could declare a job could turn a report into a
// build. Read off the credential rather than off the manifest, because the manifest is the claim and
// the credential is what the system holds a session to.
func TestTheCredentialForAPlanCriticJobMayCallNothingInPostgres(t *testing.T) {
	s, _ := aSystemWithRoles(t, &model.FakeRunner{Reply: "the plan does not say what a person types"})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	planCriticOnPostgres(t, s, workspace)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the plan", Brief: "read the design, the contracts and the build order",
		Role: "plan-critic", Requires: []string{"context"},
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	token, minted := s.JobCredentialForTest(ctx, declared.GetJob().GetId())
	if !minted {
		t.Fatal("no credential was minted for a job that names the plan-critic role")
	}
	grant, held := s.Grants().Grant(token)
	if !held {
		t.Fatal("the system does not recognise the credential it minted")
	}
	if len(grant.Verbs) != 0 {
		t.Fatalf("the credential may %v, and this role reports rather than declaring anything", grant.Verbs)
	}
}

// And the other half of the boundary, which no listing can answer: the role narrows underneath a job
// that already said it cannot be done without the context, and the job is refused before any
// container is built. A role that reads a plan with no context has no standards to check it against,
// so running it anyway would answer plausibly instead of stopping.
func TestAPlanCriticJobIsRefusedWhenTheRoleStopsReceivingContextInPostgres(t *testing.T) {
	s, boxes := aSystemWithRoles(t, &model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	planCriticOnPostgres(t, s, workspace)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the plan", Brief: "read the design, the contracts and the build order",
		Role: "plan-critic", Requires: []string{"context"},
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	// The same role at a version that receives less, attached while the job sits pending.
	narrowThePlanCritic(t, s, workspace)

	stopped := waitForJob(t, s, declared.GetJob().GetId(), job.PhaseStopped)

	for _, want := range []string{"plan-critic", "context"} {
		if !strings.Contains(stopped.GetReason(), want) {
			t.Fatalf("the refusal says %q, want it to name %q", stopped.GetReason(), want)
		}
	}
	if boxes.Asked() != 0 {
		t.Fatalf("the system was asked for %d sandboxes, want none", boxes.Asked())
	}
}

// The happy path, and the one the whole slice is for: a job that names this role and cannot be done
// without the context runs, in a session running as the role, and the brief that session would be
// told is the file's brief and not a truncation of it.
//
// The job states no sentence, and that is the crossing this tier is for rather than an oversight. A
// job at the top that states one writes a plan and waits for a person to approve it, so it never
// reaches done on its own, and what that gate does is a different test's subject. The sentence the
// critic reads a plan against is held in features/plancritic.feature.
func TestAPlanCriticJobRunsInASessionThatReceivesTheContextInPostgres(t *testing.T) {
	answer := "one finding: section 3 says the address carries an identifier and the sentence says a link"
	s, boxes := aSystemWithRoles(t, &model.FakeRunner{Reply: answer})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	planCriticOnPostgres(t, s, workspace)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the plan", Brief: "read the design, the contracts and the build order",
		Role: "plan-critic", Requires: []string{"context"},
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	done := waitForJob(t, s, declared.GetJob().GetId(), job.PhaseDone)

	if done.GetAnswer() != answer {
		t.Fatalf("the answer on the row is %q", done.GetAnswer())
	}
	if boxes.Asked() == 0 {
		t.Fatal("the system was asked for no sandbox, so the job never ran")
	}
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
		if session.GetRole() != "plan-critic" {
			t.Fatalf("the session that read the plan runs as %q, want plan-critic", session.GetRole())
		}
	}
	if !found {
		t.Fatalf("the system holds no session %s, so nothing can reach the conversation", done.GetSession())
	}

	// What the system would put in front of that session, read back out of the column rather than off
	// the import that answered. A brief that came back empty satisfies every check about what it does
	// not say, so it is held to the file first.
	onDisk, err := role.One(shippedRoles + "/plan-critic")
	if err != nil {
		t.Fatalf("reading the plan-critic role this build ships: %v", err)
	}
	kept, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer kept.Close()
	back, err := kept.GetRole(ctx, "plan-critic", 1)
	if err != nil {
		t.Fatalf("GetRole: %v", err)
	}
	if back.Brief != onDisk.Brief {
		t.Fatalf("the brief came back at %d bytes and the file is %d", len(back.Brief), len(onDisk.Brief))
	}
	if !back.Gets(role.MaterialContext) {
		t.Fatalf("the role the database holds receives %v, and the job it ran said it needed the context",
			back.Receives)
	}
	t.Logf("the plan-critic brief came back whole at %d bytes", len(back.Brief))
}
