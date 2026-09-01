package controlplane_test

import (
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A cap below zero is somebody's typing rather than a question the system can answer, so it is
// refused with a sentence saying what to send instead. Refusing it here is what keeps a caller from
// reading a silent zero as every row.
func TestAListingCannotBeCappedBelowNothing(t *testing.T) {
	s, _ := serverOnAStore(t)
	_, project := newProject(t, s)

	_, err := s.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{Project: project, Limit: -1})

	if err == nil {
		t.Fatal("a listing took a cap of minus one without complaint")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("the refusal is %v, want InvalidArgument", status.Code(err))
	}
	if !strings.Contains(err.Error(), "leave the limit out") {
		t.Fatalf("the refusal says %q, want it to say what to send instead", err)
	}
}

// What the briefing's third block asks for: the jobs that finished lately, newest finished first,
// capped. The order matters more than the narrowing, so the jobs below are declared in the reverse of
// the order they finished in.
func TestAListingAnswersWhatFinishedLatelyNewestFirst(t *testing.T) {
	s, kept := serverOnAStore(t)
	workspace, project := newProject(t, s)

	now := time.Now().UTC()
	newer := finishedJob(t, kept, workspace, project, "the newer piece of work",
		now.Add(-96*time.Hour), now.Add(-time.Hour))
	older := finishedJob(t, kept, workspace, project, "the older piece of work",
		now.Add(-72*time.Hour), now.Add(-24*time.Hour))
	lastMonth := finishedJob(t, kept, workspace, project, "something from last month",
		now.Add(-48*time.Hour), now.Add(-30*24*time.Hour))
	running := &job.Job{
		ID: store.NewID(), Workspace: workspace, Project: project, Title: "still running",
		Brief: "open it", Version: 1, Phase: job.PhaseRunning,
	}
	if err := kept.CreateJob(context.Background(), running, declaredRecord(running)); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	week := timestamppb.New(now.Add(-7 * 24 * time.Hour))
	listed, err := s.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{
		Project: project, Phase: job.PhaseDone, FinishedSince: week, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	for _, one := range listed.GetJobs() {
		if one.GetId() == lastMonth {
			t.Error("work that finished before the window is inside it")
		}
		if one.GetId() == running.ID {
			t.Error("a job that has not finished is inside a window about jobs that finished")
		}
	}
	if len(listed.GetJobs()) != 2 {
		t.Fatalf("the window holds %d jobs, want the two that finished inside it", len(listed.GetJobs()))
	}
	if listed.GetJobs()[0].GetId() != newer || listed.GetJobs()[1].GetId() != older {
		t.Fatalf("the window opens with %q, want the most recently finished first",
			listed.GetJobs()[0].GetTitle())
	}

	capped, err := s.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{
		Project: project, Phase: job.PhaseDone, FinishedSince: week, Limit: 1,
	})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(capped.GetJobs()) != 1 || capped.GetJobs()[0].GetId() != newer {
		t.Fatalf("a cap of one gave %d rows, want the most recently finished alone", len(capped.GetJobs()))
	}

	// A request that sets neither is what every caller sends today, and it still answers everything.
	everything, err := s.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{Project: project})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(everything.GetJobs()) != 4 {
		t.Fatalf("a listing that narrows by nothing holds %d jobs, want all 4", len(everything.GetJobs()))
	}
}

// finishedJob writes a job that ended, with the moment it was declared and the moment it finished
// apart, so a read by one of them can be told from a read by the other.
func finishedJob(t *testing.T, kept store.Store, workspace, project, title string,
	declared, ended time.Time) string {
	t.Helper()
	one := &job.Job{
		ID: store.NewID(), Workspace: workspace, Project: project,
		Title: title, Brief: "open it", Version: 1, Phase: job.PhaseDone,
		CreatedAt: declared, UpdatedAt: declared, FinishedAt: &ended,
	}
	if err := kept.CreateJob(context.Background(), one, declaredRecord(one)); err != nil {
		t.Fatalf("CreateJob %q: %v", title, err)
	}
	return one.ID
}

func declaredRecord(one *job.Job) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventDeclared, Job: one.ID,
		Workspace: one.Workspace, Project: one.Project, Detail: one.Title,
		OccurredAt: time.Now().UTC(),
	}
}
