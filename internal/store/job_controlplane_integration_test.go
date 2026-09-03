//go:build integration

package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/auth"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/role"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
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
	if one.GetCause() != "" || one.GetRun() != "" {
		t.Fatalf("the job says %q caused it and it is a step of run %q, want neither",
			one.GetCause(), one.GetRun())
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
	// A second job, one a session declared, written through the store because the cause comes from a
	// credential and nothing mints one here. It is a step of a run as well, so the filter that reads
	// a run's steps is proved against the column rather than against the memory store alone.
	child := &job.Job{
		ID: store.NewID(), Workspace: workspace, Project: project, Title: "the child",
		Brief: "declared by a session", Cause: root.GetJob().GetId(), Run: "run-1",
		Version: 1, Phase: job.PhasePending,
	}
	if err := kept.CreateJob(ctx, child, &job.Event{
		ID: store.NewID(), Kind: job.EventDeclared, Job: child.ID,
		Workspace: workspace, Project: project,
		Detail: child.Title, OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateJob for the second job: %v", err)
	}

	for _, tc := range []struct {
		name    string
		request *quaycrewv1.ListJobsRequest
		want    []string
	}{
		{"the project", &quaycrewv1.ListJobsRequest{Project: project}, []string{child.ID, root.GetJob().GetId()}},
		{"the workspace", &quaycrewv1.ListJobsRequest{Workspace: workspace}, []string{child.ID, root.GetJob().GetId()}},
		{"the steps of one run", &quaycrewv1.ListJobsRequest{Run: "run-1"}, []string{child.ID}},
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

// A job a session declares states its own sentence, and the database is what carries whatever it
// states. Nothing is taken from the job whose session declared it: no job sits under another, so
// there is nothing to take it from.
func TestAJobASessionDeclaresStatesItsOwnSentenceInTheDatabase(t *testing.T) {
	s, kept := aSystemOnPostgres(t)
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	if _, err := s.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{Workspace: workspace, MaxDeclared: 2},
	}); err != nil {
		t.Fatalf("SetWorkspaceLimits: %v", err)
	}
	sentence := "paste a link and get the text back"

	asked, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "build the transcript page",
		Brief: "read the design and build what it describes", Product: sentence,
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if got := asked.GetJob().GetProduct(); got != sentence {
		t.Fatalf("the job an operator declared serves %q, want %q", got, sentence)
	}

	// One the session running it declared, saying nothing about what a person gets. It is an errand,
	// and it is a job in the same project.
	errand := declaredBy(t, s, kept, project, asked.GetJob().GetId(), "decide what the address carries", "")
	// And one that states a sentence of its own, which stands: there is nothing above it to disagree
	// with.
	own := declaredBy(t, s, kept, project, asked.GetJob().GetId(), "write the page",
		"search the archive by video id")

	for _, tc := range []struct {
		id   string
		want string
	}{
		{errand, ""},
		{own, "search the archive by video id"},
	} {
		found, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: tc.id})
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if got := found.GetJob().GetProduct(); got != tc.want {
			t.Fatalf("job %s serves %q, want %q", tc.id, got, tc.want)
		}
		if found.GetJob().GetCause() != asked.GetJob().GetId() {
			t.Fatalf("job %s says %q caused it", tc.id, found.GetJob().GetCause())
		}
	}

	// And what a controller would hand the session, read off the row rather than off the call that
	// declared it.
	read, err := kept.GetJob(ctx, own)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if asked := job.Asked(read); !strings.Contains(asked, "search the archive by video id") {
		t.Fatalf("the session is asked %q, and it never sees the sentence its job states", asked)
	}

	// Three rows, side by side in the project. None of them is under another.
	listed, err := kept.ListJobs(ctx, job.Filter{Project: project})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("the project holds %d jobs, want the three that were declared", len(listed))
	}
}

// declaredBy writes one job the way the system writes it for a session running another: the control
// plane holds the declaration to every rule and names what caused it, and the store writes the row.
func declaredBy(t *testing.T, s *controlplane.Server, kept store.Store, project, cause, brief, product string) string {
	t.Helper()
	ctx := context.Background()
	declared, event, err := s.PrepareJob(ctx, job.Placement{Cause: cause}, job.Declaration{
		Project: project, Title: brief, Brief: brief, Product: product,
	})
	if err != nil {
		t.Fatalf("PrepareJob caused by %s: %v", cause, err)
	}
	if err := kept.CreateJob(ctx, declared, event); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	return declared.ID
}

// A steer recorded against a job in flight is read back on that job, and the count is that job's.
//
// Only the real engine says this. The steer is a row of its own and the count is a column on another
// table, written in one transaction, so what is proved here is the half the in memory store cannot
// have an opinion about: the foreign key holds, the count survives the read that rebuilds a job out
// of its columns, and the marks come back in the order they were made.
func TestASteerIsReadBackOnTheJobThroughPostgres(t *testing.T) {
	s, _ := aSystemOnPostgres(t)
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	if _, err := s.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{Workspace: workspace, MaxDeclared: 2},
	}); err != nil {
		t.Fatalf("SetWorkspaceLimits: %v", err)
	}
	root, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "build the transcripts page", Brief: "build what the design describes",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	// The second job is declared the way a session declares one: what caused it comes off the
	// credential the caller presented, never off the request.
	under := auth.WithGrant(ctx, auth.Grant{Job: root.GetJob().GetId(), Verbs: []string{role.VerbJobCreate}})
	declared, err := s.CreateJob(under, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "fetch the captions", Brief: "fetch them once and keep them",
	})
	if err != nil {
		t.Fatalf("declare the child: %v", err)
	}
	child := declared.GetJob().GetId()

	for _, said := range []struct{ on, text string }{
		{on: root.GetJob().GetId(), text: "the workspace has no secrets"},
		{on: child, text: "it chose a store that bills while idle"},
	} {
		if _, err := s.RecordSteer(ctx, &quaycrewv1.RecordSteerRequest{Job: said.on, Text: said.text}); err != nil {
			t.Fatalf("RecordSteer on %s: %v", said.on, err)
		}
	}

	// Each count is its own job's. A steer belongs to the job it landed on, and the job whose session
	// declared that one is a job in its own right.
	for _, counted := range []struct {
		name string
		id   string
		want int32
	}{
		{name: "the job an operator declared", id: root.GetJob().GetId(), want: 1},
		{name: "the job its session declared", id: child, want: 1},
	} {
		read, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: counted.id})
		if err != nil {
			t.Fatalf("GetJob %s: %v", counted.name, err)
		}
		if read.GetJob().GetSteers() != counted.want {
			t.Fatalf("%s counts %d steers, want %d", counted.name, read.GetJob().GetSteers(), counted.want)
		}
	}

	listed, err := s.ListSteers(ctx, &quaycrewv1.ListSteersRequest{Job: child})
	if err != nil {
		t.Fatalf("ListSteers: %v", err)
	}
	if len(listed.GetSteers()) != 1 {
		t.Fatalf("the report carries %d steers, want the one made against that job", len(listed.GetSteers()))
	}
	if listed.GetSteers()[0].GetJob() != child {
		t.Fatalf("the steer says it landed on %q, want the job the report was read against",
			listed.GetSteers()[0].GetJob())
	}
	if listed.GetJob().GetId() != child {
		t.Fatalf("the report names %q, want the job it was read against", listed.GetJob().GetId())
	}
}

// A steer against a job the database does not hold is refused by the foreign key as much as by the
// call, and the row is not written.
func TestASteerAgainstAJobThatIsNotThereIsRefusedThroughPostgres(t *testing.T) {
	s, kept := aSystemOnPostgres(t)
	ctx := context.Background()
	aProjectOnPostgres(t, s)

	if _, err := s.RecordSteer(ctx, &quaycrewv1.RecordSteerRequest{
		Job: "0123456789abcdef01234567", Text: "the workspace has no secrets",
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("a steer against a job nobody has answered %v, want NotFound", err)
	}
	listed, err := kept.ListSteers(ctx, "0123456789abcdef01234567")
	if err != nil {
		t.Fatalf("ListSteers: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("the store holds %d steers against a job it does not have", len(listed))
	}
}
