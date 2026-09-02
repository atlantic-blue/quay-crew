package storetest

import (
	"context"
	"errors"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// runJobIdeationConformance holds both stores to what a reading and its answer mean.
//
// The same pair of compare and set movements the plan is held to, one stage earlier, and neither
// store may be the lenient one. A store that let a job put its reading up while it was not running
// would put questions from a session nobody is paying for. A store that took a second answer would
// have the row change under a plan already being written from the first, and a reader could no
// longer say which words the plan came from.
func runJobIdeationConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
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

	t.Run("a running job puts its reading up, and the row carries both halves", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "the transcript page")
		if _, err := s.StartJob(ctx, id, aLease("controller-1"),
			[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}

		asking, err := s.ProposeJobIdeation(ctx, id, understood, question,
			askedEvent(id, workspace, project, question))
		if err != nil {
			t.Fatalf("ProposeJobIdeation: %v", err)
		}
		if asking.Phase != job.PhaseAsking || asking.Ideation != understood ||
			asking.Question != question {
			t.Fatalf("the row is %q with a reading of %q and the question %q",
				asking.Phase, asking.Ideation, asking.Question)
		}
		if asking.IdeationAnswer != "" {
			t.Fatalf("the reading reads as answered by %q, and nobody answered it",
				asking.IdeationAnswer)
		}
		// Nobody holds it, for the reason nobody holds any asking job: nothing comes back until a
		// person answers.
		if asking.LeaseOwner != "" || asking.LeaseUntil != nil {
			t.Fatalf("a job waiting to be answered is still held by %q", asking.LeaseOwner)
		}
		// And it reads back the same, which is what says the columns are written rather than kept in
		// whatever the call happened to answer with. This is the check the memory store cannot fail
		// and the real engine can: a column written and never read reads back empty here.
		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if kept.Ideation != understood || kept.IdeationAnswer != "" {
			t.Fatalf("the reading reads back as %q, answered %q", kept.Ideation, kept.IdeationAnswer)
		}
	})

	t.Run("a job that is not running puts up nothing", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "the transcript page")

		_, err := s.ProposeJobIdeation(ctx, id, understood, question,
			askedEvent(id, workspace, project, question))
		if !errors.Is(err, job.ErrNotRunning) {
			t.Fatalf("a pending job put its reading up: %v", err)
		}
		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if kept.Ideation != "" || kept.Phase != job.PhasePending {
			t.Fatalf("the row moved to %q carrying %q", kept.Phase, kept.Ideation)
		}
	})

	t.Run("an answer is kept whole, and a second answer changes nothing", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingToBeAnswered(t, s, workspace, project, understood, question)

		answered, err := s.AnswerJobIdeation(ctx, id, answer, toldEvent(id, workspace, project))
		if err != nil {
			t.Fatalf("AnswerJobIdeation: %v", err)
		}
		if answered.Phase != job.PhasePending || answered.IdeationAnswer != answer {
			t.Fatalf("the answered job is %q, answered %q", answered.Phase, answered.IdeationAnswer)
		}
		// What it was told is cleared, the way an approval clears it. The answer is not an instruction
		// to carry on with work: it is the material the plan is written from, and it reaches the
		// session inside the task that asks for the plan.
		if answered.Told != "" {
			t.Fatalf("an answered job carries %q as the thing it was told", answered.Told)
		}
		if answered.Ideation != understood {
			t.Fatalf("the reading changed to %q when it was answered", answered.Ideation)
		}
		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if kept.IdeationAnswer != answer {
			t.Fatalf("the answer reads back as %q", kept.IdeationAnswer)
		}

		// A second answer leaves the first one. By then the job is planning from those words, and a
		// record that changed under the plan would leave a reader unable to say which words it came
		// from.
		if _, err := s.AnswerJobIdeation(ctx, id, "2: something else entirely",
			toldEvent(id, workspace, project)); !errors.Is(err, job.ErrNotAsking) {
			t.Fatalf("a reading was answered twice: %v", err)
		}
		again, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if again.IdeationAnswer != answer {
			t.Fatalf("the second answer replaced the first: %q", again.IdeationAnswer)
		}
	})

	t.Run("a job nobody asked answers nothing", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "the transcript page")

		_, err := s.AnswerJobIdeation(ctx, id, answer, toldEvent(id, workspace, project))
		if !errors.Is(err, job.ErrNotAsking) {
			t.Fatalf("a pending job took an answer: %v", err)
		}
	})
}

// waitingToBeAnswered is a job that has said what it understood and is waiting for a person.
func waitingToBeAnswered(t *testing.T, s store.Store, workspace, project, understood,
	question string) string {
	t.Helper()
	ctx := context.Background()
	id := declaredJob(t, s, workspace, project, "the transcript page")
	if _, err := s.StartJob(ctx, id, aLease("controller-1"),
		[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	if _, err := s.ProposeJobIdeation(ctx, id, understood, question,
		askedEvent(id, workspace, project, question)); err != nil {
		t.Fatalf("ProposeJobIdeation: %v", err)
	}
	return id
}
