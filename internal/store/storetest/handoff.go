package storetest

import (
	"context"
	"errors"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// runJobHandoffConformance holds both stores to what handing a job over means.
//
// A session that has filled its context window writes down what it leaves behind, and the system then
// gives the rest of the job to a fresh conversation. Two writes, and the compare and sets in them are
// the whole of it: a handoff is written only against a job somebody is doing, and the movement applies
// only to a running job whose lease this controller holds.
//
// Neither store may be the lenient one. The lenient one would take a job away from the session that is
// still doing it, which loses the work rather than saving it.
func runJobHandoffConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	// The refusals first. A handoff on a job nobody is doing is a note about work that already ended,
	// and no fresh session is ever given one.
	t.Run("a handoff is refused against a job nobody is running", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aFailedJob(t, s, "the sandbox went away")

		_, err := s.RecordJobHandoff(ctx, id, "finish the migration", "",
			"session-1", handedOverEvent(t, s, id))
		if !errors.Is(err, job.ErrNotRunning) {
			t.Fatalf("RecordJobHandoff against a failed job: %v, want ErrNotRunning", err)
		}
		found, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if len(found.Handoffs) != 0 {
			t.Fatalf("the failed job carries %d handoffs, want none", len(found.Handoffs))
		}
	})

	t.Run("a job the store does not hold is not handed over", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aRunningJob(t, s)
		event := handedOverEvent(t, s, id)

		if _, err := s.RecordJobHandoff(ctx, store.NewID(), "finish it", "", "session-1", event); err == nil {
			t.Fatal("a handoff was written against a job the store does not hold")
		}
	})

	// The movement, and the condition that keeps two controllers from fighting over one row. A
	// controller that lost the lease must not take the job away from the session another one is
	// running it in.
	t.Run("only the controller holding the job hands it over", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aRunningJob(t, s)

		_, err := s.HandOffJob(ctx, id, job.Requeue{Owner: "controller-2"}, handedOnEvent(t, s, id))
		if !errors.Is(err, job.ErrHeld) {
			t.Fatalf("HandOffJob by a controller that holds nothing: %v, want ErrHeld", err)
		}
		found, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.Phase != job.PhaseRunning || found.Session != "session-1" {
			t.Fatalf("the job is %q in session %q, want it left where it was", found.Phase, found.Session)
		}
	})

	t.Run("a job that already ended is not handed over", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aFailedJob(t, s, "the sandbox went away")

		if _, err := s.HandOffJob(ctx, id, job.Requeue{Owner: "controller-1"},
			handedOnEvent(t, s, id)); !errors.Is(err, job.ErrHeld) {
			t.Fatalf("HandOffJob against a failed job: %v, want ErrHeld", err)
		}
	})

	// The one that decides whether any of this saved anything. A job moved with nothing written down
	// leaves a fresh session paying for every discovery the last one made, and the job then reads
	// afterwards exactly like one that handed over well.
	t.Run("a job with nothing written down is not handed over", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aRunningJob(t, s)

		_, err := s.HandOffJob(ctx, id, job.Requeue{Owner: "controller-1"}, handedOnEvent(t, s, id))
		if !errors.Is(err, job.ErrNothingHandedOver) {
			t.Fatalf("HandOffJob with no handoff on the record: %v, want ErrNothingHandedOver", err)
		}
		found, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.Phase != job.PhaseRunning || found.Session != "session-1" {
			t.Fatalf("the job is %q in session %q, want it left with the session that is doing it",
				found.Phase, found.Session)
		}
	})

	// And the road itself: the handoffs are kept in the order they were written, with the conversation
	// that wrote each one, and the movement lets go of that conversation without losing anything the
	// job produced.
	t.Run("a job keeps what each session left behind, and carries on in a fresh one", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		ctx := context.Background()
		id := aRunningJobIn(t, s, "atlantic-blue/quay-crew")
		if _, err := s.RecordJobStep(ctx, id, "read the issue", "",
			steppedEvent(t, s, id, "read the issue")); err != nil {
			t.Fatalf("RecordJobStep: %v", err)
		}

		if _, err := s.RecordJobHandoff(ctx, id,
			"the index is written, the query still reads the old one: branch 539-feat-index",
			"adding the index inside the renaming migration, which deadlocks",
			"session-1", handedOverEvent(t, s, id)); err != nil {
			t.Fatalf("RecordJobHandoff: %v", err)
		}
		handed, err := s.HandOffJob(ctx, id, job.Requeue{Owner: "controller-1"}, handedOnEvent(t, s, id))
		if err != nil {
			t.Fatalf("HandOffJob: %v", err)
		}
		if handed.Phase != job.PhasePending {
			t.Fatalf("the job is %q, want pending so a controller starts it in a fresh session", handed.Phase)
		}
		// The session is let go of, which is the one place this differs from a resume. It is also what
		// says the newest handoff is still waiting to be taken up.
		if handed.Session != "" {
			t.Fatalf("the job is still in session %q, which is the conversation that is full", handed.Session)
		}
		// A pending job carrying a reason reads as one the system is holding back for want of a
		// machine, and this one is going again at once.
		if handed.Reason != "" {
			t.Fatalf("the pending job says %q", handed.Reason)
		}

		// Reopened, because a handoff that only exists in the caller's process is not one the fresh
		// session will ever be given.
		found, err := open(t).GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if len(found.Handoffs) != 1 {
			t.Fatalf("the job carries %d handoffs, want the one that was written", len(found.Handoffs))
		}
		one := found.Handoffs[0]
		if one.Seq != 1 {
			t.Fatalf("the handoff is numbered %d, want 1", one.Seq)
		}
		if one.Left != "the index is written, the query still reads the old one: branch 539-feat-index" {
			t.Fatalf("what is left reads back as %q", one.Left)
		}
		if one.Tried != "adding the index inside the renaming migration, which deadlocks" {
			t.Fatalf("what was tried reads back as %q", one.Tried)
		}
		if one.Session != "session-1" {
			t.Fatalf("the handoff names conversation %q, want the one that wrote it", one.Session)
		}
		if one.WrittenAt.IsZero() {
			t.Fatalf("the handoff says nothing about when it was written")
		}
		// Nothing the job produced is dropped. This is one job carrying on, not a second one.
		if len(found.Steps) != 1 {
			t.Fatalf("the job records %d steps, want the one it finished before it handed over",
				len(found.Steps))
		}
	})

	// A long job can reach the ceiling more than once, so the record is a list rather than a field. The
	// newest is what the session doing it now was handed.
	t.Run("a job that handed over twice keeps both, in order", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aRunningJob(t, s)

		for _, said := range []string{"finish the migration", "open the pull request"} {
			if _, err := s.RecordJobHandoff(ctx, id, said, "", "session-1",
				handedOverEvent(t, s, id)); err != nil {
				t.Fatalf("RecordJobHandoff(%q): %v", said, err)
			}
		}

		found, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if len(found.Handoffs) != 2 {
			t.Fatalf("the job carries %d handoffs, want 2", len(found.Handoffs))
		}
		latest, written := job.Latest(found.Handoffs)
		if !written || latest.Left != "open the pull request" || latest.Seq != 2 {
			t.Fatalf("the newest handoff is %+v, want the second one", latest)
		}
	})
}

func handedOverEvent(t *testing.T, s store.Store, id string) *job.Event {
	t.Helper()
	return aJobEvent(t, s, id, job.EventHandedOver, "at the context ceiling")
}

func handedOnEvent(t *testing.T, s store.Store, id string) *job.Event {
	t.Helper()
	return aJobEvent(t, s, id, job.EventHandedOn, "the rest goes to a fresh session")
}
