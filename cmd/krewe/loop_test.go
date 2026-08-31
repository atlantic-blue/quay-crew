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

// What an operator sees about a job that went in circles.
//
// A job that was handed to another role is running again and carries no reason at all, so without a
// line of its own the reading says nothing: three attempts happened, and the record of them would be
// two tables away.

// theSameThingEveryTime is a model that fails with one sentence however often it is asked, which is
// what a session that cannot get a check green looks like from outside.
const theSameThingEveryTime = "the coverage check is still red, so I will try the same fix once more"

// aJobThatWentInCircles is a system holding one job that made three attempts nothing could tell
// apart, put back by the operator between them the way a failure is answered today.
func aJobThatWentInCircles(t *testing.T) (quaycrewv1.ControlPlaneServiceClient, string) {
	t.Helper()
	srv := controlplane.NewServer(controlplane.Config{
		Store:    store.NewMemory(),
		Runner:   &model.FakeRunner{Err: errors.New(theSameThingEveryTime)},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	client := testClientFor(t, srv)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "job", "create", "--title", "get the coverage check green",
		"--brief", "make the coverage gate pass")

	listed, err := client.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{})
	if err != nil || len(listed.GetJobs()) != 1 {
		t.Fatalf("the system holds %d jobs (%v), want the one just declared", len(listed.GetJobs()), err)
	}
	id := listed.GetJobs()[0].GetId()

	for attempt := range job.LoopAttempts {
		if attempt > 0 {
			mustRun(t, client, "job", "resume", id)
		}
		waitForPhase(t, srv, client, id, job.PhaseFailed, job.PhaseAsking)
	}
	waitForPhase(t, srv, client, id, job.PhaseAsking)
	return client, id
}

// waitForPhase ticks until the job reaches one of these phases, because the controller sends the
// task and writes what came of it on a later pass.
func waitForPhase(t *testing.T, srv *controlplane.Server, client quaycrewv1.ControlPlaneServiceClient,
	id string, phases ...string) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		srv.TickJob(ctx)
		found, err := client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: id})
		if err == nil {
			for _, phase := range phases {
				if found.GetJob().GetPhase() == phase {
					return
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the job never reached %s", strings.Join(phases, " or "))
}

func TestReadingAJobThatWentInCirclesSaysTheStepAndWhatItEscalatedTo(t *testing.T) {
	client, id := aJobThatWentInCircles(t)

	shown := mustRun(t, client, "job", "show", id)

	for _, want := range []string{
		"went in circles on step 1", "put to the operator",
		// And what the attempts said, because a similarity on its own is a number nobody can act on.
		theSameThingEveryTime, "attempt 3",
	} {
		if !strings.Contains(shown, want) {
			t.Errorf("krewe job show says:\n%s\nwant it to say %q", shown, want)
		}
	}
	// Once. A job waiting to be told something carries the attempts inside its question, and a
	// reading that prints them again above it is the reading nobody finishes.
	if said := strings.Count(shown, "attempt 3 ("); said != 1 {
		t.Errorf("krewe job show writes the third attempt out %d times:\n%s", said, shown)
	}
}

// A job nothing has stopped says nothing about circles, or every reading would carry a line about a
// thing that did not happen.
func TestReadingAnOrdinaryJobSaysNothingAboutCircles(t *testing.T) {
	srv := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "the bill is due on the 14th"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	client := testClientFor(t, srv)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "job", "create", "--title", "read the electricity bill", "--brief", "open it")
	listed, _ := client.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{})
	id := listed.GetJobs()[0].GetId()
	waitForPhase(t, srv, client, id, job.PhaseDone)

	shown := mustRun(t, client, "job", "show", id)

	if strings.Contains(shown, "went in circles") {
		t.Errorf("krewe job show says a job that finished went in circles:\n%s", shown)
	}
}

// The refusal reaches the operator through the tool, in the words the system refuses it with.
func TestDeclaringAJobThatEscalatesOntoAnotherModelIsRefused(t *testing.T) {
	srv := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "done"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	client := testClientFor(t, srv)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	printed, err := asked(t, client, "job", "create", "--title", "get the check green",
		"--brief", "make it pass", "--escalate", "model:opus")

	if err == nil {
		t.Fatalf("a job escalating onto another model was declared: %q", printed)
	}
	if !strings.Contains(err.Error(), "role:<name>") {
		t.Fatalf("the refusal says %q, want it to name what to write instead", err)
	}
}
