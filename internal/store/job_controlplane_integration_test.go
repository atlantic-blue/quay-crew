//go:build integration

package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The control plane over a real database.
//
// The conformance suite proves the store keeps its contract, and the unit tier proves the calls
// refuse what they should. Neither reaches the shapes only Postgres has: a text array, a jsonb
// document, a nullable parent that a foreign key holds, and a transaction that has to carry the row
// and the record of it together. Those either job against the real engine or they do not.

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

// Everything a caller declares has to come back off the database the way it went in. The array, the
// document and the moment are the three the in memory store cannot say anything about.
func TestJobRoundTripsThroughPostgres(t *testing.T) {
	s, _ := aSystemOnPostgres(t)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)
	deadline := time.Now().UTC().Add(time.Hour).Truncate(time.Second)

	first, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it and say when it is due",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	second, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "pay the electricity bill", Brief: "pay it",
		Mode: "plan", ExpectFile: "notes/bill.md", ExpectContains: "paid",
		After: []string{first.GetJob().GetId()}, Deadline: timestamppb.New(deadline),
		BudgetTokens: 5000, Labels: map[string]string{"owner": "house", "kind": "bills"},
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	found, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: second.GetJob().GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	one := found.GetJob()
	if got := one.GetAfter(); len(got) != 1 || got[0] != first.GetJob().GetId() {
		t.Fatalf("what it waits for reads back as %v", got)
	}
	if one.GetLabels()["owner"] != "house" || one.GetLabels()["kind"] != "bills" {
		t.Fatalf("the labels read back as %v", one.GetLabels())
	}
	if !one.GetDeadline().AsTime().Equal(deadline) {
		t.Fatalf("the deadline reads back as %v, want %v", one.GetDeadline().AsTime(), deadline)
	}
	if one.GetBudgetTokens() != 5000 || one.GetMode() != model.PermissionPlan {
		t.Fatalf("the budget is %d and the mode is %q", one.GetBudgetTokens(), one.GetMode())
	}
	if one.GetParent() != "" || one.GetDepth() != 0 {
		t.Fatalf("the job has parent %q at depth %d, want a root", one.GetParent(), one.GetDepth())
	}
	if one.GetCreatedAt() == nil || one.GetCreatedAt().AsTime().IsZero() {
		t.Fatal("the database did not stamp when the job was declared")
	}
}

// The row and the record of how it came to exist are written together or not at all.
func TestTheRecordOfADeclarationIsCommittedWithTheJob(t *testing.T) {
	s, kept := aSystemOnPostgres(t)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	events, err := kept.ListJobEvents(ctx, declared.GetJob().GetId())
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	if len(events) != 1 || events[0].Kind != job.EventDeclared {
		t.Fatalf("the database holds %d records for the declaration", len(events))
	}
	if events[0].Detail != "read the electricity bill" {
		t.Fatalf("the record says %q", events[0].Detail)
	}

	if _, err := s.StopJob(ctx, &quaycrewv1.StopJobRequest{
		Id: declared.GetJob().GetId(), Reason: "the bill is not due yet",
	}); err != nil {
		t.Fatalf("StopJob: %v", err)
	}
	events, err = kept.ListJobEvents(ctx, declared.GetJob().GetId())
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	if len(events) != 2 || events[1].Kind != job.EventStopped {
		t.Fatalf("the database holds %d records after the stop", len(events))
	}
}

// A declaration the system refuses must leave nothing behind, which is a claim about the transaction
// as much as about the check.
func TestARefusedDeclarationLeavesNoRowInTheDatabase(t *testing.T) {
	s, _ := aSystemOnPostgres(t)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	if _, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "", Brief: "open it",
	}); err == nil {
		t.Fatal("job with no title was accepted")
	}
	if _, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the bill", Brief: "open it",
		After: []string{"0123456789abcdef01234567"},
	}); err == nil {
		t.Fatal("job waiting on something the system does not hold was accepted")
	}

	listed, err := s.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Project: project})
	if err != nil {
		t.Fatalf("ListJob: %v", err)
	}
	if len(listed.GetJobs()) != 0 {
		t.Fatalf("%d rows were left behind by refused declarations", len(listed.GetJobs()))
	}
}

// The listing narrows in the database rather than in the process, so each of these is a query.
func TestAListingNarrowsInTheDatabase(t *testing.T) {
	s, kept := aSystemOnPostgres(t)
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)

	root, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "the root", Brief: "do it", Labels: map[string]string{"owner": "house"},
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	// A child, written through the store because the parent comes from a credential and nothing
	// mints one yet. The foreign key on parent is the part being proved.
	child := &job.Job{
		ID: store.NewID(), Workspace: workspace, Project: project, Title: "the child",
		Brief: "under the root", Parent: root.GetJob().GetId(), Depth: 1,
		Version: 1, Phase: job.PhasePending,
	}
	if err := kept.CreateJob(ctx, child, &job.Event{
		ID: store.NewID(), Kind: job.EventDeclared, Job: child.ID,
		Workspace: workspace, Project: project, Parent: child.Parent, Depth: 1,
		Detail: child.Title, OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateJob for the child: %v", err)
	}

	for _, tc := range []struct {
		name    string
		request *quaycrewv1.ListJobsRequest
		want    []string
	}{
		{"the project", &quaycrewv1.ListJobsRequest{Project: project}, []string{child.ID, root.GetJob().GetId()}},
		{"the workspace", &quaycrewv1.ListJobsRequest{Workspace: workspace}, []string{child.ID, root.GetJob().GetId()}},
		{"the children of one", &quaycrewv1.ListJobsRequest{Parent: root.GetJob().GetId()}, []string{child.ID}},
		{"the roots", &quaycrewv1.ListJobsRequest{Project: project, RootsOnly: true}, []string{root.GetJob().GetId()}},
		{"one phase", &quaycrewv1.ListJobsRequest{Project: project, Phase: job.PhasePending}, []string{child.ID, root.GetJob().GetId()}},
		{"one label", &quaycrewv1.ListJobsRequest{Project: project, LabelKey: "owner", LabelValue: "house"}, []string{root.GetJob().GetId()}},
		{"a label key alone", &quaycrewv1.ListJobsRequest{Project: project, LabelKey: "owner"}, []string{root.GetJob().GetId()}},
		{"a label nothing carries", &quaycrewv1.ListJobsRequest{Project: project, LabelKey: "owner", LabelValue: "nobody"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listed, err := s.ListJobs(ctx, tc.request)
			if err != nil {
				t.Fatalf("ListJob: %v", err)
			}
			got := make([]string, 0, len(listed.GetJobs()))
			for _, one := range listed.GetJobs() {
				got = append(got, one.GetId())
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("the listing is %v, want %v", got, tc.want)
			}
		})
	}
}

// Job that already ended is not stopped again, and the database is where that has to hold: the
// update is conditional on the phase, so two callers racing cannot both win.
func TestJobThatAlreadyEndedIsNotStoppedAgainInTheDatabase(t *testing.T) {
	s, _ := aSystemOnPostgres(t)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if _, err := s.StopJob(ctx, &quaycrewv1.StopJobRequest{
		Id: declared.GetJob().GetId(), Reason: "the bill is not due yet",
	}); err != nil {
		t.Fatalf("StopJob: %v", err)
	}

	_, err = s.StopJob(ctx, &quaycrewv1.StopJobRequest{
		Id: declared.GetJob().GetId(), Reason: "changed my mind",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("the second stop answered %v, want FailedPrecondition", status.Code(err))
	}
	found, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: declared.GetJob().GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if found.GetJob().GetReason() != "the bill is not due yet" {
		t.Fatalf("the reason is %q, and the second stop should not have reached it", found.GetJob().GetReason())
	}
	if found.GetJob().GetFinishedAt() == nil {
		t.Fatal("stopped job carries no finished time")
	}
}

// Intent outlives the process. A second connection pool is a different process as far as the rows
// are concerned.
func TestJobOutlivesTheProcessThatDeclaredIt(t *testing.T) {
	s, _ := aSystemOnPostgres(t)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it and say when it is due",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	reopened, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatalf("reopen postgres: %v", err)
	}
	t.Cleanup(reopened.Close)
	again := controlplane.NewServer(controlplane.Config{
		Store: reopened, Runner: &model.FakeRunner{}, Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})

	found, err := again.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: declared.GetJob().GetId()})
	if err != nil {
		t.Fatalf("the job was gone from a new process: %v", err)
	}
	if found.GetJob().GetBrief() != "open it and say when it is due" {
		t.Fatalf("the brief reads back as %q", found.GetJob().GetBrief())
	}
	if found.GetJob().GetPhase() != job.PhasePending {
		t.Fatalf("the job is %q, want pending", found.GetJob().GetPhase())
	}
}
