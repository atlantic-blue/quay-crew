//go:build integration

package controlplane_test

import (
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
)

// Which stage a job is in, read off the wire, over a real Postgres.
//
// The stage is read from five things the row carries: the sentence, the parent, the answer to what
// the job understood, the plan and its approval. The memory store holds a job.Job in a map and
// cannot say whether those four reach the row, come back out of it, and cross the control plane. A
// store that missed one of them in jobColumns or in scanJob would leave every surface reading a job
// in the wrong stage while the whole unit tier stayed green.
func TestTheStageIsReadOffTheWireThroughPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	durable := aRealStore(t, ctx)
	server := controlplane.NewServer(controlplane.Config{
		Store: durable, Runner: &model.FakeRunner{Reply: "i said what i understood"},
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
		Title:   "the transcript page", Brief: "build what the design describes",
		Product: "you paste a link and get the text back",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	id := declared.GetJob().GetId()

	if stage := stageOnTheWire(t, ctx, client, id); stage.Name != job.StageIdeation {
		t.Fatalf("a declared job reads off the wire as stage %q, want ideation", stage.Name)
	}

	// Through ideation, the way the controller runs it: the session is asked what it understood, it
	// says so, and the job stops for a person. The tick that starts the job and the tick that reads
	// what came back are two passes over the row, so this keeps ticking until the row moves.
	tickUntilThePhase(t, ctx, server, client, id, job.PhaseAsking)

	asking := readJob(t, ctx, client, id)
	if asking.GetIdeation() == "" {
		t.Fatalf("the job is asking and carries no reading, so there is nothing to answer")
	}
	// Asking and still in ideation. That pair is the useful thing to read, and it is the reason the
	// stage sits beside the phase rather than replacing it.
	if stage := job.StageOf(&job.Job{
		Product: asking.GetProduct(), Parent: asking.GetParent(),
		IdeationAnswer: asking.GetIdeationAnswer(),
		Plan:           asking.GetPlan(), PlanApproved: asking.GetPlanApproved(),
	}); stage.Name != job.StageIdeation {
		t.Fatalf("a job waiting for its answer is in stage %q, phase %q", stage.Name, asking.GetPhase())
	}

	if _, err := client.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: id, Answer: "1: on the command line, the way every other listing is read",
	}); err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}

	// And out of Postgres again. This is the read the trap lives in: the answer is written, and a
	// column that is not selected or not scanned reads back empty, which would put the job back in
	// the stage it just left.
	moved := stageOnTheWire(t, ctx, client, id)
	if moved.Name != job.StageDesign {
		t.Fatalf("an answered job reads off the wire as stage %q, want design", moved.Name)
	}
	if moved.Closed != "ideation closed on your answer to what it understood" {
		t.Fatalf("it says ideation was closed by %q", moved.Closed)
	}
	if !moved.Built || moved.Unbuilt != "" {
		t.Fatalf("design reads as unbuilt, saying %q", moved.Unbuilt)
	}

	// Then the list, and the same trap one stage further on: the acceptance is a column too, and a
	// flag that is written and never selected reads back false, which would hold the job in design
	// after a person had moved it out.
	tickUntilThePhase(t, ctx, server, client, id, job.PhaseAsking)
	listed := readJob(t, ctx, client, id)
	if listed.GetDesign() == "" {
		t.Fatalf("the job is asking and carries no list, so there is nothing to accept")
	}
	if _, err := client.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{Id: id, Answer: "yes"}); err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}

	accepted := stageOnTheWire(t, ctx, client, id)
	if accepted.Name != job.StageTest {
		t.Fatalf("a job whose list was accepted reads off the wire as stage %q, want test", accepted.Name)
	}
	if accepted.Closed != "design closed on your acceptance of the list it would build" {
		t.Fatalf("it says design was closed by %q", accepted.Closed)
	}
	if accepted.Built || accepted.Unbuilt == "" {
		t.Fatalf("test reads as built, and test is a later slice")
	}
	// And what it says the job is doing instead is true of this job: its list was accepted a moment
	// ago, so it has no plan, and nobody has approved anything.
	if !strings.Contains(accepted.Unbuilt, "writes its plan next") {
		t.Fatalf("a job with no plan is told %q", accepted.Unbuilt)
	}
}

// stageOnTheWire is the stage one job reads as, off the job the control plane answers with.
func stageOnTheWire(t *testing.T, ctx context.Context,
	client quaycrewv1.ControlPlaneServiceClient, id string) job.Stage {
	t.Helper()
	one := readJob(t, ctx, client, id)
	return job.StageOf(&job.Job{
		Product: one.GetProduct(), Parent: one.GetParent(),
		IdeationAnswer: one.GetIdeationAnswer(),
		Design:         one.GetDesign(), DesignAccepted: one.GetDesignAccepted(),
		Plan: one.GetPlan(), PlanApproved: one.GetPlanApproved(),
	})
}

func readJob(t *testing.T, ctx context.Context, client quaycrewv1.ControlPlaneServiceClient,
	id string) *quaycrewv1.Job {
	t.Helper()
	read, err := client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: id})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	return read.GetJob()
}

// tickUntilThePhase drives the controller until the row reaches a phase.
//
// The controller does one thing per pass: one tick starts the job, the task runs detached, and a
// later tick reads what came back. So a test that ticked a fixed number of times would pass on a
// fast machine and fail on a loaded one.
func tickUntilThePhase(t *testing.T, ctx context.Context, server *controlplane.Server,
	client quaycrewv1.ControlPlaneServiceClient, id, phase string) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		server.TickJob(ctx)
		if readJob(t, ctx, client, id).GetPhase() == phase {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s never reached %s, it is %s", id, phase, readJob(t, ctx, client, id).GetPhase())
}
