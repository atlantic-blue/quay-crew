//go:build integration

package store_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A claim against the database that holds it.
//
// The in memory store serialises everything behind one lock, so it cannot show the case this exists
// for: two callers declaring the same piece of work at the same moment, in two transactions neither
// of which can see the other's row until it commits. Without the lock on the claim both read no
// holder, both write one, and the system has done exactly what it was built to stop while every
// test about claiming passes.

// TestTwoJobsClaimingOneIssueLeaveOneRunningJobAndOneRefusal is the failure from the issue, at the
// speed it actually happens: two sessions reaching for the same issue at once.
func TestTwoJobsClaimingOneIssueLeaveOneRunningJobAndOneRefusalInPostgres(t *testing.T) {
	s, _ := aSystemWithAController(t, &model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declare := func() (*quaycrewv1.CreateJobResponse, error) {
		return s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
			Project: project, Title: "nothing claims a piece of work", Brief: "build the claim",
			Claim: "atlantic-blue/quay-krewe#540",
		})
	}
	var wait sync.WaitGroup
	answers := make([]*quaycrewv1.CreateJobResponse, 2)
	failures := make([]error, 2)
	start := make(chan struct{})
	for i := range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			answers[i], failures[i] = declare()
		}()
	}
	close(start)
	wait.Wait()

	kept, refusals := 0, 0
	holder := ""
	for i := range 2 {
		switch {
		case failures[i] == nil:
			kept++
			holder = answers[i].GetJob().GetId()
		case status.Code(failures[i]) == codes.FailedPrecondition:
			refusals++
		default:
			t.Fatalf("declaring the job failed for another reason: %v", failures[i])
		}
	}
	if kept != 1 || refusals != 1 {
		t.Fatalf("two declarations of one piece of work left %d jobs and %d refusals, want one of each. "+
			"Both were written, so two sessions would build the same slice", kept, refusals)
	}
	for i := range 2 {
		if failures[i] != nil && !strings.Contains(failures[i].Error(), holder) {
			t.Errorf("the refusal does not name the job holding the work: %v", failures[i])
		}
	}

	// One job, and it is the one that runs. The refusal wrote nothing behind it.
	listed, err := s.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Project: project})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(listed.GetJobs()) != 1 {
		t.Fatalf("the system holds %d jobs on one piece of work", len(listed.GetJobs()))
	}
	done := waitForJob(t, s, holder, job.PhaseDone)
	if done.GetClaim() != "atlantic-blue/quay-krewe#540" {
		t.Fatalf("the job that ran claims %q", done.GetClaim())
	}
}

// A claim ends when the job holding it settles, so the next job takes the work. Through the
// controller rather than through a stop, because settling is what a job does on its own.
func TestWorkAFinishedJobClaimedIsClaimedAgainInPostgres(t *testing.T) {
	s, _ := aSystemWithAController(t, &model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	first, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "nothing claims a piece of work", Brief: "build the claim",
		Claim: "atlantic-blue/quay-krewe#540",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	// Refused while it runs.
	if _, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "build it again", Brief: "build the claim",
		Claim: "atlantic-blue/quay-krewe#540",
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a second job on work in flight was answered with %v", err)
	}

	waitForJob(t, s, first.GetJob().GetId(), job.PhaseDone)

	second, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "carry on from there", Brief: "build the rest",
		Claim: "atlantic-blue/quay-krewe#540",
	})
	if err != nil {
		t.Fatalf("work a finished job claimed is still held by it: %v", err)
	}
	if second.GetJob().GetClaim() != "atlantic-blue/quay-krewe#540" {
		t.Fatalf("the second job claims %q", second.GetJob().GetClaim())
	}
}

// The expiry, in the database's own clock rather than in Go's. A claim that never runs out passes
// every test above and deadlocks the system the first time a container dies with its job still
// reading as running.
func TestAClaimOnAJobNothingHasMovedRunsOutInPostgres(t *testing.T) {
	s, _ := aSystemOnPostgres(t)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	stuck, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "nothing claims a piece of work", Brief: "build the claim",
		Claim: "atlantic-blue/quay-krewe#540",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	// The row is left running and nothing moves it again, which is what a session that died leaves
	// behind. The clock is moved rather than the test waiting two hours for it.
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open a pool: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `
		update jobs set phase = $2, updated_at = now() - make_interval(secs => $3)
		where id = $1`, stuck.GetJob().GetId(), job.PhaseRunning, (job.ClaimLife + time.Hour).Seconds()); err != nil {
		t.Fatalf("age the row: %v", err)
	}

	next, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "pick the work back up", Brief: "build the claim",
		Claim: "atlantic-blue/quay-krewe#540",
	})
	if err != nil {
		t.Fatalf("a claim on a job nothing has moved for %s still blocks the work: %v", job.ClaimLife, err)
	}
	if next.GetJob().GetClaim() != "atlantic-blue/quay-krewe#540" {
		t.Fatalf("the job that picked the work up claims %q", next.GetJob().GetClaim())
	}
}
