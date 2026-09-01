package controlplane_test

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

// What the crew did, read back through the control plane. A session could read the repository it
// stood in and nothing else, so these hold the read that replaced the operator's memory.

// aCrewWithHistory stands up a system holding one project, and hands back the server and the store
// underneath it, so a test can seed jobs that already ended. Nothing in this slice runs a job, and a
// history of nothing but pending jobs proves none of the arithmetic.
func aCrewWithHistory(t *testing.T) (*controlplane.Server, store.Store, string) {
	t.Helper()
	kept := store.NewMemory()
	s := controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: &model.FakeRunner{}, Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	ctx := context.Background()
	workspace, err := s.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := s.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "quay-crew",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return s, kept, project.GetProject().GetId()
}

// ended writes one job that already finished, so a history has something to add up.
type ended struct {
	title, phase, role, pullRequest, reason string
	tokens                                  int64
	steers                                  int
	declared                                time.Time
	ran                                     time.Duration
}

func seed(t *testing.T, kept store.Store, project string, jobs ...ended) {
	t.Helper()
	for _, one := range jobs {
		started := one.declared
		finished := started.Add(one.ran)
		written := &job.Job{
			ID: store.NewID(), Workspace: "acme", Project: project,
			Title: one.title, Brief: "a brief nobody should have to read to know what happened",
			Answer: "an answer nobody should have to read either",
			Role:   one.role, Phase: one.phase, SpentTokens: one.tokens, Steers: one.steers,
			PullRequest: one.pullRequest, Reason: one.reason,
			Version: 1, CreatedAt: one.declared, UpdatedAt: one.declared,
		}
		if one.ran > 0 {
			written.StartedAt, written.FinishedAt = &started, &finished
		}
		if err := kept.CreateJob(context.Background(), written, &job.Event{
			ID: store.NewID(), Kind: job.EventDeclared, Job: written.ID, OccurredAt: one.declared,
		}); err != nil {
			t.Fatalf("seeding %q: %v", one.title, err)
		}
	}
}

func on(day int) time.Time { return time.Date(2026, time.August, day, 9, 0, 0, 0, time.UTC) }

// twoDaysOfWork is the incident this read was built for: the crew's own work over two days, which had
// to be typed into a brief by hand because nothing could read it back.
func twoDaysOfWork(t *testing.T, kept store.Store, project string) {
	t.Helper()
	seed(t, kept, project,
		ended{title: "a failed job is continued rather than repeated", phase: job.PhaseDone,
			role: "implementer", tokens: 62_140, ran: 18 * time.Minute, declared: on(29),
			pullRequest: "https://github.com/atlantic-blue/quay-crew/pull/531"},
		ended{title: "a job counts the steers it took", phase: job.PhaseDone,
			role: "implementer", tokens: 41_000, steers: 1, ran: 12 * time.Minute, declared: on(29),
			pullRequest: "https://github.com/atlantic-blue/quay-crew/pull/530"},
		ended{title: "prove the coverage gate ran", phase: job.PhaseFailed,
			role: "verifier", tokens: 18_004, ran: 4 * time.Minute, declared: on(30),
			reason: "the gate was piped through tail, so its exit status said nothing"},
		ended{title: "write the release notes", phase: job.PhaseStopped,
			role: "releaser", tokens: 900, ran: time.Minute, declared: on(30),
			reason: "stopped by the operator"},
		ended{title: "read the machine's headroom", phase: job.PhaseRunning,
			role: "implementer", declared: on(30)},
	)
}

func TestAHistorySaysWhatRanWhatItCostAndWhatFailed(t *testing.T) {
	s, kept, project := aCrewWithHistory(t)
	twoDaysOfWork(t, kept, project)

	read, err := s.GetHistory(context.Background(), &quaycrewv1.GetHistoryRequest{
		Since: timestamppb.New(on(29)), Until: timestamppb.New(on(31)),
	})
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}

	total := read.GetTotal()
	if total.GetJobs() != 5 || total.GetDone() != 2 || total.GetFailed() != 1 ||
		total.GetStopped() != 1 || total.GetUnfinished() != 1 {
		t.Fatalf("the window reads %+v, want 5 jobs: 2 done, 1 failed, 1 stopped, 1 unfinished", total)
	}
	if total.GetSpentTokens() != 122_044 {
		t.Fatalf("the window cost %d tokens, want 122044", total.GetSpentTokens())
	}
	if total.GetPullRequests() != 2 {
		t.Fatalf("%d jobs opened a pull request, want 2", total.GetPullRequests())
	}
	if total.GetSteers() != 1 {
		t.Fatalf("the window took %d steers, want 1", total.GetSteers())
	}
	// 18 + 12 + 4 + 1 minutes, and nothing from the job that is still running.
	if want := int64(35 * 60); total.GetWorkingSeconds() != want {
		t.Fatalf("the window worked for %d seconds, want %d", total.GetWorkingSeconds(), want)
	}
}

// Why a job failed comes back with the job, because "what failed and why" is one question. A reader
// who has to ask again for every failure is back where they started.
func TestAFailedJobCarriesItsReason(t *testing.T) {
	s, kept, project := aCrewWithHistory(t)
	twoDaysOfWork(t, kept, project)

	read, err := s.GetHistory(context.Background(), &quaycrewv1.GetHistoryRequest{
		Since: timestamppb.New(on(29)), Until: timestamppb.New(on(31)),
	})
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	for _, one := range read.GetJobs() {
		if one.GetPhase() != job.PhaseFailed {
			continue
		}
		if !strings.Contains(one.GetReason(), "piped through tail") {
			t.Fatalf("the failure says %q, and does not say why it failed", one.GetReason())
		}
		return
	}
	t.Fatal("the history holds no failed job, so a reader cannot see what went wrong")
}

// The read that has to stay affordable. A digest carrying the brief and the answer would cost a
// session the context it wanted the history in order to spend.
func TestAHistoryCarriesNoBriefAndNoAnswer(t *testing.T) {
	s, kept, project := aCrewWithHistory(t)
	twoDaysOfWork(t, kept, project)

	read, err := s.GetHistory(context.Background(), &quaycrewv1.GetHistoryRequest{
		Since: timestamppb.New(on(29)), Until: timestamppb.New(on(31)),
	})
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(read.GetJobs()) == 0 {
		t.Fatal("the history came back empty, so this proves nothing")
	}
	// The seed writes both, so a digest that carried either would show it here.
	for _, one := range read.GetJobs() {
		rendered := one.String()
		if strings.Contains(rendered, "a brief nobody should have to read") ||
			strings.Contains(rendered, "an answer nobody should have to read") {
			t.Fatalf("a digest carries the prose of the job: %s", rendered)
		}
	}
}

// The whole correctness of this read. The total is taken over the window and the page is cut
// afterwards, so a reader who takes two rows is not handed a summary of two rows.
func TestTheTotalCoversTheWindowEvenWhenTheLimitCutsTheListing(t *testing.T) {
	s, kept, project := aCrewWithHistory(t)
	twoDaysOfWork(t, kept, project)

	read, err := s.GetHistory(context.Background(), &quaycrewv1.GetHistoryRequest{
		Since: timestamppb.New(on(29)), Until: timestamppb.New(on(31)), Limit: 2,
	})
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(read.GetJobs()) != 2 {
		t.Fatalf("the listing holds %d jobs, want the 2 that were asked for", len(read.GetJobs()))
	}
	if read.GetLeftOut() != 3 {
		t.Fatalf("the answer left out %d jobs, want 3", read.GetLeftOut())
	}
	if read.GetTotal().GetJobs() != 5 || read.GetTotal().GetSpentTokens() != 122_044 {
		t.Fatalf("the total reads %d jobs and %d tokens, and describes the page rather than the window",
			read.GetTotal().GetJobs(), read.GetTotal().GetSpentTokens())
	}
}

func TestAHistoryLeavesOutWhatFallsOutsideTheWindow(t *testing.T) {
	s, kept, project := aCrewWithHistory(t)
	twoDaysOfWork(t, kept, project)

	read, err := s.GetHistory(context.Background(), &quaycrewv1.GetHistoryRequest{
		Since: timestamppb.New(on(30)), Until: timestamppb.New(on(31)),
	})
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if read.GetTotal().GetJobs() != 3 {
		t.Fatalf("a window over one day holds %d jobs, want the 3 declared that day",
			read.GetTotal().GetJobs())
	}
}

// The window comes back with the answer, so a caller that named neither end still knows what it read.
func TestAHistorySaysWhichWindowItRead(t *testing.T) {
	s, _, _ := aCrewWithHistory(t)

	read, err := s.GetHistory(context.Background(), &quaycrewv1.GetHistoryRequest{})
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if read.GetSince() == nil || read.GetUntil() == nil {
		t.Fatal("a history that bounded itself does not say what window it read")
	}
	if got := read.GetUntil().AsTime().Sub(read.GetSince().AsTime()); got != job.DefaultWindow {
		t.Fatalf("a history nobody bounded read %s, want the default window of %s", got, job.DefaultWindow)
	}
}

func TestAWindowThatEndsBeforeItStartsIsRefused(t *testing.T) {
	s, _, _ := aCrewWithHistory(t)

	_, err := s.GetHistory(context.Background(), &quaycrewv1.GetHistoryRequest{
		Since: timestamppb.New(on(30)), Until: timestamppb.New(on(28)),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a backwards window was answered with %v, want an invalid argument", err)
	}
}

// A history narrows to one project, so a total from one project is never taken for the crew's.
func TestAHistoryNarrowsToOneProject(t *testing.T) {
	s, kept, project := aCrewWithHistory(t)
	twoDaysOfWork(t, kept, project)
	other, err := s.CreateProject(context.Background(), &quaycrewv1.CreateProjectRequest{
		Workspace: workspaceOf(t, s), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	seed(t, kept, other.GetProject().GetId(),
		ended{title: "read the electricity bill", phase: job.PhaseDone, declared: on(30), tokens: 10})

	read, err := s.GetHistory(context.Background(), &quaycrewv1.GetHistoryRequest{
		Project: other.GetProject().GetId(),
		Since:   timestamppb.New(on(29)), Until: timestamppb.New(on(31)),
	})
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if read.GetTotal().GetJobs() != 1 || read.GetTotal().GetSpentTokens() != 10 {
		t.Fatalf("a history of one project reads %+v, want only that project's one job", read.GetTotal())
	}
}

// workspaceOf is the workspace the crew was built in, for a test that needs a second project in it.
func workspaceOf(t *testing.T, s *controlplane.Server) string {
	t.Helper()
	listed, err := s.ListWorkspaces(context.Background(), &quaycrewv1.ListWorkspacesRequest{})
	if err != nil || len(listed.GetWorkspaces()) == 0 {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	return listed.GetWorkspaces()[0].GetId()
}

// A role that may not read jobs may not read the history either, and a role that may, may. The
// history is a digest of jobs, so it is held to the verb that already guards them rather than to a
// second one meaning the same thing.
func TestAHistoryNeedsTheVerbThatGuardsJobs(t *testing.T) {
	method := quaycrewv1.ControlPlaneService_GetHistory_FullMethodName

	refused := controlplane.DeniedToJob(method, &quaycrewv1.GetHistoryRequest{},
		auth.Grant{Job: "job-1", Verbs: []string{role.VerbJobCreate}})
	if status.Code(refused) != codes.PermissionDenied {
		t.Fatalf("a role without %s read the history: %v", role.VerbJobRead, refused)
	}
	// Named, so a session that was refused knows what to ask its operator for.
	if !strings.Contains(refused.Error(), role.VerbJobRead) {
		t.Fatalf("the refusal does not name the verb to ask for: %v", refused)
	}

	allowed := controlplane.DeniedToJob(method, &quaycrewv1.GetHistoryRequest{},
		auth.Grant{Job: "job-1", Verbs: []string{role.VerbJobRead}})
	if allowed != nil {
		t.Fatalf("a role holding %s was refused the history: %v", role.VerbJobRead, allowed)
	}
}
