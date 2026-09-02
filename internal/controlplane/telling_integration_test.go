//go:build integration

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
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
)

// What waits for a person, over a real Postgres.
//
// The unit tier proves this over the memory store, which holds a job.Job in a map and cannot say
// whether the two new moments reach the row and come back. That is the trap issue 614 names: a
// column that misses jobColumns or scanJob reads zero from Postgres while every memory test passes.
func TestWhatWaitsIsReadBackThroughPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	durable := aRealStore(t, ctx)
	// The task is held open, so the question is put while it is under way, which is the only state a
	// session can ask from. A model that answered instantly would race the ask against the landing.
	runner := &model.FakeRunner{
		Reply: "i asked a question and stopped", Gate: make(chan struct{}), Started: make(chan struct{}),
	}
	server := controlplane.NewServer(controlplane.Config{
		Store: durable, Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	client := servedOver(t, server)

	workspace, err := client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	declared, err := client.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project.GetProject().GetId(),
		Title:   "choose where the transcripts are stored", Brief: "read what the project says about cost",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	one := declared.GetJob()

	server.TickJob(ctx)
	select {
	case <-runner.Started:
	case <-ctx.Done():
		t.Fatal("the controller never started the job")
	}
	t.Cleanup(func() {
		close(runner.Gate)
		waiting, doneWaiting := context.WithTimeout(context.Background(), 30*time.Second)
		defer doneWaiting()
		server.WaitForTasks(waiting)
	})

	const question = "aurora serverless version two bills a minimum capacity continuously. Which?"
	asked, err := server.AskJob(
		auth.WithGrant(ctx, auth.Grant{Job: one.GetId()}),
		&quaycrewv1.AskJobRequest{Question: question})
	if err != nil {
		t.Fatalf("AskJob: %v", err)
	}
	if asked.GetJob().GetAskedAt() == nil {
		t.Fatalf("a job that asked carries no moment for the question, so no gap can be measured")
	}

	answer, err := server.GetWaiting(ctx, &quaycrewv1.GetWaitingRequest{Surface: "console"})
	if err != nil {
		t.Fatalf("GetWaiting: %v", err)
	}
	if len(answer.GetWaiting()) != 1 {
		t.Fatalf("%d jobs wait for a person, want the one that asked", len(answer.GetWaiting()))
	}
	told := answer.GetWaiting()[0]
	if told.GetJob() != one.GetId() || told.GetWhy() != job.WaitingAsking {
		t.Fatalf("the telling reads as %s, %q", told.GetJob(), told.GetWhy())
	}
	if !strings.Contains(told.GetWant(), "aurora serverless") {
		t.Fatalf("the telling does not say what the job wants: %q", told.GetWant())
	}

	// Read back off the row through Postgres, which is what proves both columns are written,
	// selected and scanned. A store that missed either would answer this with nothing.
	read, err := client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: one.GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if read.GetJob().GetAskedAt() == nil || read.GetJob().GetRaisedAt() == nil {
		t.Fatalf("the row reads back asked %v, told %v", read.GetJob().GetAskedAt(), read.GetJob().GetRaisedAt())
	}
	if gap := read.GetJob().GetRaisedAt().AsTime().Sub(read.GetJob().GetAskedAt().AsTime()); gap < 0 {
		t.Fatalf("the telling reads as %s older than the question it carried", -gap)
	}

	// A second surface drawing the same wait writes no second record, so the gap stays the time the
	// person spent not knowing rather than the time since the last redraw.
	firstTold := read.GetJob().GetRaisedAt().AsTime()
	time.Sleep(10 * time.Millisecond)
	if _, err := server.GetWaiting(ctx, &quaycrewv1.GetWaitingRequest{Surface: "command line"}); err != nil {
		t.Fatalf("GetWaiting: %v", err)
	}
	again, err := client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: one.GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if !again.GetJob().GetRaisedAt().AsTime().Equal(firstTold) {
		t.Fatalf("a second surface moved the telling from %s to %s",
			firstTold, again.GetJob().GetRaisedAt().AsTime())
	}
	events, err := durable.ListJobEvents(ctx, one.GetId())
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	raised := 0
	for _, event := range events {
		if event.Kind == job.EventRaised {
			raised++
		}
	}
	if raised != 1 {
		t.Fatalf("%d records of the telling for one wait", raised)
	}
}
