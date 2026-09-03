package storetest

import (
	"context"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// runJobStageConformance holds both stores to the stage a job reads as.
//
// The stage is read off the row rather than written on it, so what has to be the same in both tiers
// is the material it is read from: the sentence, the parent, the answer to what the job understood,
// the plan and its approval. A store that writes one of those and does not select it, or selects
// it and does not scan it, reads back a job in the wrong stage while every reading of the same rows
// in memory is right. That is the shape this suite exists for, and it is why the stage is read here
// through the ordinary read rather than off the value the write answered with.
func runJobStageConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	const understood = "Understood: a page that takes a link and gives back the text\n" +
		"Not: a page that takes an identifier\n" +
		"Told: the person pastes a link\n" +
		"Assumed: the transcript is already stored\n" +
		"Unknown: which surface this is read on\n" +
		"Confidence: fairly sure of the shape\n" +
		"Question 1: which surface does a person read this on"
	const question = "Here is what it understands the work to be."
	const answer = "1: on the command line, the way every other listing is read"

	t.Run("a job that has never started reads as ideation, out of the store", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := jobWithASentence(t, s, workspace, project, "")

		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		stage := job.StageOf(kept)
		if stage.Name != job.StageIdeation || stage.Number != 1 {
			t.Fatalf("a declared job reads as stage %q, %d of four: the sentence reads back as %q",
				stage.Name, stage.Number, kept.Product)
		}
		if stage.Closed == "" || stage.Opens == "" {
			t.Fatalf("the stage says %q closed the one before it and %q opens the next",
				stage.Closed, stage.Opens)
		}
	})

	t.Run("an answered reading moves the job to design, out of the store", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := jobWithASentence(t, s, workspace, project, "")
		if _, err := s.StartJob(ctx, id, aLease("controller-1"),
			[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}
		if _, err := s.ProposeJobIdeation(ctx, id, understood, question,
			askedEvent(id, workspace, project, question)); err != nil {
			t.Fatalf("ProposeJobIdeation: %v", err)
		}

		asking, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		// Asking and still in ideation. The pair is the point: the phase says the system is waiting
		// and the stage says what it is waiting for.
		if asking.Phase != job.PhaseAsking || job.StageOf(asking).Name != job.StageIdeation {
			t.Fatalf("a job waiting for its answer is %q in stage %q",
				asking.Phase, job.StageOf(asking).Name)
		}

		if _, err := s.AnswerJobIdeation(ctx, id, answer, toldEvent(id, workspace, project)); err != nil {
			t.Fatalf("AnswerJobIdeation: %v", err)
		}
		answered, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		stage := job.StageOf(answered)
		if stage.Name != job.StageDesign {
			t.Fatalf("a job whose reading a person answered reads as stage %q: the answer reads back "+
				"as %q, which is the column this suite exists to catch", stage.Name, answered.IdeationAnswer)
		}
		if !stage.Built {
			t.Fatalf("design reads as a stage that is not built, and the job is asked for its list there")
		}
	})

	t.Run("an accepted list moves the job to test, out of the store", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingToAcceptTheList(t, s, workspace, project)

		asking, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		// Asking and still in design, which is the pair again: the phase says the system is waiting and
		// the stage says what it is waiting for.
		if asking.Phase != job.PhaseAsking || job.StageOf(asking).Name != job.StageDesign {
			t.Fatalf("a job waiting for its list to be accepted is %q in stage %q",
				asking.Phase, job.StageOf(asking).Name)
		}

		if _, err := s.AcceptJobDesign(ctx, id, toldEvent(id, workspace, project)); err != nil {
			t.Fatalf("AcceptJobDesign: %v", err)
		}
		accepted, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		stage := job.StageOf(accepted)
		if stage.Name != job.StageTest {
			t.Fatalf("a job whose list a person accepted reads as stage %q: the acceptance reads back "+
				"as %t, which is the column this suite exists to catch",
				stage.Name, accepted.DesignAccepted)
		}
		if !stage.Built {
			t.Fatalf("test reads as a stage that is not built, and the job writes its failing tests there")
		}

		// And the record of those failing tests moves it on again, to the stage nobody has written.
		if _, err := s.RecordJobTests(ctx, id, theRedSuite,
			testedEvent(id, workspace, project)); err != nil {
			t.Fatalf("RecordJobTests: %v", err)
		}
		red, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		stage = job.StageOf(red)
		if stage.Name != job.StageBuild {
			t.Fatalf("a job whose suite is red reads as stage %q: the record reads back as %q, which is "+
				"the column this suite exists to catch", stage.Name, red.Tests)
		}
		if !stage.Built || stage.Doing == "" {
			t.Fatalf("build reads as a stage that is not built, saying %q", stage.Doing)
		}
		// And it says where the job stands inside that stage, truthfully. This job went red a moment ago
		// and has written no plan, so a line about building would describe a state it is not in yet.
		if !strings.Contains(stage.Doing, "writes the plan") {
			t.Fatalf("a job with no plan is told %q", stage.Doing)
		}
	})

	t.Run("a job a session declared runs its own stages, out of the store", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		caused := jobWithASentence(t, s, workspace, project, declaredJob(t, s, workspace, project, "the ask"))

		kept, err := s.GetJob(ctx, caused)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if kept.Cause == "" {
			t.Fatal("the job a session declared reads back with nothing that caused it")
		}
		// What caused a job says how it came about and nothing about what it is. It states the
		// sentence, so it runs the four stages like any other job, starting at the first.
		stage := job.StageOf(kept)
		if stage.Outside != "" || stage.Name != job.StageIdeation {
			t.Fatalf("a job a session declared reads as stage %q, outside because %q, want ideation",
				stage.Name, stage.Outside)
		}
	})

	t.Run("a step of a flow run runs no stages of its own, out of the store", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		step := jobShaped(t, s, workspace, project, "one step of a run", func(w *job.Job) {
			w.Product, w.Run = "you paste a link and get the text back", "a-run"
		})

		kept, err := s.GetJob(ctx, step)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if kept.Run != "a-run" {
			t.Fatalf("the step reads back in run %q, want the run that declared it", kept.Run)
		}
		stage := job.StageOf(kept)
		if stage.Name != "" || stage.Outside == "" {
			t.Fatalf("a step of a run reads as being in stage %q of its own", stage.Name)
		}
	})

	t.Run("a job that states no sentence runs no stages, out of the store", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "rotate the signing key")

		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if stage := job.StageOf(kept); stage.Name != "" || stage.Says() != "-" {
			t.Fatalf("an errand reads as being in stage %q", stage.Name)
		}
	})
}

// jobWithASentence is a job that states what a person gets out of it, which is what puts a job into
// the stages at all. A cause records the job whose session declared it, and changes nothing else.
func jobWithASentence(t *testing.T, s store.Store, workspace, project, cause string) string {
	t.Helper()
	declared := &job.Job{
		ID: store.NewID(), Workspace: workspace, Project: project,
		Title: "the transcript page", Brief: "build what the design describes",
		Product: "you paste a link and get the text back",
		Version: 1, Phase: job.PhasePending,
	}
	declared.Cause = cause
	if err := s.CreateJob(context.Background(), declared, declaredEvent(declared)); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	return declared.ID
}
