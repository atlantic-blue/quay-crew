package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// runJobTellingConformance holds both stores to what it means to tell somebody a job is waiting.
//
// This is the shape the issue warns about: a new column has to reach jobColumns and scanJob, and a
// store that misses either reads zero from Postgres while the memory store passes every test. So
// both moments are written here and read back through the ordinary read, in both tiers.
//
// The write itself is a compare and set, and neither store may be the lenient one. A store that
// wrote a second record for the same wait would turn the gap from job.asked into the time since the
// last redraw, which is a measurement of nothing.
func runJobTellingConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("the question and the telling are both moments on the row", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := asking(t, s, workspace, project, "aurora or a key value store?")

		asking, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if asking.AskedAt == nil {
			t.Fatalf("a job that asked carries no moment for the question, so no gap can be measured")
		}
		if asking.RaisedAt != nil {
			t.Fatalf("a job nobody has been told about was raised at %s", asking.RaisedAt)
		}

		written, err := s.RaiseJob(ctx, id, raisedEvent(id, workspace, project, "console"))
		if err != nil {
			t.Fatalf("RaiseJob: %v", err)
		}
		if !written {
			t.Fatalf("the first surface to name this job did not record the telling")
		}

		// Read back through the ordinary read, which is what proves the column is selected and
		// scanned rather than only written.
		told, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if told.RaisedAt == nil {
			t.Fatalf("the telling was written and reads back as nothing: the column is not selected or not scanned")
		}
		if told.AskedAt == nil {
			t.Fatalf("the question's moment reads back as nothing after the telling")
		}
		if told.RaisedAt.Before(*told.AskedAt) {
			t.Fatalf("the telling reads as older than the question: asked %s, told %s", told.AskedAt, told.RaisedAt)
		}
		// And in a listing, which selects the same columns by a different road.
		listed, err := s.ListJobs(ctx, job.Filter{Workspace: workspace})
		if err != nil {
			t.Fatalf("ListJobs: %v", err)
		}
		var found *job.Job
		for _, one := range listed {
			if one.ID == id {
				found = one
			}
		}
		if found == nil || found.RaisedAt == nil {
			t.Fatalf("a listing does not carry the telling: %+v", found)
		}
	})

	t.Run("a second surface writes no second telling", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := asking(t, s, workspace, project, "aurora or a key value store?")

		if _, err := s.RaiseJob(ctx, id, raisedEvent(id, workspace, project, "console")); err != nil {
			t.Fatalf("RaiseJob: %v", err)
		}
		written, err := s.RaiseJob(ctx, id, raisedEvent(id, workspace, project, "command line"))
		if err != nil {
			t.Fatalf("RaiseJob: %v", err)
		}
		if written {
			t.Fatalf("the second surface to draw the same wait recorded a second telling")
		}

		events, err := s.ListJobEvents(ctx, id)
		if err != nil {
			t.Fatalf("ListJobEvents: %v", err)
		}
		raised := 0
		for _, one := range events {
			if one.Kind == job.EventRaised {
				raised++
			}
		}
		if raised != 1 {
			t.Fatalf("%d records of the telling for one wait", raised)
		}
	})

	t.Run("the telling does not move when the wait started", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := asking(t, s, workspace, project, "aurora or a key value store?")

		before, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
		if _, err := s.RaiseJob(ctx, id, raisedEvent(id, workspace, project, "console")); err != nil {
			t.Fatalf("RaiseJob: %v", err)
		}
		after, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		// Every surface measures the age of a wait from this moment, so a telling that touched it
		// would reset the very age it was reporting.
		if !after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Fatalf("the telling moved when the wait started, from %s to %s", before.UpdatedAt, after.UpdatedAt)
		}
	})

	t.Run("a job put back to work is told about again when it stops again", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := asking(t, s, workspace, project, "aurora or a key value store?")

		if _, err := s.RaiseJob(ctx, id, raisedEvent(id, workspace, project, "console")); err != nil {
			t.Fatalf("RaiseJob: %v", err)
		}
		if _, err := s.AnswerJob(ctx, id, "the key value store", toldEvent(id, workspace, project)); err != nil {
			t.Fatalf("AnswerJob: %v", err)
		}
		if _, err := s.StartJob(ctx, id, aLease("controller-1"),
			[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}

		running, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if running.RaisedAt != nil {
			t.Fatalf("a job back at work still carries a telling from the wait before it")
		}
		question := "the second question, which nobody has been told about yet"
		if _, err := s.AskJob(ctx, id, question, askedEvent(id, workspace, project, question)); err != nil {
			t.Fatalf("AskJob: %v", err)
		}
		written, err := s.RaiseJob(ctx, id, raisedEvent(id, workspace, project, "console"))
		if err != nil {
			t.Fatalf("RaiseJob: %v", err)
		}
		if !written {
			t.Fatalf("the second wait was never told to anybody, because the first one had been")
		}
	})

	t.Run("a workspace says how long a wait lasts before its age is named", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, _ := aProject(t, s)

		unset, err := s.WorkspaceLimits(ctx, workspace)
		if err != nil {
			t.Fatalf("WorkspaceLimits: %v", err)
		}
		if unset.WaitingSeconds != 0 {
			t.Fatalf("a workspace nobody configured carries a wait of %d on the row, want none",
				unset.WaitingSeconds)
		}
		// Unset reads as the system's own rather than as no limit at all, because a workspace with
		// no age on any wait is a workspace where an hour reads like a second.
		if unset.Waiting() != job.DefaultWaiting {
			t.Fatalf("a workspace nobody configured waits %s, want the system's own %s",
				unset.Waiting(), job.DefaultWaiting)
		}

		if _, err := s.SetWorkspaceLimits(ctx, job.Limits{Workspace: workspace, WaitingSeconds: 300}); err != nil {
			t.Fatalf("SetWorkspaceLimits: %v", err)
		}
		held, err := s.WorkspaceLimits(ctx, workspace)
		if err != nil {
			t.Fatalf("WorkspaceLimits: %v", err)
		}
		if held.WaitingSeconds != 300 || held.Waiting() != 5*time.Minute {
			t.Fatalf("the wait reads back as %d seconds, %s", held.WaitingSeconds, held.Waiting())
		}
	})

	// Every stage that stops for a person stamps the same moment. The ideation gate arrived from one
	// branch and these two columns from another, so nothing held the two together: an ideation
	// question that moved to asking without the stamp reads as a wait nobody can measure, and the
	// gap the telling is judged on is unreadable for it.
	t.Run("a question about what the job understood is a wait like any other", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingToBeAnswered(t, s, workspace, project,
			"the work is the transcript page, and it is not the search over it",
			"is the page read by a person or by a job?")

		asked, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if asked.AskedAt == nil {
			t.Fatalf("a job asking what it understood carries no moment, so no gap can be measured")
		}
		if asked.RaisedAt != nil {
			t.Fatalf("a job nobody has been told about was raised at %s", asked.RaisedAt)
		}
		why, want, waiting := job.Waits(asked)
		if !waiting || why != job.WaitingAsking {
			t.Fatalf("an ideation question does not read as a wait: %s, %v", why, waiting)
		}
		if want != "is the page read by a person or by a job?" {
			t.Fatalf("the telling carries %q rather than the question it asked", want)
		}

		written, err := s.RaiseJob(ctx, id, raisedEvent(id, workspace, project, "console"))
		if err != nil {
			t.Fatalf("RaiseJob: %v", err)
		}
		if !written {
			t.Fatalf("the first surface to name this ideation question did not record the telling")
		}
		told, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if told.RaisedAt == nil || told.AskedAt == nil {
			t.Fatalf("the two moments do not read back: asked %v, told %v", told.AskedAt, told.RaisedAt)
		}
		if told.RaisedAt.Before(*told.AskedAt) {
			t.Fatalf("the telling reads as older than the question: asked %s, told %s",
				told.AskedAt, told.RaisedAt)
		}
	})
}

func raisedEvent(id, workspace, project, surface string) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventRaised, Job: id,
		Workspace: workspace, Project: project, Detail: surface, OccurredAt: time.Now().UTC(),
	}
}
