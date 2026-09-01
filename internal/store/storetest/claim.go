package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// runJobClaimConformance holds both stores to what a claim means.
//
// The check is a read inside the transaction that writes the row, so this is exactly the shape a
// double gets wrong: a store that reads before it writes, or that reads the wrong set of phases,
// passes every test about taking a claim and lets two jobs hold one piece of work. Neither store is
// allowed to be the lenient one.
func runJobClaimConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	// The refusal first. A store that keeps the column and never reads it passes everything below it.
	t.Run("a second job claiming work another job holds is refused, naming the holder", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		first := claimingJob(t, s, workspace, project, "the payments page", "build the payments page")

		second := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "build the payments page again", Brief: "do it", Version: 1,
			Phase: job.PhasePending, Claim: "the payments page",
		}
		err := s.CreateJob(ctx, second, declaredEvent(second))
		var held *job.Held
		if !errors.As(err, &held) {
			t.Fatalf("a second job took work the first holds, and the store answered %v", err)
		}
		if held.Holder != first {
			t.Errorf("the refusal names %q as the holder, want %q", held.Holder, first)
		}
		if held.Title != "build the payments page" {
			t.Errorf("the refusal names the holder's title as %q", held.Title)
		}
		if held.TakenAt.IsZero() {
			t.Error("the refusal says nothing about how old the claim is, so a reader cannot tell a job " +
				"that started a minute ago from one that has held the work all day")
		}
		// Behind the refusal, which is the half a refusal cannot prove about itself.
		listed, err := s.ListJobs(ctx, job.Filter{Project: project})
		if err != nil {
			t.Fatalf("ListJobs: %v", err)
		}
		if len(listed) != 1 {
			t.Fatalf("the store holds %d jobs, want only the one that claimed the work", len(listed))
		}
	})

	// The expiry. Without it the first crashed session holds a piece of work forever, and every test
	// above still passes.
	t.Run("work a job stopped moving on is claimed by the next job", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		long := time.Now().UTC().Add(-job.ClaimLife - time.Hour)
		crashed := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "build the payments page", Brief: "do it", Version: 1,
			Phase: job.PhaseRunning, Claim: "the payments page",
			CreatedAt: long, UpdatedAt: long,
		}
		if err := s.CreateJob(ctx, crashed, declaredEvent(crashed)); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}

		next := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "build the payments page", Brief: "do it", Version: 1,
			Phase: job.PhasePending, Claim: "the payments page",
		}
		if err := s.CreateJob(ctx, next, declaredEvent(next)); err != nil {
			t.Fatalf("a claim on a job nothing has moved for %s still blocks the work: %v", job.ClaimLife, err)
		}
	})

	t.Run("work a settled job claimed is claimed by the next job", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		done := claimingJob(t, s, workspace, project, "the payments page", "build the payments page")
		if _, err := s.StopJob(ctx, done, "the page moved", stoppedEvent(done, workspace, project, "the page moved")); err != nil {
			t.Fatalf("StopJob: %v", err)
		}

		next := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "build the payments page", Brief: "do it", Version: 1,
			Phase: job.PhasePending, Claim: "the payments page",
		}
		if err := s.CreateJob(ctx, next, declaredEvent(next)); err != nil {
			t.Fatalf("work a stopped job claimed is still held by it: %v", err)
		}
	})

	t.Run("two jobs claiming different work are both kept", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		claimingJob(t, s, workspace, project, "the payments page", "build the payments page")

		other := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "build the refunds page", Brief: "do it", Version: 1,
			Phase: job.PhasePending, Claim: "the refunds page",
		}
		if err := s.CreateJob(ctx, other, declaredEvent(other)); err != nil {
			t.Fatalf("a job claiming a different piece of work was refused: %v", err)
		}
		listed, err := s.ListJobs(ctx, job.Filter{Project: project})
		if err != nil {
			t.Fatalf("ListJobs: %v", err)
		}
		if len(listed) != 2 {
			t.Fatalf("the store holds %d jobs, want both", len(listed))
		}
	})

	// A job that claims nothing is every job written before this existed, and most jobs after it.
	t.Run("jobs that claim nothing never block one another", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		declaredJob(t, s, workspace, project, "read the electricity bill")

		second := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "pay the electricity bill", Brief: "do it", Version: 1, Phase: job.PhasePending,
		}
		if err := s.CreateJob(ctx, second, declaredEvent(second)); err != nil {
			t.Fatalf("a job claiming nothing was refused: %v", err)
		}
	})

	// The claim is read back off the row, because a listing is where a person looks before starting
	// work somebody else has.
	t.Run("what a job claims is read back", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := claimingJob(t, s, workspace, project, "atlantic-blue/quay-krewe#540", "build the claim")

		found, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.Claim != "atlantic-blue/quay-krewe#540" {
			t.Fatalf("the job claims %q", found.Claim)
		}
		listed, err := s.ListJobs(ctx, job.Filter{Project: project})
		if err != nil {
			t.Fatalf("ListJobs: %v", err)
		}
		if len(listed) != 1 || listed[0].Claim != "atlantic-blue/quay-krewe#540" {
			t.Fatalf("a listing says nothing about what is claimed, so nobody reads it before starting")
		}
	})
}

// claimingJob writes one job that has taken a piece of work, and answers with its identifier.
func claimingJob(t *testing.T, s store.Store, workspace, project, claim, title string) string {
	t.Helper()
	declared := &job.Job{
		ID: store.NewID(), Workspace: workspace, Project: project,
		Title: title, Brief: "do it", Version: 1, Phase: job.PhasePending, Claim: claim,
	}
	if err := s.CreateJob(context.Background(), declared, declaredEvent(declared)); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	return declared.ID
}
