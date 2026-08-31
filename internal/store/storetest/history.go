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
// A history is the read a session makes to say what the crew did, so what is proved here is the
// narrowing that decides which jobs it sees: the window, the address, and the order. The arithmetic
// is not here, because neither store does any: both filter and order, and internal/job adds up what
// they return. That split is deliberate, and it is why these two implementations cannot disagree
// about a number nobody could check.
func runHistoryConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	// declaredAt writes one job into a project at a given moment, and hands back its identifier.
	declaredAt := func(t *testing.T, s store.Store, workspace, project, title string, at time.Time) string {
		t.Helper()
		written := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: title, Brief: "a brief a history never carries",
			Answer: "an answer a history never carries",
			Role:   "implementer", Phase: job.PhaseDone, SpentTokens: 1200, Steers: 1,
			PullRequest: "https://github.com/atlantic-blue/quay-crew/pull/531",
			Version:     1, CreatedAt: at, UpdatedAt: at,
		}
		started, finished := at, at.Add(10*time.Minute)
		written.StartedAt, written.FinishedAt = &started, &finished
		if err := s.CreateJob(context.Background(), written, &job.Event{
			ID: store.NewID(), Kind: job.EventDeclared, Job: written.ID,
			Workspace: workspace, Project: project, OccurredAt: at,
		}); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}
		return written.ID
	}

	day := func(n int) time.Time { return time.Date(2026, time.August, n, 9, 0, 0, 0, time.UTC) }

	t.Run("a history holds the jobs declared inside its window and no others", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		declaredAt(t, s, workspace, project, "before the window", day(20))
		inside := declaredAt(t, s, workspace, project, "inside the window", day(25))
		declaredAt(t, s, workspace, project, "after the window", day(30))

		history, err := open(t).JobHistory(ctx, job.HistoryQuery{
			Window: job.Window{Since: day(24), Until: day(26)},
		})
		if err != nil {
			t.Fatalf("JobHistory: %v", err)
		}
		if len(history) != 1 || history[0].ID != inside {
			t.Fatalf("the window holds %d jobs, want only the one declared inside it", len(history))
		}
	})

	// The near end is included and the far end is not, so two windows laid end to end count a job
	// once. A store that closed the far end would double count every boundary.
	t.Run("the near end of a window is included and the far end is not", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		workspace, project := aProject(t, s)

		near := declaredAt(t, s, workspace, project, "on the near end", day(25))
		declaredAt(t, s, workspace, project, "on the far end", day(27))

		history, err := s.JobHistory(context.Background(), job.HistoryQuery{
			Window: job.Window{Since: day(25), Until: day(27)},
		})
		if err != nil {
			t.Fatalf("JobHistory: %v", err)
		}
		if len(history) != 1 || history[0].ID != near {
			t.Fatalf("the window holds %d jobs, want only the one on the near end", len(history))
		}
	})

	t.Run("a history comes back newest first", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		workspace, project := aProject(t, s)

		declaredAt(t, s, workspace, project, "the oldest", day(21))
		declaredAt(t, s, workspace, project, "the middle", day(22))
		declaredAt(t, s, workspace, project, "the newest", day(23))

		history, err := s.JobHistory(context.Background(), job.HistoryQuery{
			Window: job.Window{Since: day(20), Until: day(24)},
		})
		if err != nil {
			t.Fatalf("JobHistory: %v", err)
		}
		if len(history) != 3 {
			t.Fatalf("the window holds %d jobs, want 3", len(history))
		}
		for i, want := range []string{"the newest", "the middle", "the oldest"} {
			if history[i].Title != want {
				t.Fatalf("the history reads %q at %d, want %q", history[i].Title, i, want)
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

		here := declaredAt(t, s, workspace, project, "in this project", day(25))
		declaredAt(t, s, workspace, other.GetId(), "in the other project", day(25))

		window := job.Window{Since: day(24), Until: day(26)}
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
		declaredAt(t, s, workspace, project, "the job", day(25))

		history, err := s.JobHistory(context.Background(), job.HistoryQuery{
			Window: job.Window{Since: day(24), Until: day(26)},
		})
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
		if one.SpentToken != 1200 || one.Steers != 1 {
			t.Fatalf("the digest lost the cost or the steers: %+v", one)
		}
		if one.PullRequest == "" {
			t.Fatalf("the digest lost the pull request: %+v", one)
		}
		// The moments the arithmetic reads. A store that dropped them would report a window that did
		// no work at all.
		if one.StartedAt.IsZero() || one.FinishedAt.IsZero() {
			t.Fatalf("the digest lost the moments the job ran between: %+v", one)
		}
		if !one.CreatedAt.Equal(day(25)) {
			t.Fatalf("the digest was declared at %s, want %s", one.CreatedAt, day(25))
		}
	})

	// A job that never started has no moments, and a store that wrote the zero time instead would put
	// the first of January year one in front of a reader.
	t.Run("a job that never ran comes back with no moments", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		written := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "never started", Phase: job.PhasePending, Version: 1,
			CreatedAt: day(25), UpdatedAt: day(25),
		}
		if err := s.CreateJob(ctx, written, &job.Event{
			ID: store.NewID(), Kind: job.EventDeclared, Job: written.ID,
			Workspace: workspace, Project: project, OccurredAt: day(25),
		}); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}

		history, err := s.JobHistory(ctx, job.HistoryQuery{
			Window: job.Window{Since: day(24), Until: day(26)},
		})
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
		declaredAt(t, s, workspace, project, "the job", day(25))

		history, err := s.JobHistory(context.Background(), job.HistoryQuery{
			Window: job.Window{Since: day(1), Until: day(2)},
		})
		if err != nil {
			t.Fatalf("JobHistory: %v", err)
		}
		if len(history) != 0 {
			t.Fatalf("an empty window holds %d jobs", len(history))
		}
	})
}
