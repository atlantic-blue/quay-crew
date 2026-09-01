package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// runJobLoopConformance holds both stores to what going in circles means.
//
// The record is the part that has to match. A loop is read off the attempts a job has already made,
// so a store that loses an attempt, or keeps one twice, decides differently from the other one about
// whether to stop somebody's work. The same task read twice is the case that matters: whichever
// controller holds a job next reads the task the last one read, and an attempt counted twice would
// manufacture a loop out of one piece of work.
//
// The compare and set is the rest. A loop applies only to a running job, and only for the controller
// holding it, in the same statement, so two controllers cannot both escalate one job.
func runJobLoopConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("what a job's attempts said is on the record, in order, once per task", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		ctx := context.Background()
		id := aRunningJob(t, s)

		landed := job.Landing{
			Phase: job.PhaseFailed, Reason: "the check is still red",
			Attempt: &job.Attempt{
				Task: "task-1", Step: 1, Session: "session-1", Said: "the check is still red",
				Similarity: 0, OccurredAt: time.Now().UTC(),
			},
		}
		if _, err := s.LandJob(ctx, id, landed, stoppedEvent(id, "", "", "failed")); err != nil {
			t.Fatalf("LandJob: %v", err)
		}

		// Reopened, because an attempt that only exists in the caller's process is not something the
		// next controller can hold this job's next attempt against.
		found, err := open(t).GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if len(found.Attempted) != 1 {
			t.Fatalf("the job records %d attempts, want 1", len(found.Attempted))
		}
		kept := found.Attempted[0]
		switch {
		case kept.Task != "task-1":
			t.Fatalf("the attempt reads back as task %q", kept.Task)
		case kept.Seq != 1:
			t.Fatalf("the attempt is numbered %d, want 1", kept.Seq)
		case kept.Step != 1:
			t.Fatalf("the attempt was at step %d, want 1", kept.Step)
		case kept.Said != "the check is still red":
			t.Fatalf("what the attempt said reads back as %q", kept.Said)
		case kept.Session != "session-1":
			t.Fatalf("the attempt was made in %q, want the conversation the job ran in", kept.Session)
		case kept.OccurredAt.IsZero():
			t.Fatal("the attempt says nothing about when it was made")
		}
	})

	// The same task, read again by whichever controller holds the job next. One row, or three
	// readings of one attempt would be a loop.
	t.Run("the same task recorded twice leaves one attempt", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aRunningJob(t, s)

		attempt := &job.Attempt{Task: "task-1", Step: 1, Said: "the check is still red", Similarity: 0.9}
		for _, phase := range []string{job.PhaseFailed, job.PhaseFailed} {
			// Put back to running between the two, which is what taking a job over does.
			if _, err := s.LandJob(ctx, id, job.Landing{Phase: phase, Attempt: attempt},
				stoppedEvent(id, "", "", "failed")); err != nil && !errors.Is(err, job.ErrNotRunning) {
				t.Fatalf("LandJob: %v", err)
			}
		}
		found, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if len(found.Attempted) != 1 {
			t.Fatalf("one task recorded %d attempts: a task read twice would look like a loop",
				len(found.Attempted))
		}
	})

	t.Run("a job that went in circles puts the question to the operator", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aRunningJob(t, s)

		looped := job.Loop{
			Owner: "controller-1", Step: 2, Similarity: 0.91, To: "ask", Phase: job.PhaseAsking,
			Question: "this job made three attempts at step 2 that read the same. What should change?",
			Attempt:  &job.Attempt{Task: "task-3", Step: 2, Said: "the check is still red", Similarity: 0.91},
		}
		found, err := s.LoopJob(ctx, id, looped, loopedEvent(id))
		if err != nil {
			t.Fatalf("LoopJob: %v", err)
		}
		switch {
		case found.Phase != job.PhaseAsking:
			t.Fatalf("the job is %q, want it waiting to be told", found.Phase)
		case found.LoopedStep != 2:
			t.Fatalf("the job says it looped on step %d, want 2", found.LoopedStep)
		case found.EscalatedTo != "ask":
			t.Fatalf("the job says it escalated to %q, want ask", found.EscalatedTo)
		case found.Question != looped.Question:
			t.Fatalf("the question reads back as %q", found.Question)
		case found.Session == "":
			t.Fatal("a job that asked left its conversation, and the person answering answers into it")
		case found.LeaseOwner != "":
			t.Fatalf("the job is still held by %q, and nothing but an answer moves it", found.LeaseOwner)
		}
	})

	t.Run("a job handed to another role goes back to pending without its conversation", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aRunningJob(t, s)

		found, err := s.LoopJob(ctx, id, job.Loop{
			Owner: "controller-1", Step: 1, Similarity: 0.88, To: "role:architect",
			Phase: job.PhasePending, Handed: true,
			Attempt: &job.Attempt{Task: "task-3", Step: 1, Said: "the check is still red", Similarity: 0.88},
		}, loopedEvent(id))
		if err != nil {
			t.Fatalf("LoopJob: %v", err)
		}
		switch {
		case found.Phase != job.PhasePending:
			t.Fatalf("the job is %q, want it waiting to be started again", found.Phase)
		case found.EscalatedTo != "role:architect":
			t.Fatalf("the job says it escalated to %q", found.EscalatedTo)
		case found.Session != "":
			t.Fatalf("the job still names %q, and a role is read only when a session is born",
				found.Session)
		case found.Reason != "":
			t.Fatalf("the job carries the reason %q while pending, which reads as one the machine is "+
				"holding back for want of room", found.Reason)
		case found.LeaseOwner != "":
			t.Fatalf("the job is still held by %q", found.LeaseOwner)
		case len(found.Attempted) != 1:
			t.Fatalf("the loop wrote %d attempts, want the one that closed it", len(found.Attempted))
		}
	})

	t.Run("a job that had escalated already is stopped rather than escalated again", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aRunningJob(t, s)

		if _, err := s.LoopJob(ctx, id, job.Loop{
			Owner: "controller-1", Step: 1, To: "role:architect", Phase: job.PhasePending, Handed: true,
			Attempt: &job.Attempt{Task: "task-3", Step: 1, Said: "still red"},
		}, loopedEvent(id)); err != nil {
			t.Fatalf("LoopJob: %v", err)
		}
		if _, err := s.StartJob(ctx, id, aLease("controller-1"), []*job.Event{startedEvent(id, "", "")}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}
		found, err := s.LoopJob(ctx, id, job.Loop{
			Owner: "controller-1", Step: 1, Phase: job.PhaseStopped,
			Reason:  "this job went in circles again after it was already escalated",
			Attempt: &job.Attempt{Task: "task-6", Step: 1, Said: "still red"},
		}, loopedEvent(id))
		if err != nil {
			t.Fatalf("LoopJob: %v", err)
		}
		switch {
		case found.Phase != job.PhaseStopped:
			t.Fatalf("the job is %q, want it stopped", found.Phase)
		case found.EscalatedTo != "role:architect":
			t.Fatalf("the job says it escalated to %q, and the route it took the first time is what a "+
				"reader needs to see why it stopped", found.EscalatedTo)
		case found.FinishedAt == nil:
			t.Fatal("the job stopped and says nothing about when")
		case len(found.Attempted) != 2:
			t.Fatalf("the job records %d attempts, want both", len(found.Attempted))
		}
	})

	// What a person last told this job belongs to the attempt that has just ended. A handed session
	// given an answer to somebody else's question reads it as its instruction, and a person reading a
	// new question with an old answer under it takes the one for the other.
	t.Run("a job that goes in circles forgets what it was last told", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aRunningJob(t, s)
		if _, err := s.AskJob(ctx, id, "which store?", loopedEvent(id)); err != nil {
			t.Fatalf("AskJob: %v", err)
		}
		if _, err := s.AnswerJob(ctx, id, "the on demand one", loopedEvent(id)); err != nil {
			t.Fatalf("AnswerJob: %v", err)
		}
		if _, err := s.StartJob(ctx, id, aLease("controller-1"), []*job.Event{startedEvent(id, "", "")}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}

		found, err := s.LoopJob(ctx, id, job.Loop{
			Owner: "controller-1", Step: 1, To: "role:architect", Phase: job.PhasePending, Handed: true,
			Attempt: &job.Attempt{Task: "task-4", Step: 1, Said: "still red"},
		}, loopedEvent(id))
		if err != nil {
			t.Fatalf("LoopJob: %v", err)
		}
		if found.Told != "" {
			t.Fatalf("the job still carries %q, which answered a question the attempt that just ended "+
				"asked", found.Told)
		}
	})

	// The compare and set. A controller that lost the row must not stop somebody else's job, and a
	// job that has already ended must not be moved by a loop arriving late.
	t.Run("a loop is refused for a controller that does not hold the job", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aRunningJob(t, s)

		_, err := s.LoopJob(ctx, id, job.Loop{
			Owner: "controller-2", Step: 1, To: "ask", Phase: job.PhaseAsking, Question: "what now?",
			Attempt: &job.Attempt{Task: "task-3", Step: 1, Said: "still red"},
		}, loopedEvent(id))
		if !errors.Is(err, job.ErrHeld) {
			t.Fatalf("a loop written by a controller that does not hold the job answered %v, want ErrHeld", err)
		}
		found, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.Phase != job.PhaseRunning || len(found.Attempted) != 0 {
			t.Fatalf("the job is %q with %d attempts, and neither should have moved",
				found.Phase, len(found.Attempted))
		}
	})

	t.Run("a loop is refused against a job that is not running", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")

		_, err := s.LoopJob(ctx, id, job.Loop{
			Owner: "controller-1", Step: 1, To: "ask", Phase: job.PhaseAsking, Question: "what now?",
			Attempt: &job.Attempt{Task: "task-1", Step: 1, Said: "still red"},
		}, loopedEvent(id))
		if !errors.Is(err, job.ErrNotRunning) {
			t.Fatalf("a loop against a pending job answered %v, want ErrNotRunning", err)
		}
	})

	// The route is a declared field like any other, and a job that loses it on the way into the store
	// escalates by asking rather than by what its author decided.
	t.Run("the route a job declared survives the write", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		workspace, project := aProject(t, s)
		id := jobShaped(t, s, workspace, project, "sort the listing", func(one *job.Job) {
			one.Escalation = "role:architect"
		})

		found, err := open(t).GetJob(context.Background(), id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.Escalation != "role:architect" {
			t.Fatalf("the route reads back as %q, want role:architect", found.Escalation)
		}
	})
}

func loopedEvent(id string) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventLooped, Job: id,
		Detail: "three attempts the system could not tell apart", OccurredAt: time.Now().UTC(),
	}
}
