package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/atlantic-blue/krewe/internal/secrets"
	"github.com/atlantic-blue/krewe/internal/store"
)

// What an operator sees when a job failed: what it failed with, what it finished, and both ways out.

// aFailedJobToRead is a system holding one job whose task died for a reason that was not the work.
//
// It ticks until the row says failed rather than until the task does. The two are a tick apart: the
// controller sends the task and lets go of it, and writes what came of it on a later pass.
func aFailedJobToRead(t *testing.T) (quaycrewv1.ControlPlaneServiceClient, string) {
	t.Helper()
	srv := controlplane.NewServer(controlplane.Config{
		Store:    store.NewMemory(),
		Runner:   &model.FakeRunner{Err: errors.New("the credential ran out")},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	client := testClientFor(t, srv)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "job", "create", "--title", "sort the listing", "--brief", "make the listing sort by the clock")

	listed, err := client.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{})
	if err != nil || len(listed.GetJobs()) != 1 {
		t.Fatalf("the system holds %d jobs (%v), want the one just declared", len(listed.GetJobs()), err)
	}
	id := listed.GetJobs()[0].GetId()

	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		srv.TickJob(ctx)
		found, err := client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: id})
		if err == nil && found.GetJob().GetPhase() == job.PhaseFailed {
			return client, id
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the job never failed, so there is nothing here to continue or refuse")
	return nil, ""
}

// A failure is read by somebody who then has to decide which of the two answers it gets, so the
// reading says both. An operator who has to go and find the commands is an operator who declares the
// job again, which is the thing this exists to stop.
func TestReadingAFailedJobSaysHowToContinueItAndHowToRefuseIt(t *testing.T) {
	client, id := aFailedJobToRead(t)

	shown := mustRun(t, client, "job", "show", id)

	for _, want := range []string{"failed", "krewe job resume", "krewe job refuse"} {
		if !strings.Contains(shown, want) {
			t.Errorf("krewe job show says %q, want it to say %q", shown, want)
		}
	}
}

// Continuing says what the job failed with, because whether that failure was the work being wrong is
// the one thing the person typing this has to judge.
func TestContinuingAJobSaysWhatItFailedWithAndHowToRefuseItInstead(t *testing.T) {
	client, id := aFailedJobToRead(t)

	said := mustRun(t, client, "job", "resume", id)

	for _, want := range []string{"the credential ran out", "krewe job refuse"} {
		if !strings.Contains(said, want) {
			t.Errorf("krewe job resume says %q, want it to say %q", said, want)
		}
	}
	// And the job is going again, which is what the sentence above claims.
	found, err := client.GetJob(context.Background(), &quaycrewv1.GetJobRequest{Id: id})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if found.GetJob().GetPhase() != job.PhasePending {
		t.Fatalf("the job is %q after being continued, want pending", found.GetJob().GetPhase())
	}
}

// Refusing a job the operator has already continued is refused, because it is running again. The
// tool says which phase it is in rather than failing silently.
func TestRefusingAJobThatIsGoingAgainIsRefused(t *testing.T) {
	client, id := aFailedJobToRead(t)
	mustRun(t, client, "job", "resume", id)

	printed, err := asked(t, client, "job", "refuse", id, "the migration was wrong")

	if err == nil {
		t.Fatalf("refusing a job that is going again was accepted: %q", printed)
	}
	if !strings.Contains(err.Error(), "krewe job stop") {
		t.Fatalf("the refusal says %q, want it to name what to type instead", err)
	}
}
