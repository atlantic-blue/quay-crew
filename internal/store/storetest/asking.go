package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// runJobAskingConformance holds both stores to what a question means.
//
// A question is a phase on the row rather than a message in flight, so what has to hold is the pair
// of compare and sets around it: a job asks only from running, and an answer applies only to a job
// that is asking. Neither store is allowed to be the lenient one, because the lenient one would let
// a second answer send a second task and pay for the same work twice.
func runJobAskingConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a running job asks, and nothing but an answer moves it", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "choose the store")
		if _, err := s.StartJob(ctx, id, aLease("controller-1"), []*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}

		question := "aurora serverless version two bills a minimum capacity continuously, and a key value store on demand bills nothing at rest. Which?"
		asking, err := s.AskJob(ctx, id, question, askedEvent(id, workspace, project, question))
		if err != nil {
			t.Fatalf("AskJob: %v", err)
		}
		if asking.Phase != job.PhaseAsking {
			t.Fatalf("a job that asked is %q, want %q", asking.Phase, job.PhaseAsking)
		}
		if asking.Question != question {
			t.Fatalf("the question reads back as %q", asking.Question)
		}
		// Nobody is holding an asking job: there is nothing to come back for until a person answers,
		// and a hold left on it would read as a controller still working.
		if asking.LeaseOwner != "" || asking.LeaseUntil != nil {
			t.Fatalf("an asking job is still held by %q until %v", asking.LeaseOwner, asking.LeaseUntil)
		}
		// Not runnable and not held, so no controller starts it again and no controller writes an
		// answer onto it. That is the whole of what makes the question a gate.
		runnable, err := s.RunnableJob(ctx, 10)
		if err != nil {
			t.Fatalf("RunnableJob: %v", err)
		}
		if len(runnable) != 0 {
			t.Fatalf("an asking job is offered to be started: %d runnable", len(runnable))
		}
		held, err := s.HeldJob(ctx, "controller-1", 10)
		if err != nil {
			t.Fatalf("HeldJob: %v", err)
		}
		if len(held) != 0 {
			t.Fatalf("an asking job is still one the controller holds: %d held", len(held))
		}
	})

	t.Run("an answer puts the job back to pending, carrying what it was told", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := asking(t, s, workspace, project, "which store?")

		answered, err := s.AnswerJob(ctx, id, "the key value store, on demand", toldEvent(id, workspace, project))
		if err != nil {
			t.Fatalf("AnswerJob: %v", err)
		}
		if answered.Phase != job.PhasePending {
			t.Fatalf("an answered job is %q, want %q", answered.Phase, job.PhasePending)
		}
		if answered.Told != "the key value store, on demand" {
			t.Fatalf("what it was told reads back as %q", answered.Told)
		}
		// The question stays, because the answer means nothing without it and the session is about to
		// be handed both.
		if answered.Question != "which store?" {
			t.Fatalf("the question was lost on the answer: %q", answered.Question)
		}
		runnable, err := s.RunnableJob(ctx, 10)
		if err != nil {
			t.Fatalf("RunnableJob: %v", err)
		}
		if len(runnable) != 1 || runnable[0].ID != id {
			t.Fatalf("an answered job is not offered to be started again: %d runnable", len(runnable))
		}
	})

	// Two people answering at once must leave one answer and one task. The second answer is refused
	// rather than written, because a job that starts twice is a job paid for twice.
	t.Run("a question is answered once, and the second answer is refused", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := asking(t, s, workspace, project, "which store?")

		if _, err := s.AnswerJob(ctx, id, "the first answer", toldEvent(id, workspace, project)); err != nil {
			t.Fatalf("AnswerJob: %v", err)
		}
		_, err := s.AnswerJob(ctx, id, "the second answer", toldEvent(id, workspace, project))
		if !errors.Is(err, job.ErrNotAsking) {
			t.Fatalf("the second answer was taken: %v", err)
		}
		found, _ := s.GetJob(ctx, id)
		if found.Told != "the first answer" {
			t.Fatalf("the second answer overwrote the first: %q", found.Told)
		}
	})

	t.Run("a job that is not running cannot ask", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "choose the store")

		_, err := s.AskJob(ctx, id, "which store?", askedEvent(id, workspace, project, "which store?"))
		if !errors.Is(err, job.ErrNotRunning) {
			t.Fatalf("a pending job asked a question: %v", err)
		}
		found, _ := s.GetJob(ctx, id)
		if found.Phase != job.PhasePending || found.Question != "" {
			t.Fatalf("the refused question was written anyway: %s, %q", found.Phase, found.Question)
		}
	})

	t.Run("a job nobody asked about takes no answer", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "choose the store")

		_, err := s.AnswerJob(ctx, id, "the key value store", toldEvent(id, workspace, project))
		if !errors.Is(err, job.ErrNotAsking) {
			t.Fatalf("a job nobody asked about took an answer: %v", err)
		}
	})

	// Asking again is asking about a different decision, so the answer to the last one goes. A row
	// carrying one question and the answer to a previous one is a row a reader cannot use.
	t.Run("asking again clears what the job was told", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := asking(t, s, workspace, project, "which store?")
		if _, err := s.AnswerJob(ctx, id, "the key value store", toldEvent(id, workspace, project)); err != nil {
			t.Fatalf("AnswerJob: %v", err)
		}
		if _, err := s.StartJob(ctx, id, aLease("controller-1"), []*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}
		if _, err := s.AskJob(ctx, id, "which region?", askedEvent(id, workspace, project, "which region?")); err != nil {
			t.Fatalf("AskJob: %v", err)
		}

		found, _ := s.GetJob(ctx, id)
		if found.Question != "which region?" {
			t.Fatalf("the second question reads back as %q", found.Question)
		}
		if found.Told != "" {
			t.Fatalf("the job still carries the answer to the previous question: %q", found.Told)
		}
	})

	// Both movements write their record in the same transaction as the row, so the history and the
	// row cannot disagree about whether a question was ever asked.
	t.Run("the record of a question and of the answer is written with the row", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := asking(t, s, workspace, project, "which store?")
		if _, err := s.AnswerJob(ctx, id, "the key value store", toldEvent(id, workspace, project)); err != nil {
			t.Fatalf("AnswerJob: %v", err)
		}

		events, err := s.ListJobEvents(ctx, id)
		if err != nil {
			t.Fatalf("ListJobEvents: %v", err)
		}
		kinds := eventKindsOf(events)
		want := []string{job.EventDeclared, job.EventStarted, job.EventAsked, job.EventTold}
		if len(kinds) != len(want) {
			t.Fatalf("the records read %v, want %v", kinds, want)
		}
		for i, kind := range want {
			if kinds[i] != kind {
				t.Fatalf("the records read %v, want %v", kinds, want)
			}
		}
	})
}

// asking is a job that has started and put one question to a person.
func asking(t *testing.T, s store.Store, workspace, project, question string) string {
	t.Helper()
	ctx := context.Background()
	id := declaredJob(t, s, workspace, project, "choose the store")
	if _, err := s.StartJob(ctx, id, aLease("controller-1"), []*job.Event{startedEvent(id, workspace, project)}); err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	if _, err := s.AskJob(ctx, id, question, askedEvent(id, workspace, project, question)); err != nil {
		t.Fatalf("AskJob: %v", err)
	}
	return id
}

func askedEvent(id, workspace, project, question string) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventAsked, Job: id,
		Workspace: workspace, Project: project, Detail: question, OccurredAt: time.Now().UTC(),
	}
}

func toldEvent(id, workspace, project string) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventTold, Job: id,
		Workspace: workspace, Project: project, Detail: "answered by the operator", OccurredAt: time.Now().UTC(),
	}
}
