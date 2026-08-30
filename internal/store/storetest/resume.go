package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/store"
)

// runJobResumeConformance holds both stores to what continuing a job means.
//
// The compare and sets are the whole of it. A step is recorded only against a job somebody is doing,
// and the same step twice leaves one row. A resume applies only to a job that failed, in the same
// statement, so continuing twice leaves one attempt rather than two tasks against one conversation.
// A refusal applies to exactly those rows too, and lands the job in stopped, which nothing continues.
//
// Neither store may be the lenient one. The lenient one would send a second task for work that is
// already going, which is the bill this whole behaviour exists to stop paying.
func runJobResumeConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a running job records what it finished, in order, once each", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aRunningJob(t, s)

		for _, said := range []string{"read the issue", "cut the worktree", "read the issue"} {
			if _, err := s.RecordJobStep(ctx, id, said, steppedEvent(t, s, id, said)); err != nil {
				t.Fatalf("RecordJobStep(%q): %v", said, err)
			}
		}

		found, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if len(found.Steps) != 2 {
			t.Fatalf("the job records %d steps, want 2: %v", len(found.Steps), summaries(found.Steps))
		}
		for at, want := range []string{"read the issue", "cut the worktree"} {
			if found.Steps[at].Summary != want {
				t.Fatalf("the steps read %v, want them in the order they were finished", summaries(found.Steps))
			}
			if found.Steps[at].Seq != at+1 {
				t.Fatalf("step %d is numbered %d, want %d", at+1, found.Steps[at].Seq, at+1)
			}
			if found.Steps[at].FinishedAt.IsZero() {
				t.Fatalf("step %q says nothing about when it was finished", want)
			}
		}
	})

	// A step against a job nobody is doing is refused rather than written. What it would otherwise
	// record is work that no attempt is paying for.
	t.Run("a step is refused against a job nobody is running", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")

		_, err := s.RecordJobStep(ctx, id, "read the issue", steppedEvent(t, s, id, "read the issue"))
		if !errors.Is(err, job.ErrNotRunning) {
			t.Fatalf("RecordJobStep on a pending job answered %v, want ErrNotRunning", err)
		}
		found, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if len(found.Steps) != 0 {
			t.Fatalf("a pending job recorded %v", summaries(found.Steps))
		}
	})

	t.Run("a job that failed goes back to pending, keeping its session and its steps", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aFailedJob(t, s, "the model did not finish: the credential ran out")

		resumed, err := s.ResumeJob(ctx, id, resumedEvent(t, s, id))
		if err != nil {
			t.Fatalf("ResumeJob: %v", err)
		}
		if resumed.Phase != job.PhasePending {
			t.Fatalf("a job being continued is %q, want pending", resumed.Phase)
		}
		// The failure moves off the reason and onto the row as what this attempt is continuing past. A
		// pending job carrying a reason is one the system is holding back for want of a machine, and a
		// listing says "held" for it.
		if resumed.Resuming != "the model did not finish: the credential ran out" {
			t.Fatalf("the job is continuing past %q, want what it failed with", resumed.Resuming)
		}
		if resumed.Reason != "" {
			t.Fatalf("a job going again still says %q, which reads as one the machine is holding back", resumed.Reason)
		}
		// The session is the point. The worktree, the branch and the pull request are inside it, so a
		// resume that lost it would be a second attempt from nothing wearing the same identifier.
		if resumed.Session == "" {
			t.Fatal("the job being continued lost the session its work is in")
		}
		if len(resumed.Steps) != 1 {
			t.Fatalf("the job being continued carries %v, want what it finished", summaries(resumed.Steps))
		}
		// And a controller picks it up, or the resume moved a row nothing acts on.
		runnable, err := s.RunnableJob(ctx, 10)
		if err != nil {
			t.Fatalf("RunnableJob: %v", err)
		}
		if len(runnable) != 1 || runnable[0].ID != id {
			t.Fatalf("%d jobs are runnable after a resume, want the one being continued", len(runnable))
		}
	})

	// Continuing twice is the failure this guards. The second call must not put a second task into
	// the conversation the first one is already working in.
	t.Run("a job that is already going again is not continued a second time", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aFailedJob(t, s, "the sandbox went away")

		if _, err := s.ResumeJob(ctx, id, resumedEvent(t, s, id)); err != nil {
			t.Fatalf("ResumeJob: %v", err)
		}
		if _, err := s.ResumeJob(ctx, id, resumedEvent(t, s, id)); !errors.Is(err, job.ErrNotFailed) {
			t.Fatalf("a second resume answered %v, want ErrNotFailed", err)
		}
	})

	t.Run("a job that was refused is stopped, and nothing continues it", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aFailedJob(t, s, "the model did not finish")

		refused, err := s.RefuseJob(ctx, id, "the migration was wrong, this needs declaring again",
			refusedEvent(t, s, id))
		if err != nil {
			t.Fatalf("RefuseJob: %v", err)
		}
		if refused.Phase != job.PhaseStopped {
			t.Fatalf("a refused job is %q, want stopped", refused.Phase)
		}
		if refused.Reason != "the migration was wrong, this needs declaring again" {
			t.Fatalf("a refused job says %q, want what the operator decided", refused.Reason)
		}
		if refused.FinishedAt == nil {
			t.Fatal("a refused job does not say when it ended")
		}
		if _, err := s.ResumeJob(ctx, id, resumedEvent(t, s, id)); !errors.Is(err, job.ErrNotFailed) {
			t.Fatalf("a refused job was continued anyway: %v", err)
		}
		runnable, err := s.RunnableJob(ctx, 10)
		if err != nil {
			t.Fatalf("RunnableJob: %v", err)
		}
		if len(runnable) != 0 {
			t.Fatalf("a refused job is offered to be started: %d runnable", len(runnable))
		}
	})

	t.Run("a job that did not fail is neither continued nor refused", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")

		if _, err := s.ResumeJob(ctx, id, resumedEvent(t, s, id)); !errors.Is(err, job.ErrNotFailed) {
			t.Fatalf("a pending job was continued: %v", err)
		}
		if _, err := s.RefuseJob(ctx, id, "no", refusedEvent(t, s, id)); !errors.Is(err, job.ErrNotFailed) {
			t.Fatalf("a pending job was refused: %v", err)
		}
	})

	// The record is what somebody reads a week later, so which of the two answers a failure got is on
	// it, in the same transaction as the movement it describes.
	t.Run("the record says what was finished, and which answer the failure got", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aFailedJob(t, s, "the sandbox went away")

		if _, err := s.ResumeJob(ctx, id, resumedEvent(t, s, id)); err != nil {
			t.Fatalf("ResumeJob: %v", err)
		}
		events, err := s.ListJobEvents(ctx, id)
		if err != nil {
			t.Fatalf("ListJobEvents: %v", err)
		}
		kinds := eventKindsOf(events)
		if kinds[len(kinds)-1] != job.EventResumed {
			t.Fatalf("the records read %v, want the last to say the job was continued", kinds)
		}
		var stepped int
		for _, kind := range kinds {
			if kind == job.EventStepped {
				stepped++
			}
		}
		if stepped != 1 {
			t.Fatalf("the records read %v, want one saying a step was finished", kinds)
		}
	})
}

// aRunningJob declares one job and puts it in the phase a session works in.
func aRunningJob(t *testing.T, s store.Store) string {
	t.Helper()
	workspace, project := aProject(t, s)
	id := declaredJob(t, s, workspace, project, "sort the listing")
	if _, err := s.StartJob(context.Background(), id, aLease("controller-1"),
		[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	if err := s.RecordJobSession(context.Background(), id, "session-1"); err != nil {
		t.Fatalf("RecordJobSession: %v", err)
	}
	return id
}

// aFailedJob is a job that ran, recorded one step, and then failed for a reason that was not about
// the work: the shape every one of these calls answers.
func aFailedJob(t *testing.T, s store.Store, failure string) string {
	t.Helper()
	ctx := context.Background()
	id := aRunningJob(t, s)
	if _, err := s.RecordJobStep(ctx, id, "read the issue", steppedEvent(t, s, id, "read the issue")); err != nil {
		t.Fatalf("RecordJobStep: %v", err)
	}
	found, err := s.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if _, err := s.LandJob(ctx, id, job.Landing{Phase: job.PhaseFailed, Reason: failure},
		&job.Event{
			ID: store.NewID(), Kind: job.EventFailed, Job: id, Workspace: found.Workspace,
			Project: found.Project, Detail: failure, OccurredAt: time.Now().UTC(),
		}); err != nil {
		t.Fatalf("LandJob: %v", err)
	}
	return id
}

func steppedEvent(t *testing.T, s store.Store, id, summary string) *job.Event {
	t.Helper()
	return aJobEvent(t, s, id, job.EventStepped, summary)
}

func resumedEvent(t *testing.T, s store.Store, id string) *job.Event {
	t.Helper()
	return aJobEvent(t, s, id, job.EventResumed, "continued after a failure")
}

func refusedEvent(t *testing.T, s store.Store, id string) *job.Event {
	t.Helper()
	return aJobEvent(t, s, id, job.EventRefused, "refused rather than continued")
}

// aJobEvent builds one record about a job the store already holds, so the workspace and the project
// on it are the job's own.
func aJobEvent(t *testing.T, s store.Store, id, kind, detail string) *job.Event {
	t.Helper()
	found, err := s.GetJob(context.Background(), id)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	return &job.Event{
		ID: store.NewID(), Kind: kind, Job: id, Workspace: found.Workspace, Project: found.Project,
		Detail: detail, OccurredAt: time.Now().UTC(),
	}
}

func summaries(steps []job.Step) []string {
	said := make([]string, 0, len(steps))
	for _, one := range steps {
		said = append(said, one.Summary)
	}
	return said
}
