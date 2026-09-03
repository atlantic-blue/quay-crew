package controlplane_test

import (
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/auth"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/role"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A session may declare job, and what bounds it is two things that mean different things: the role
// says what it may do, and the workspace says how much of it. The effective capability is the
// intersection, and a refusal names the one an operator would change.

// asJobCredential is a call made by a session running a job, which is a context carrying
// that job's grant.
func asJobCredential(ctx context.Context, jobID string, verbs ...string) context.Context {
	return auth.WithGrant(ctx, auth.Grant{Job: jobID, Verbs: verbs})
}

// raiseDeclared lets one session in a workspace declare this many jobs.
func raiseDeclared(t *testing.T, s *controlplane.Server, workspace string, many int32) {
	t.Helper()
	if _, err := s.SetWorkspaceLimits(context.Background(), &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{Workspace: workspace, MaxDeclared: many},
	}); err != nil {
		t.Fatalf("SetWorkspaceLimits: %v", err)
	}
}

// Default deny: a workspace nobody configured lets no session declare anything.
func TestAWorkspaceLetsNoSessionDeclareAnythingUntilSomebodyRaisesIt(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	workspace, _ := newProject(t, s)

	held, err := s.GetWorkspaceLimits(context.Background(), &quaycrewv1.GetWorkspaceLimitsRequest{
		Workspace: workspace,
	})
	if err != nil {
		t.Fatalf("GetWorkspaceLimits: %v", err)
	}
	if got := held.GetLimits().GetMaxDeclared(); got != 0 {
		t.Fatalf("a workspace nobody configured lets a session declare %d jobs, want 0 until somebody raises it", got)
	}
	for _, unset := range []int64{int64(held.GetLimits().GetMaxRunning()), held.GetLimits().GetBudgetTokens(),
		int64(held.GetLimits().GetLeaseSeconds())} {
		if unset != 0 {
			t.Fatalf("a workspace nobody configured carries %d, want every limit unset", unset)
		}
	}
}

// The operator declares a job, whatever the ceiling says: the ceiling is on what a session declares,
// not on what a person does. A job an operator declared is caused by nothing and belongs to no run.
func TestTheOperatorDeclaresAJobInAWorkspaceWithNoCeiling(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	declared := declareJob(t, s, project, "read the electricity bill")

	if declared.GetCause() != "" || declared.GetRun() != "" {
		t.Fatalf("the operator's job says %q caused it and it is a step of run %q, want neither",
			declared.GetCause(), declared.GetRun())
	}
}

// What caused a job is read from the credential, so a job a session declares records the job that
// session is running. It is a job in its project, listed beside the one that caused it.
func TestAJobASessionDeclaresRecordsWhatCausedItAndIsListedBesideIt(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	workspace, project := newProject(t, s)
	raiseDeclared(t, s, workspace, 2)
	asked := declareJob(t, s, project, "clear the backlog")

	caused, err := s.CreateJob(asJobCredential(context.Background(), asked.GetId(), role.VerbJobCreate),
		&quaycrewv1.CreateJobRequest{
			Project: project, Title: "pull request 341", Brief: "review it",
		})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if caused.GetJob().GetCause() != asked.GetId() {
		t.Fatalf("the job says %q caused it, want the job the session is running", caused.GetJob().GetCause())
	}
	if caused.GetJob().GetRun() != "" {
		t.Fatalf("a job a session declared is a step of run %q, want none", caused.GetJob().GetRun())
	}
	// Beside it, not under it. Both rows are in the project's listing and neither is folded away.
	listed, err := s.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{Project: project})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	held := map[string]bool{}
	for _, one := range listed.GetJobs() {
		held[one.GetId()] = true
	}
	if len(listed.GetJobs()) != 2 || !held[asked.GetId()] || !held[caused.GetJob().GetId()] {
		t.Fatalf("the project lists %d jobs, want both the job that was asked for and the one it caused",
			len(listed.GetJobs()))
	}
}

// The refusal names the limit and the command that raises it, because a session that was refused has
// to tell its operator what to change.
func TestASessionPastItsCeilingIsRefusedNamingTheLimit(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	workspace, project := newProject(t, s)
	raiseDeclared(t, s, workspace, 1)
	asked := declareJob(t, s, project, "clear the backlog")
	as := asJobCredential(context.Background(), asked.GetId(), role.VerbJobCreate)
	if _, err := s.CreateJob(as, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "pull request 341", Brief: "review it",
	}); err != nil {
		t.Fatalf("the first job this session declared: %v", err)
	}

	_, err := s.CreateJob(as, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "write a test", Brief: "write it",
	})

	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a second job from a session allowed one answered %v, want PermissionDenied", status.Code(err))
	}
	for _, want := range []string{"declare 1 jobs", "declared 1 already", "krewe limits"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal says %q, want it to say %q", err, want)
		}
	}
	// And nothing was written: a refusal that leaves a row behind is a refusal nobody can trust.
	listed, err := s.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{Project: project})
	if err != nil {
		t.Fatalf("ListJob: %v", err)
	}
	if len(listed.GetJobs()) != 2 {
		t.Fatalf("the project holds %d jobs, want the two that were allowed", len(listed.GetJobs()))
	}
}

// A session in a workspace nobody raised may declare nothing at all, which is what default deny
// means when a session meets it.
func TestASessionInAWorkspaceWithNoCeilingDeclaresNothing(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	asked := declareJob(t, s, project, "clear the backlog")

	_, err := s.CreateJob(asJobCredential(context.Background(), asked.GetId(), role.VerbJobCreate),
		&quaycrewv1.CreateJobRequest{Project: project, Title: "pull request 341", Brief: "review it"})

	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a session in a workspace nobody raised answered %v, want PermissionDenied", status.Code(err))
	}
	if !strings.Contains(err.Error(), "declare 0 jobs") {
		t.Fatalf("the refusal says %q, want it to name the limit", err)
	}
}

// A limit is a number, and a number below zero is not one.
func TestALimitBelowZeroIsRefused(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	workspace, _ := newProject(t, s)

	_, err := s.SetWorkspaceLimits(context.Background(), &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{Workspace: workspace, MaxDeclared: -1},
	})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a depth of -1 answered %v, want InvalidArgument", status.Code(err))
	}
}

func TestTheLimitsOfAWorkspaceNobodyHasAreRefused(t *testing.T) {
	s := newServer(&model.FakeRunner{})

	_, err := s.GetWorkspaceLimits(context.Background(), &quaycrewv1.GetWorkspaceLimitsRequest{
		Workspace: "nowhere",
	})

	if status.Code(err) != codes.NotFound {
		t.Fatalf("the limits of a workspace nobody has answered %v, want NotFound", status.Code(err))
	}
}

// The whole row is written, so an operator raising one number does not silently keep another they
// meant to change.
func TestTheCeilingIsWrittenWholeAndReadBack(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	workspace, _ := newProject(t, s)

	written, err := s.SetWorkspaceLimits(context.Background(), &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{
			Workspace: workspace, MaxDeclared: 2, MaxRunning: 4, BudgetTokens: 5000, LeaseSeconds: 90,
		},
	})
	if err != nil {
		t.Fatalf("SetWorkspaceLimits: %v", err)
	}
	if written.GetLimits().GetMaxDeclared() != 2 || written.GetLimits().GetMaxRunning() != 4 ||
		written.GetLimits().GetBudgetTokens() != 5000 || written.GetLimits().GetLeaseSeconds() != 90 {
		t.Fatalf("the ceiling reads back as %+v", written.GetLimits())
	}

	held, err := s.GetWorkspaceLimits(context.Background(), &quaycrewv1.GetWorkspaceLimitsRequest{
		Workspace: workspace,
	})
	if err != nil {
		t.Fatalf("GetWorkspaceLimits: %v", err)
	}
	if held.GetLimits().GetMaxRunning() != 4 {
		t.Fatalf("the ceiling reads back as %+v", held.GetLimits())
	}
}

// The credential is minted for one job, carries the verbs that job's role declared, and
// expires. Everything about it is narrower than the driver's token.
func TestTheCredentialAJobRunsUnderCarriesItsRolesVerbs(t *testing.T) {
	s, kept := serverOnAStore(t)
	workspace, project := newProject(t, s)
	importRoleThatMay(t, s, "backlog-clearer", role.VerbJobCreate, role.VerbJobRead)
	attachRole(t, s, workspace, "backlog-clearer")
	declared, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "clear the backlog", Brief: "read the pull requests",
		Role: "backlog-clearer",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	_ = kept

	token, minted := s.JobCredentialForTest(context.Background(), declared.GetJob().GetId())
	if !minted {
		t.Fatal("no credential was minted for a job")
	}
	grant, recognised := s.Grants().Grant(token)
	if !recognised {
		t.Fatal("the system does not recognise the credential it minted")
	}
	if grant.Job != declared.GetJob().GetId() {
		t.Fatalf("the credential is bound to %q, want the job it was minted for", grant.Job)
	}
	for _, verb := range []string{role.VerbJobCreate, role.VerbJobRead} {
		if !grant.May(verb) {
			t.Errorf("the credential may not %s, and the role declared it", verb)
		}
	}
	for _, verb := range []string{role.VerbJobStop, role.VerbJobAnswer} {
		if grant.May(verb) {
			t.Errorf("the credential may %s, and the role never declared it", verb)
		}
	}
	if grant.ExpiresAt.IsZero() {
		t.Fatal("the credential never expires, so one that leaks out of a sandbox works forever")
	}
}

// Job that runs as no role gets a credential that may call nothing. Default deny: a role is what
// grants, so job without one grants nothing.
func TestJobThatRunsAsNoRoleCarriesACredentialThatMayCallNothing(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	declared := declareJob(t, s, project, "read the electricity bill")

	token, minted := s.JobCredentialForTest(context.Background(), declared.GetId())
	if !minted {
		t.Fatal("no credential was minted")
	}
	grant, _ := s.Grants().Grant(token)
	for _, verb := range role.Grantable {
		if grant.May(verb) {
			t.Errorf("job that runs as no role may %s", verb)
		}
	}
}

// The deadline is the ceiling on the credential's life, because a grant that outlives the job it
// belongs to is a grant nobody is watching.
func TestACredentialExpiresNoLaterThanTheWorksDeadline(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	deadline := timeSoon()
	declared, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
		Deadline: deadline,
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	token, _ := s.JobCredentialForTest(context.Background(), declared.GetJob().GetId())
	grant, _ := s.Grants().Grant(token)

	if grant.ExpiresAt.After(deadline.AsTime()) {
		t.Fatalf("the credential runs to %v, past the job's own deadline of %v", grant.ExpiresAt, deadline.AsTime())
	}
}

func importRoleThatMay(t *testing.T, s *controlplane.Server, name string, verbs ...string) {
	t.Helper()
	manifest := "name: " + name + "\nversion: 1\nsummary: clears the backlog\nmodel: opus\nreceives:\n  - job\n"
	if len(verbs) > 0 {
		manifest += "verbs:\n"
		for _, verb := range verbs {
			manifest += "  - " + verb + "\n"
		}
	}
	if _, err := s.ImportRole(context.Background(), &quaycrewv1.ImportRoleRequest{
		Files: []*quaycrewv1.RoleFile{
			{Path: role.ManifestFile, Body: []byte(manifest)},
			{Path: role.BriefFile, Body: []byte("Read the open pull requests.")},
		},
	}); err != nil {
		t.Fatalf("ImportRole: %v", err)
	}
}

// timeSoon is a deadline close enough that the credential's own life is not the shorter of the two.
func timeSoon() *timestamppb.Timestamp {
	return timestamppb.New(time.Now().UTC().Add(time.Minute))
}

// The credential travels on the task and never at sandbox birth.
//
// A sandbox keeps the configuration it was made with, so a credential written at birth would label
// every later task with the first task's grant, and one minted after birth would never reach the
// container at all. This is the assertion that pins the shape: the sandbox carries no token, and the
// task does.
func TestTheCredentialTravelsOnTheTaskAndNeverAtSandboxBirth(t *testing.T) {
	runner := &model.FakeRunner{Reply: "done"}
	provider := &sandbox.FakeProvider{}
	s := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: runner, Provider: provider, Secrets: secrets.NewMemory(),
		// A system a session could reach, because a credential is only written where the address is.
		Reachable: "controlplane:50051",
	})
	_, project := newProject(t, s)
	declared := declareJob(t, s, project, "read the electricity bill")

	if _, err := s.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Handle: "job-session", Text: "open the bill",
		Job: declared.GetId(),
	}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if len(provider.Created) != 1 {
		t.Fatalf("%d sandboxes were made, want 1", len(provider.Created))
	}
	for _, line := range provider.Created[0].Env {
		if strings.HasPrefix(line, auth.TokenEnv+"=") {
			t.Fatalf("the sandbox was born carrying a credential: %q", line)
		}
	}
	token := runner.LastReq.Env[auth.TokenEnv]
	if token == "" {
		t.Fatal("the task carried no credential, so a session running job holds nothing")
	}
	grant, recognised := s.Grants().Grant(token)
	if !recognised || grant.Job != declared.GetId() {
		t.Fatalf("the task carried a credential for %q, want the job it runs", grant.Job)
	}
	if runner.LastReq.Env["QC_GRPC_ADDR"] == "" {
		t.Fatal("the task carries a credential and no address, so the session cannot use it")
	}
}

// A task that runs no job carries no credential at all, which is every task the system ran
// before this existed.
func TestATaskThatRunsNoJobCarriesNoCredential(t *testing.T) {
	runner := &model.FakeRunner{Reply: "done"}
	s := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: runner, Provider: &sandbox.FakeProvider{},
		Secrets: secrets.NewMemory(), Reachable: "controlplane:50051",
	})
	_, project := newProject(t, s)

	if _, err := s.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Text: "hello",
	}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if token := runner.LastReq.Env[auth.TokenEnv]; token != "" {
		t.Fatalf("a task running no job carried a credential: %q", token)
	}
}
