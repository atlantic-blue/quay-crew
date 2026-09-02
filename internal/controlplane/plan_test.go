package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// A job that states the sentence writes its plan, a person approves it, and the work is held to it.
// Driven through the control plane's own calls, and asserted on what the session was actually given:
// a test that stops at the row has proved half a feature, because the half that decides whether this
// works is what the session is handed next.

// theSentence is what a person said they wanted, and thePlan is what the crew answers with when it
// is asked to plan for it.
const (
	theSentence = "you paste a link and get the text back"
	thePlan     = "Right, here is what I will do.\n\nStep 1: read the design\n" +
		"Step 2: build the address that takes a link"
)

// planning is a system with one planned job in it, and the model it answers with.
type planning struct {
	server *controlplane.Server
	runner *model.FakeRunner
	job    *quaycrewv1.Job
}

// aPlannedJob declares a job at the top that states the sentence, and runs the controller until the
// job is waiting for a person to approve its plan.
func aPlannedJob(t *testing.T, reply string) planning {
	t.Helper()
	runner := &model.FakeRunner{Reply: reply}
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	_, project := newProject(t, server)
	declared, err := server.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "the transcript page",
		Brief: "build what the design describes", Product: theSentence,
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	system := planning{server: server, runner: runner, job: declared.GetJob()}
	ctx := context.Background()
	// The reading comes first, and a person answers it, because a job that has not said what it
	// understood never reaches the plan. The double answers the reading to the task that asks for one,
	// the way a session that read its task would.
	server.TickJob(ctx)
	system.landed(t)
	server.TickJob(ctx)
	if _, err := server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: theAnswerToTheReading,
	}); err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}
	server.TickJob(ctx)
	system.landed(t)
	// Then the list of what it would build, which a person accepts, because a job whose list nobody
	// accepted never reaches the plan either. The double answers a list to the task that asks for one.
	server.TickJob(ctx)
	if _, err := server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: "yes",
	}); err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}
	server.TickJob(ctx)
	system.landed(t)
	server.TickJob(ctx)
	return system
}

// theAnswerToTheReading is what a person writes about what the job understood: prose, opening with
// the number of the question it answers.
const theAnswerToTheReading = "1: on the command line, the way every other listing is read"

// reading is the job as the system holds it now.
func (p planning) reading(t *testing.T) *quaycrewv1.Job {
	t.Helper()
	found, err := p.server.GetJob(context.Background(), &quaycrewv1.GetJobRequest{Id: p.job.GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	return found.GetJob()
}

// landed waits for the task in flight to end, so the controller's next pass reads an answer rather
// than a task still running.
func (p planning) landed(t *testing.T) {
	t.Helper()
	waitFor(t, func() bool {
		for _, task := range tasksOf(t, p.server, p.job.GetId()) {
			if task.GetStatus() == "running" {
				return false
			}
		}
		return len(tasksOf(t, p.server, p.job.GetId())) > 0
	})
}

// asked is what the system last sent the session doing this job.
func (p planning) asked(t *testing.T) string {
	t.Helper()
	sent := tasksOf(t, p.server, p.job.GetId())
	if len(sent) == 0 {
		t.Fatal("the system sent this job's session nothing")
	}
	return sent[len(sent)-1].GetPrompt()
}

// The gate, end to end. Nothing is built, the plan is on the row, and a person holds one question.
func TestAPlannedJobStopsForAPersonBeforeItBuildsAnything(t *testing.T) {
	system := aPlannedJob(t, thePlan)

	got := system.reading(t)
	if got.GetPhase() != job.PhaseAsking {
		t.Fatalf("the job is %q, want asking: %s", got.GetPhase(), got.GetReason())
	}
	if got.GetPlan() != "Step 1: read the design\nStep 2: build the address that takes a link" {
		t.Fatalf("the plan on the row is %q", got.GetPlan())
	}
	if got.GetPlanApproved() {
		t.Fatal("the plan reads as approved and nobody approved it")
	}
	for _, phrase := range []string{theSentence, "Step 1: read the design"} {
		if !strings.Contains(got.GetQuestion(), phrase) {
			t.Fatalf("the question is %q, want it to say %q", got.GetQuestion(), phrase)
		}
	}
	// Three tasks, which is what the three gates cost: one to say what it understood, one to list what
	// it would build, and one to write the plan from both. The same answer after everything is built
	// costs the job.
	if sent := tasksOf(t, system.server, got.GetId()); len(sent) != 3 {
		t.Fatalf("the session ran %d tasks before the plan was approved, want 3", len(sent))
	}
}

// An answer of no does not end the job. It replaces the plan, and the person who said no writes no
// plan: what they said goes back to the session with the plan it wrote.
func TestAnAnswerOfNoSendsTheCrewBackToWriteThePlanAgain(t *testing.T) {
	system := aPlannedJob(t, thePlan)
	ctx := context.Background()
	correction := "a reader pastes a link, so do not make them find an identifier first"

	if _, err := system.server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: correction,
	}); err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}
	if phase := system.reading(t).GetPhase(); phase != job.PhasePending {
		t.Fatalf("the job is %q after an answer of no, want pending so the crew plans again", phase)
	}
	if system.reading(t).GetPlanApproved() {
		t.Fatal("an answer that was not yes approved the plan")
	}

	system.server.TickJob(ctx)

	// What decides whether this works is what the session is handed, so the assertion is on the task.
	sent := system.asked(t)
	for _, phrase := range []string{"was not approved", correction, "Step 1: read the design", "Do no work yet"} {
		if !strings.Contains(sent, phrase) {
			t.Fatalf("the session was sent %q, want it to say %q", sent, phrase)
		}
	}
}

// Approved, and the work starts in the same conversation, carrying the plan it is held to.
func TestAnAnswerOfYesStartsTheWorkAgainstThePlan(t *testing.T) {
	system := aPlannedJob(t, thePlan)
	ctx := context.Background()

	approved, err := system.server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: "yes",
	})
	if err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}
	if !approved.GetJob().GetPlanApproved() {
		t.Fatal("the plan is not approved after a person answered yes")
	}
	if approved.GetJob().GetPhase() != job.PhasePending {
		t.Fatalf("the approved job is %q, want pending so a controller starts the work",
			approved.GetJob().GetPhase())
	}

	system.runner.Reply = "the page takes a link and gives the text back"
	system.server.TickJob(ctx)

	sent := system.asked(t)
	for _, phrase := range []string{
		"build what the design describes", "Step 1: read the design", "record it with its number",
	} {
		if !strings.Contains(sent, phrase) {
			t.Fatalf("the work task is %q, want it to say %q", sent, phrase)
		}
	}
	if strings.Contains(sent, "Do no work yet") {
		t.Fatalf("the session was told to do no work after its plan was approved: %q", sent)
	}
}

// The test that matters most. A plan approved and then not followed is the same failure as no plan
// at all, one step further along.
func TestAnApprovedPlanTheWorkDidNotFollowStopsTheJob(t *testing.T) {
	system := aPlannedJob(t, thePlan)
	ctx := context.Background()
	if _, err := system.server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: "yes",
	}); err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}
	// Stating its outcome, because a session that read its task states one and an answer without it
	// stops the job before it is ever held against the plan.
	const built = "built the page\n\n" + job.OutcomeMarker + " " + job.OutcomeProved
	system.runner.Reply = built
	system.server.TickJob(ctx)
	system.landed(t)

	// The session accounted for the first step of the plan and nothing else.
	if _, err := system.server.RecordJobStep(asJobCredential(ctx, system.job.GetId()),
		&quaycrewv1.RecordJobStepRequest{Summary: "1: read the design"}); err != nil {
		t.Fatalf("RecordJobStep: %v", err)
	}
	system.server.TickJob(ctx)

	got := system.reading(t)
	if got.GetPhase() != job.PhaseStopped {
		t.Fatalf("the job is %q, want stopped", got.GetPhase())
	}
	for _, phrase := range []string{"step 2", "build the address that takes a link"} {
		if !strings.Contains(got.GetReason(), phrase) {
			t.Fatalf("the reason is %q, want it to name %q", got.GetReason(), phrase)
		}
	}
	if got.GetAnswer() != built {
		t.Fatalf("the work is not on the row: the answer is %q", got.GetAnswer())
	}
}

// The other half. A check that always fires is the same as no check, so a job that followed its plan
// finishes with nothing said.
func TestAnApprovedPlanTheWorkFollowedFinishesInSilence(t *testing.T) {
	system := aPlannedJob(t, thePlan)
	ctx := context.Background()
	if _, err := system.server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: "yes",
	}); err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}
	system.runner.Reply = "the page takes a link and gives the text back"
	system.server.TickJob(ctx)
	system.landed(t)

	for _, said := range []string{"1: read the design", "2: built the address"} {
		if _, err := system.server.RecordJobStep(asJobCredential(ctx, system.job.GetId()),
			&quaycrewv1.RecordJobStepRequest{Summary: said}); err != nil {
			t.Fatalf("RecordJobStep(%q): %v", said, err)
		}
	}
	system.server.TickJob(ctx)

	got := system.reading(t)
	if got.GetPhase() != job.PhaseDone {
		t.Fatalf("the job is %q, want done: %s", got.GetPhase(), got.GetReason())
	}
	if got.GetReason() != "" {
		t.Fatalf("a job that followed its plan carries a reason: %q", got.GetReason())
	}
}

// A job that states no sentence is an errand: no plan, no question, and one task.
func TestAnErrandIsNeverAskedToPlan(t *testing.T) {
	runner := &model.FakeRunner{Reply: "the bill is due on the 14th"}
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	declared := declaredIn(t, server, "read the electricity bill")
	ctx := context.Background()

	server.TickJob(ctx)
	waitFor(t, func() bool {
		sent := tasksOf(t, server, declared.GetId())
		return len(sent) == 1 && sent[0].GetStatus() != "running"
	})
	server.TickJob(ctx)

	found, err := server.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: declared.GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	got := found.GetJob()
	if got.GetPhase() != job.PhaseDone {
		t.Fatalf("the errand is %q, want done: %s", got.GetPhase(), got.GetReason())
	}
	if got.GetPlan() != "" || got.GetQuestion() != "" {
		t.Fatalf("an errand was asked to plan: plan %q, question %q", got.GetPlan(), got.GetQuestion())
	}
}
