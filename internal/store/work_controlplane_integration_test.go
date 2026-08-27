//go:build integration

package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/atlantic-blue/quay-crew/internal/work"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The control plane over a real database.
//
// The conformance suite proves the store keeps its contract, and the unit tier proves the calls
// refuse what they should. Neither reaches the shapes only Postgres has: a text array, a jsonb
// document, a nullable parent that a foreign key holds, and a transaction that has to carry the row
// and the record of it together. Those either work against the real engine or they do not.

// aCrewOnPostgres stands the control plane up on a real database, empty of rows.
func aCrewOnPostgres(t *testing.T) (*controlplane.Server, store.Store) {
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
func TestWorkRoundTripsThroughPostgres(t *testing.T) {
	s, _ := aCrewOnPostgres(t)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)
	deadline := time.Now().UTC().Add(time.Hour).Truncate(time.Second)

	first, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it and say when it is due",
	})
	if err != nil {
		t.Fatalf("CreateWork: %v", err)
	}
	second, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "pay the electricity bill", Brief: "pay it",
		Mode: "plan", ExpectFile: "notes/bill.md", ExpectContains: "paid",
		After: []string{first.GetWork().GetId()}, Deadline: timestamppb.New(deadline),
		BudgetTokens: 5000, Labels: map[string]string{"owner": "house", "kind": "bills"},
	})
	if err != nil {
		t.Fatalf("CreateWork: %v", err)
	}

	found, err := s.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: second.GetWork().GetId()})
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	one := found.GetWork()
	if got := one.GetAfter(); len(got) != 1 || got[0] != first.GetWork().GetId() {
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
		t.Fatalf("the work has parent %q at depth %d, want a root", one.GetParent(), one.GetDepth())
	}
	if one.GetCreatedAt() == nil || one.GetCreatedAt().AsTime().IsZero() {
		t.Fatal("the database did not stamp when the work was declared")
	}
}

// The row and the record of how it came to exist are written together or not at all.
func TestTheRecordOfADeclarationIsCommittedWithTheWork(t *testing.T) {
	s, kept := aCrewOnPostgres(t)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateWork: %v", err)
	}

	events, err := kept.ListWorkEvents(ctx, declared.GetWork().GetId())
	if err != nil {
		t.Fatalf("ListWorkEvents: %v", err)
	}
	if len(events) != 1 || events[0].Kind != work.EventDeclared {
		t.Fatalf("the database holds %d records for the declaration", len(events))
	}
	if events[0].Detail != "read the electricity bill" {
		t.Fatalf("the record says %q", events[0].Detail)
	}

	if _, err := s.StopWork(ctx, &quaycrewv1.StopWorkRequest{
		Id: declared.GetWork().GetId(), Reason: "the bill is not due yet",
	}); err != nil {
		t.Fatalf("StopWork: %v", err)
	}
	events, err = kept.ListWorkEvents(ctx, declared.GetWork().GetId())
	if err != nil {
		t.Fatalf("ListWorkEvents: %v", err)
	}
	if len(events) != 2 || events[1].Kind != work.EventStopped {
		t.Fatalf("the database holds %d records after the stop", len(events))
	}
}

// A declaration the crew refuses must leave nothing behind, which is a claim about the transaction
// as much as about the check.
func TestARefusedDeclarationLeavesNoRowInTheDatabase(t *testing.T) {
	s, _ := aCrewOnPostgres(t)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	if _, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "", Brief: "open it",
	}); err == nil {
		t.Fatal("work with no title was accepted")
	}
	if _, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "read the bill", Brief: "open it",
		After: []string{"0123456789abcdef01234567"},
	}); err == nil {
		t.Fatal("work waiting on something the crew does not hold was accepted")
	}

	listed, err := s.ListWork(ctx, &quaycrewv1.ListWorkRequest{Project: project})
	if err != nil {
		t.Fatalf("ListWork: %v", err)
	}
	if len(listed.GetWork()) != 0 {
		t.Fatalf("%d rows were left behind by refused declarations", len(listed.GetWork()))
	}
}

// The listing narrows in the database rather than in the process, so each of these is a query.
func TestAListingNarrowsInTheDatabase(t *testing.T) {
	s, kept := aCrewOnPostgres(t)
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)

	root, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "the root", Brief: "do it", Labels: map[string]string{"owner": "house"},
	})
	if err != nil {
		t.Fatalf("CreateWork: %v", err)
	}
	// A child, written through the store because the parent comes from a credential and nothing
	// mints one yet. The foreign key on parent is the part being proved.
	child := &work.Work{
		ID: store.NewID(), Workspace: workspace, Project: project, Title: "the child",
		Brief: "under the root", Parent: root.GetWork().GetId(), Depth: 1,
		Version: 1, Phase: work.PhasePending,
	}
	if err := kept.CreateWork(ctx, child, &work.Event{
		ID: store.NewID(), Kind: work.EventDeclared, Work: child.ID,
		Workspace: workspace, Project: project, Parent: child.Parent, Depth: 1,
		Detail: child.Title, OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateWork for the child: %v", err)
	}

	for _, tc := range []struct {
		name    string
		request *quaycrewv1.ListWorkRequest
		want    []string
	}{
		{"the project", &quaycrewv1.ListWorkRequest{Project: project}, []string{child.ID, root.GetWork().GetId()}},
		{"the workspace", &quaycrewv1.ListWorkRequest{Workspace: workspace}, []string{child.ID, root.GetWork().GetId()}},
		{"the children of one", &quaycrewv1.ListWorkRequest{Parent: root.GetWork().GetId()}, []string{child.ID}},
		{"the roots", &quaycrewv1.ListWorkRequest{Project: project, RootsOnly: true}, []string{root.GetWork().GetId()}},
		{"one phase", &quaycrewv1.ListWorkRequest{Project: project, Phase: work.PhasePending}, []string{child.ID, root.GetWork().GetId()}},
		{"one label", &quaycrewv1.ListWorkRequest{Project: project, LabelKey: "owner", LabelValue: "house"}, []string{root.GetWork().GetId()}},
		{"a label key alone", &quaycrewv1.ListWorkRequest{Project: project, LabelKey: "owner"}, []string{root.GetWork().GetId()}},
		{"a label nothing carries", &quaycrewv1.ListWorkRequest{Project: project, LabelKey: "owner", LabelValue: "nobody"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listed, err := s.ListWork(ctx, tc.request)
			if err != nil {
				t.Fatalf("ListWork: %v", err)
			}
			got := make([]string, 0, len(listed.GetWork()))
			for _, one := range listed.GetWork() {
				got = append(got, one.GetId())
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("the listing is %v, want %v", got, tc.want)
			}
		})
	}
}

// Work that already ended is not stopped again, and the database is where that has to hold: the
// update is conditional on the phase, so two callers racing cannot both win.
func TestWorkThatAlreadyEndedIsNotStoppedAgainInTheDatabase(t *testing.T) {
	s, _ := aCrewOnPostgres(t)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateWork: %v", err)
	}
	if _, err := s.StopWork(ctx, &quaycrewv1.StopWorkRequest{
		Id: declared.GetWork().GetId(), Reason: "the bill is not due yet",
	}); err != nil {
		t.Fatalf("StopWork: %v", err)
	}

	_, err = s.StopWork(ctx, &quaycrewv1.StopWorkRequest{
		Id: declared.GetWork().GetId(), Reason: "changed my mind",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("the second stop answered %v, want FailedPrecondition", status.Code(err))
	}
	found, err := s.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: declared.GetWork().GetId()})
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if found.GetWork().GetReason() != "the bill is not due yet" {
		t.Fatalf("the reason is %q, and the second stop should not have reached it", found.GetWork().GetReason())
	}
	if found.GetWork().GetFinishedAt() == nil {
		t.Fatal("stopped work carries no finished time")
	}
}

// Intent outlives the process. A second connection pool is a different process as far as the rows
// are concerned.
func TestWorkOutlivesTheProcessThatDeclaredIt(t *testing.T) {
	s, _ := aCrewOnPostgres(t)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it and say when it is due",
	})
	if err != nil {
		t.Fatalf("CreateWork: %v", err)
	}

	reopened, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatalf("reopen postgres: %v", err)
	}
	t.Cleanup(reopened.Close)
	again := controlplane.NewServer(controlplane.Config{
		Store: reopened, Runner: &model.FakeRunner{}, Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})

	found, err := again.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: declared.GetWork().GetId()})
	if err != nil {
		t.Fatalf("the work was gone from a new process: %v", err)
	}
	if found.GetWork().GetBrief() != "open it and say when it is due" {
		t.Fatalf("the brief reads back as %q", found.GetWork().GetBrief())
	}
	if found.GetWork().GetPhase() != work.PhasePending {
		t.Fatalf("the work is %q, want pending", found.GetWork().GetPhase())
	}
}
