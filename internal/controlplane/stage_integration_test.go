//go:build integration

package controlplane_test

import (
	"context"
	"fmt"
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
// The stage is read from what the row carries: the sentence, the parent, the answer to what the job
// understood, the accepted list, the record of the failing tests, the plan and its approval. The memory store holds a job.Job in a map and
// cannot say whether those four reach the row, come back out of it, and cross the control plane. A
// store that missed one of them in jobColumns or in scanJob would leave every surface reading a job
// in the wrong stage while the whole unit tier stayed green.
func TestTheStageIsReadOffTheWireThroughPostgres(t *testing.T) {
	// Four stages, each of them several passes of the controller against a real engine, so the whole
	// walk needs more room than one stage does.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	durable := aRealStore(t, ctx)
	// The double is held rather than made inline, because what it says has to change once: a session
	// asked for a plan and answering anything else is asked again and then stopped, which is the plan
	// gate working rather than this test failing.
	runner := &model.FakeRunner{Reply: "i said what i understood"}
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
	if !moved.Built || moved.Doing != "" {
		t.Fatalf("design reads as unbuilt, or says where a job stands in it: %q", moved.Doing)
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
	if !accepted.Built || accepted.Doing != "" {
		t.Fatalf("test reads as unbuilt, or says where a job stands in it: %q", accepted.Doing)
	}

	// Then the failing tests those requirements become, and the same trap once more: the record is a
	// column of its own, and one written and never selected reads back empty, which would hold the job
	// in test for ever while every unit test stayed green.
	tickUntilTheTests(t, ctx, server, client, id)
	red := stageOnTheWire(t, ctx, client, id)
	if red.Name != job.StageBuild {
		t.Fatalf("a job whose suite is red reads off the wire as stage %q, want build", red.Name)
	}
	if red.Closed != "test closed on a failing test for every requirement on that list" {
		t.Fatalf("it says test was closed by %q", red.Closed)
	}
	if !red.Built {
		t.Fatalf("build reads as a stage that is not built, and it is built")
	}
	// And what it says the job is doing is true of this job: its suite went red a moment ago, so it has
	// no plan, and nobody has approved anything.
	if !strings.Contains(red.Doing, "writes the plan") {
		t.Fatalf("a job with no plan is told %q", red.Doing)
	}
	// The record itself crosses the wire whole, with every failure under the requirement it came from.
	kept := readJob(t, ctx, client, id)
	requirements, failing := job.TestsOn(kept.GetTests())
	if requirements == 0 || failing < requirements {
		t.Fatalf("the record off the wire covers %d requirements with %d failing tests: %q",
			requirements, failing, kept.GetTests())
	}

	// Then the plan those tests are turned green by, which a person approves before anything is built.
	// The double says a plan from here on, because a session asked for one and answering anything else
	// is asked again and then stopped.
	runner.Reply = "Step 1: build the vertical on the command line\nStep 2: build the one in a browser"
	tickUntilThePhase(t, ctx, server, client, id, job.PhaseAsking)
	if planned := readJob(t, ctx, client, id); planned.GetPlan() == "" {
		t.Fatalf("the job is asking and carries no plan, so there is nothing to approve")
	}
	if _, err := client.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{Id: id, Answer: "yes"}); err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}

	// And the build, which is the last stage: one worker for each vertical, all at once, none of them
	// able to change a test. The same trap once more, and it is the one the whole slice turns on: the
	// record is a column of its own, and one written and never selected would fan the same verticals
	// out again on every tick while every unit test stayed green.
	tickUntilTheBuild(t, ctx, server, client, id)
	built := stageOnTheWire(t, ctx, client, id)
	if built.Name != job.StageBuild {
		t.Fatalf("a built job reads off the wire as stage %q, want build", built.Name)
	}
	// And it says so in the words a person acts on: what they are being asked to look at, how many
	// pictures there are, and what they are being asked to say about them. A line that holds for a
	// person without telling them any of that is the failure this asserts phrase by phrase.
	for _, ask := range []string{"waits for you to look at", "2 pictures of them running",
		"say whether the value arrived"} {
		if !strings.Contains(built.Doing, ask) {
			t.Fatalf("a built job is told %q, which does not say %q", built.Doing, ask)
		}
	}

	// It holds for a person rather than calling itself done, and the question names what they are
	// being asked to look at.
	whole := readJob(t, ctx, client, id)
	if whole.GetPhase() != job.PhaseAsking {
		t.Fatalf("a built job is %q, want asking so a person accepts it", whole.GetPhase())
	}
	if !strings.Contains(whole.GetQuestion(), "say whether the value arrived") {
		t.Fatalf("the question a built job asks is %q", whole.GetQuestion())
	}
	verticals, passing := job.BuiltOn(whole.GetBuild())
	if verticals == 0 || passing < verticals {
		t.Fatalf("the record off the wire covers %d verticals with %d passing tests: %q",
			verticals, passing, whole.GetBuild())
	}

	// And every run of that fan out reads back in the build stage, which is what the dispatch reads to
	// tell the session's runtime. A run read back in any other stage would build under no gate at all,
	// and the suite would agree with whatever the build wrote.
	runs, listErr := client.ListExecutions(ctx,
		&quaycrewv1.ListExecutionsRequest{Job: id, Stage: job.StageBuild})
	if listErr != nil {
		t.Fatalf("ListExecutions: %v", listErr)
	}
	builders := 0
	for _, run := range runs.GetExecutions() {
		builders++
		if run.GetStage() != job.StageBuild {
			t.Fatalf("the run of vertical %d reads back in stage %q, so it built outside the boundary",
				run.GetNumber(), run.GetStage())
		}
	}
	if builders != verticals {
		t.Fatalf("%d runs built %d verticals", builders, verticals)
	}

	// And no row nobody declared stands in the listing of declared work, which is the whole of the
	// split: a job with five runs under it used to be six rows on the screen.
	onTheBoard, listErr := client.ListJobs(ctx,
		&quaycrewv1.ListJobsRequest{Project: project.GetProject().GetId()})
	if listErr != nil {
		t.Fatalf("ListJobs: %v", listErr)
	}
	for _, one := range onTheBoard.GetJobs() {
		if one.GetId() != id {
			t.Fatalf("the jobs listing carries %q, %q, which nobody declared", one.GetId(), one.GetTitle())
		}
	}

	// Every vertical arrives with a picture of it running and a label saying where the picture came
	// from, and both cross the wire inside the record. A person answering from a terminal has nothing
	// to look at without them.
	shots := job.PicturesIn(whole.GetBuild())
	if len(shots) != verticals {
		t.Fatalf("%d verticals were built and %d pictures came off the wire", verticals, len(shots))
	}
	for _, shot := range shots {
		if err := shot.Shows(); err != nil {
			t.Fatalf("the picture of vertical %d does not show it working: %v", shot.Vertical, err)
		}
	}

	// The gate itself, over a real engine. Ticking changes nothing while the question stands: an
	// acceptance that never comes has to leave the job exactly where it is, or the gate is decoration.
	for i := 0; i < 3; i++ {
		server.TickJob(ctx)
	}
	waiting := readJob(t, ctx, client, id)
	if waiting.GetPhase() != job.PhaseAsking || waiting.GetAccepted() {
		t.Fatalf("three ticks with nobody answering left the job %q, accepted %v",
			waiting.GetPhase(), waiting.GetAccepted())
	}

	// Then the person's word, which is the only thing that lands it. This is the trap one last time
	// and the one this slice turns on: accepted is a column, and one written and never selected reads
	// back false, so the job would stop on its own acceptance gate for ever while every unit test
	// stayed green.
	if _, err := client.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{Id: id, Answer: "yes"}); err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}
	tickUntilItIsAccepted(t, ctx, server, client, id)

	// Their word is permission rather than an ending, so what is left is the ending every job has: the
	// pull request the work is read in, and an account of the plan a person approved. The model double
	// answers a task, it never records a step, so the account is written here the way the session is
	// told to write it. Without it the job stops on the plan gate, which is a different gate from the
	// one this test is about, and a test that cannot tell those two apart proves neither.
	accountForThePlan(t, ctx, server, client, id)
	tickUntilThePhase(t, ctx, server, client, id, job.PhaseDone)

	done := readJob(t, ctx, client, id)
	if !done.GetAccepted() {
		t.Fatal("the job reads back unaccepted off the wire, so the column is written and never read")
	}
	if done.GetOutcome() != job.OutcomeProved {
		t.Fatalf("an accepted job settled %q, want proved", done.GetOutcome())
	}
	// The record of what was built, and its pictures, are still there. That is what the person
	// accepted, and it is what anybody reading this job afterwards has to be able to see.
	if len(job.PicturesIn(done.GetBuild())) != verticals {
		t.Fatalf("landing the job lost the pictures: %q", done.GetBuild())
	}
	if ended := stageOnTheWire(t, ctx, client, id); !strings.Contains(ended.Doing, "value arrived") {
		t.Fatalf("an accepted job is told %q", ended.Doing)
	}
}

// tickUntilTheBuild drives the controller until every vertical of the accepted list is built and the
// record is on the row.
//
// The stage runs one job for each vertical, and each of those is started, dispatched and landed on
// its own passes, so the number of ticks it takes is the machine's to decide rather than this test's.
func tickUntilTheBuild(t *testing.T, ctx context.Context, server *controlplane.Server,
	client quaycrewv1.ControlPlaneServiceClient, id string) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		server.TickJob(ctx)
		one := readJob(t, ctx, client, id)
		if one.GetBuild() != "" {
			return
		}
		// Asking with no record is the stage stopping for a person, which is a failure of this run
		// rather than the hold it ends on: the hold carries the record.
		if one.GetPhase() == job.PhaseAsking {
			t.Fatalf("the job stopped in the build stage: %s", one.GetQuestion())
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s never built its verticals", id)
}

// tickUntilTheTests drives the controller until every requirement of the accepted list has a failing
// test and the record is on the row.
//
// The stage runs one job for each requirement, and each of those is started, dispatched and landed on
// its own passes, so the number of ticks it takes is the machine's to decide rather than this test's.
func tickUntilTheTests(t *testing.T, ctx context.Context, server *controlplane.Server,
	client quaycrewv1.ControlPlaneServiceClient, id string) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		server.TickJob(ctx)
		one := readJob(t, ctx, client, id)
		if one.GetTests() != "" {
			return
		}
		if one.GetPhase() == job.PhaseAsking {
			t.Fatalf("the job stopped in the test stage: %s", one.GetQuestion())
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s never wrote the failing tests for its requirements", id)
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
		Tests: one.GetTests(), Build: one.GetBuild(), Accepted: one.GetAccepted(),
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

// tickUntilItIsAccepted drives the controller until a person's word is on the row.
//
// It is its own wait rather than part of the one below, because the acceptance and the ending are two
// movements and only the first of them is what this test is about. A run that never gets here fails
// saying the word never landed, which is the column trap, and a run that gets past here and stops
// fails saying something else, which is not.
func tickUntilItIsAccepted(t *testing.T, ctx context.Context, server *controlplane.Server,
	client quaycrewv1.ControlPlaneServiceClient, id string) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		server.TickJob(ctx)
		if readJob(t, ctx, client, id).GetAccepted() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	one := readJob(t, ctx, client, id)
	t.Fatalf("a person answered %q and job %s reads accepted %v, it is %s: %s",
		"yes", id, one.GetAccepted(), one.GetPhase(), one.GetReason())
}

// accountForThePlan records one step against each step of the plan, while the job's own session runs.
//
// The session is told to do this, in the same words: a person approved this plan and the job does not
// end until every step of it has a record. A real session runs krewe job step as it goes, and the
// model double cannot, so the test writes what the session would write. It waits for the job to run
// first, because a step is something a running job's session finished and the store refuses it from
// any other phase.
func accountForThePlan(t *testing.T, ctx context.Context, server *controlplane.Server,
	client quaycrewv1.ControlPlaneServiceClient, id string) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		server.TickJob(ctx)
		one := readJob(t, ctx, client, id)
		if one.GetPhase() != job.PhaseRunning {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		steps := job.PlanIn(one.GetPlan())
		if len(steps) == 0 {
			t.Fatalf("the plan a person approved reads as no steps at all: %q", one.GetPlan())
		}
		for _, step := range steps {
			if _, err := server.RecordJobStep(asJobCredential(ctx, id),
				&quaycrewv1.RecordJobStepRequest{
					Summary: fmt.Sprintf("%d: %s", step.Number, step.Text),
				}); err != nil {
				t.Fatalf("RecordJobStep(%d): %v", step.Number, err)
			}
		}
		return
	}
	t.Fatalf("job %s never ran the session that ends it, it is %s", id,
		readJob(t, ctx, client, id).GetPhase())
}
