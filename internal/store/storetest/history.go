package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/store"
)

// runHistoryConformance holds both stores to the history contract.
//
// A history is the read a session makes to say what the system did, so what is proved here is the
// narrowing that decides which jobs it sees: the window, its two ends, the address and the order. The
// arithmetic is not here, because neither store does any. Both filter and order, and internal/job
// adds up what they return, which is why the two cannot disagree about a number nobody could check.
//
// Nothing here writes a declaration time. The Postgres store leaves created_at to the database clock
// and the memory store takes one if it is offered, so a scenario that set its own would be testing
// one store against a behaviour the other does not have. Every window below is built from the moment
// a job actually came back with, which is the same on both.
func runHistoryConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	// declared writes one job and hands back the moment the store stamped it.
	declared := func(t *testing.T, s store.Store, workspace, project, title string) (string, time.Time) {
		t.Helper()
		ctx := context.Background()
		written := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: title, Brief: theBriefAHistoryNeverCarries, Answer: theAnswerAHistoryNeverCarries,
			Role: "implementer", Phase: job.PhaseDone, SpentTokens: 1200,
			PullRequest: "https://github.com/atlantic-blue/quay-crew/pull/531",
			Version:     1,
		}
		started := time.Now().UTC().Add(-10 * time.Minute)
		finished := started.Add(10 * time.Minute)
		written.StartedAt, written.FinishedAt = &started, &finished
		if err := s.CreateJob(ctx, written, &job.Event{
			ID: store.NewID(), Kind: job.EventDeclared, Job: written.ID,
			Workspace: workspace, Project: project, OccurredAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}
		back, err := s.GetJob(ctx, written.ID)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		return written.ID, back.CreatedAt.UTC()
	}

	// around is a window comfortably holding one moment, for the scenarios that are not about the ends.
	around := func(at time.Time) job.Window {
		return job.Window{Since: at.Add(-time.Hour), Until: at.Add(time.Hour)}
	}

	t.Run("a history holds the jobs declared inside its window and no others", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		workspace, project := aProject(t, s)

		inside, at := declared(t, s, workspace, project, "inside the window")

		history, err := open(t).JobHistory(context.Background(), job.HistoryQuery{Window: around(at)})
		if err != nil {
			t.Fatalf("JobHistory: %v", err)
		}
		if len(history) != 1 || history[0].ID != inside {
			t.Fatalf("the window holds %d jobs, want the one declared inside it", len(history))
		}

		// And a window that ended before the job was declared holds nothing.
		before, err := s.JobHistory(context.Background(), job.HistoryQuery{
			Window: job.Window{Since: at.Add(-2 * time.Hour), Until: at.Add(-time.Hour)},
		})
		if err != nil {
			t.Fatalf("JobHistory: %v", err)
		}
		if len(before) != 0 {
			t.Fatalf("a window that ended before the job was declared holds %d jobs", len(before))
		}
	})

	// The near end is included and the far end is not, so two windows laid end to end count a job
	// once. A store that closed the far end would count every boundary twice.
	t.Run("the near end of a window is included and the far end is not", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		workspace, project := aProject(t, s)

		id, at := declared(t, s, workspace, project, "on the boundary")
		ctx := context.Background()

		onTheNearEnd, err := s.JobHistory(ctx, job.HistoryQuery{
			Window: job.Window{Since: at, Until: at.Add(time.Hour)},
		})
		if err != nil {
			t.Fatalf("JobHistory: %v", err)
		}
		if len(onTheNearEnd) != 1 || onTheNearEnd[0].ID != id {
			t.Fatalf("a job on the near end of a window is left out of it")
		}

		onTheFarEnd, err := s.JobHistory(ctx, job.HistoryQuery{
			Window: job.Window{Since: at.Add(-time.Hour), Until: at},
		})
		if err != nil {
			t.Fatalf("JobHistory: %v", err)
		}
		if len(onTheFarEnd) != 0 {
			t.Fatalf("a job on the far end of a window is counted in it, so two windows laid end to " +
				"end would count it twice")
		}
	})

	t.Run("a history comes back newest first", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		workspace, project := aProject(t, s)

		_, first := declared(t, s, workspace, project, "the oldest")
		declared(t, s, workspace, project, "the middle")
		_, last := declared(t, s, workspace, project, "the newest")

		history, err := s.JobHistory(context.Background(), job.HistoryQuery{
			Window: job.Window{Since: first.Add(-time.Hour), Until: last.Add(time.Hour)},
		})
		if err != nil {
			t.Fatalf("JobHistory: %v", err)
		}
		if len(history) != 3 {
			t.Fatalf("the window holds %d jobs, want 3", len(history))
		}
		// Asserted as an order over the moments the store stamped rather than as three titles, because
		// the stamps are the store's to write and only their order is the contract.
		for i := 1; i < len(history); i++ {
			if history[i].CreatedAt.After(history[i-1].CreatedAt) {
				t.Fatalf("the history runs oldest first: %s came back before %s",
					history[i-1].CreatedAt, history[i].CreatedAt)
			}
		}
	})

	t.Run("a history narrows to one project and to one workspace", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		other, err := s.CreateProject(ctx, workspace, "the-other-project")
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}

		here, at := declared(t, s, workspace, project, "in this project")
		declared(t, s, workspace, other.GetId(), "in the other project")
		window := around(at)

		narrowed, err := s.JobHistory(ctx, job.HistoryQuery{Project: project, Window: window})
		if err != nil {
			t.Fatalf("JobHistory: %v", err)
		}
		if len(narrowed) != 1 || narrowed[0].ID != here {
			t.Fatalf("a history of one project holds %d jobs, want the 1 declared in it", len(narrowed))
		}

		// The workspace holds both projects, so a history of it holds both jobs.
		wide, err := s.JobHistory(ctx, job.HistoryQuery{Workspace: workspace, Window: window})
		if err != nil {
			t.Fatalf("JobHistory: %v", err)
		}
		if len(wide) != 2 {
			t.Fatalf("a history of the workspace holds %d jobs, want 2", len(wide))
		}
	})

	// The whole reason a digest is its own shape. A history that carried each brief and each answer
	// would cost a session the context it wanted the history in order to spend.
	t.Run("a digest carries the facts and never the prose", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		workspace, project := aProject(t, s)
		_, at := declared(t, s, workspace, project, "the job")

		history, err := s.JobHistory(context.Background(), job.HistoryQuery{Window: around(at)})
		if err != nil {
			t.Fatalf("JobHistory: %v", err)
		}
		if len(history) != 1 {
			t.Fatalf("the window holds %d jobs, want 1", len(history))
		}
		one := history[0]
		if one.Title != "the job" || one.Role != "implementer" || one.Phase != job.PhaseDone {
			t.Fatalf("the digest reads %+v", one)
		}
		// The cost, but never the steers: a declaration does not write a steer count. That column
		// is raised by a steer, so asserting it here would be asserting against a write neither
		// store makes at this point.
		if one.SpentToken != 1200 {
			t.Fatalf("the digest lost the cost: %+v", one)
		}
		if one.PullRequest == "" {
			t.Fatalf("the digest lost the pull request: %+v", one)
		}
		// The moments the arithmetic reads. A store that dropped them would report a window that did
		// no work at all.
		if one.StartedAt.IsZero() || one.FinishedAt.IsZero() {
			t.Fatalf("the digest lost the moments the job ran between: %+v", one)
		}
	})

	// A job that never started has no moments, and a store writing the zero time instead would put the
	// first of January year one in front of a reader.
	t.Run("a job that never ran comes back with no moments", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		written := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "never started", Phase: job.PhasePending, Version: 1,
		}
		if err := s.CreateJob(ctx, written, &job.Event{
			ID: store.NewID(), Kind: job.EventDeclared, Job: written.ID,
			Workspace: workspace, Project: project, OccurredAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}
		back, err := s.GetJob(ctx, written.ID)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}

		history, err := s.JobHistory(ctx, job.HistoryQuery{Window: around(back.CreatedAt.UTC())})
		if err != nil {
			t.Fatalf("JobHistory: %v", err)
		}
		if len(history) != 1 {
			t.Fatalf("the window holds %d jobs, want 1", len(history))
		}
		if !history[0].StartedAt.IsZero() || !history[0].FinishedAt.IsZero() {
			t.Fatalf("a job that never ran reads as started %s and finished %s",
				history[0].StartedAt, history[0].FinishedAt)
		}
	})

	t.Run("a window holding nothing is empty rather than an error", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		workspace, project := aProject(t, s)
		declared(t, s, workspace, project, "the job")

		history, err := s.JobHistory(context.Background(), job.HistoryQuery{
			Window: job.Window{
				Since: time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
				Until: time.Date(2020, time.January, 2, 0, 0, 0, 0, time.UTC),
			},
		})
		if err != nil {
			t.Fatalf("JobHistory: %v", err)
		}
		if len(history) != 0 {
			t.Fatalf("an empty window holds %d jobs", len(history))
		}
	})
}

// The prose a history must never carry, named once so the seed and the assertion cannot drift apart
// and quietly stop proving anything.
const (
	theBriefAHistoryNeverCarries  = "a brief nobody should have to read to know what happened"
	theAnswerAHistoryNeverCarries = "an answer nobody should have to read either"
)
