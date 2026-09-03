package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// runSteerConformance holds both stores to the steer contract.
//
// A steer is the score of a job, so what is proved here is that the mark and the count cannot
// disagree: the row is written and every job it counts against goes up in the same transaction, and
// a whole tree's marks read back in the order they were made.
func runSteerConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a steer is written, counted on the jobs it belongs to, and outlives the caller", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		root := declaredJob(t, s, workspace, project, "build the transcripts page")

		at := time.Now().UTC().Truncate(time.Millisecond)
		steer := &job.Steer{
			ID: store.NewID(), Job: root, Workspace: workspace, Project: project,
			Text: "the workspace has no secrets", OccurredAt: at,
		}
		if err := s.RecordSteer(ctx, steer); err != nil {
			t.Fatalf("RecordSteer: %v", err)
		}

		// Reopened, because a score that only exists in the caller's process is not a score anybody
		// can compare next month.
		reopened := open(t)
		listed, err := reopened.ListSteers(ctx, root)
		if err != nil {
			t.Fatalf("ListSteers: %v", err)
		}
		if len(listed) != 1 {
			t.Fatalf("the job reads back %d steers, want 1", len(listed))
		}
		if listed[0].Text != "the workspace has no secrets" {
			t.Fatalf("the steer reads back as %q", listed[0].Text)
		}
		if listed[0].Job != root {
			t.Fatalf("the steer landed on %q, want the job it was made against", listed[0].Job)
		}
		if !listed[0].OccurredAt.Equal(at) {
			t.Fatalf("the steer happened at %v, want %v", listed[0].OccurredAt, at)
		}
		found, err := reopened.GetJob(ctx, root)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.Steers != 1 {
			t.Fatalf("the job counts %d steers, want 1", found.Steers)
		}
	})

	t.Run("a steer counts on the job it landed on and on no other", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		asked := declaredJob(t, s, workspace, project, "build the transcripts page")
		caused := causedJob(t, s, workspace, project, asked, "fetch the captions")
		beside := declaredJob(t, s, workspace, project, "a job of its own")

		steer := &job.Steer{
			ID: store.NewID(), Job: caused, Workspace: workspace, Project: project,
			Text: "it chose a store that bills while idle", OccurredAt: time.Now().UTC(),
		}
		if err := s.RecordSteer(ctx, steer); err != nil {
			t.Fatalf("RecordSteer: %v", err)
		}

		// The job it landed on, and nothing else. The job whose session declared that one is a job in
		// its own right, so a mark against one is not a mark against it.
		for _, counted := range []struct {
			name string
			id   string
			want int
		}{
			{name: "the job it landed on", id: caused, want: 1},
			{name: "the job that caused it", id: asked, want: 0},
			{name: "a job beside it", id: beside, want: 0},
		} {
			found, err := s.GetJob(ctx, counted.id)
			if err != nil {
				t.Fatalf("GetJob %s: %v", counted.name, err)
			}
			if found.Steers != counted.want {
				t.Fatalf("%s counts %d steers, want %d", counted.name, found.Steers, counted.want)
			}
		}

		listed, err := s.ListSteers(ctx, caused)
		if err != nil {
			t.Fatalf("ListSteers: %v", err)
		}
		if len(listed) != 1 || listed[0].Job != caused {
			t.Fatalf("the job reads back %d steers, and the first landed on %q", len(listed), listed[0].Job)
		}
		if listed, err = s.ListSteers(ctx, asked); err != nil || len(listed) != 0 {
			t.Fatalf("the job that caused it reads back %d steers, want none", len(listed))
		}
	})

	t.Run("steers read back oldest first, whichever order they were written in", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		root := declaredJob(t, s, workspace, project, "build the transcripts page")

		at := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
		later := &job.Steer{
			ID: store.NewID(), Job: root, Workspace: workspace, Project: project,
			Text: "second", OccurredAt: at.Add(time.Minute),
		}
		if err := s.RecordSteer(ctx, later); err != nil {
			t.Fatalf("RecordSteer: %v", err)
		}
		earlier := &job.Steer{
			ID: store.NewID(), Job: root, Workspace: workspace, Project: project,
			Text: "first", OccurredAt: at,
		}
		if err := s.RecordSteer(ctx, earlier); err != nil {
			t.Fatalf("RecordSteer: %v", err)
		}

		listed, err := s.ListSteers(ctx, root)
		if err != nil {
			t.Fatalf("ListSteers: %v", err)
		}
		if len(listed) != 2 || listed[0].Text != "first" || listed[1].Text != "second" {
			t.Fatalf("the steers read back in the wrong order: %v", texts(listed))
		}
		found, err := s.GetJob(ctx, root)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.Steers != 2 {
			t.Fatalf("the job counts %d steers, want 2", found.Steers)
		}
	})

	t.Run("a job nobody steered counts none and reads back nothing", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		root := declaredJob(t, s, workspace, project, "build the transcripts page")

		listed, err := s.ListSteers(ctx, root)
		if err != nil {
			t.Fatalf("ListSteers: %v", err)
		}
		if len(listed) != 0 {
			t.Fatalf("a job nobody steered reads back %d steers", len(listed))
		}
		found, err := s.GetJob(ctx, root)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.Steers != 0 {
			t.Fatalf("a job nobody steered counts %d steers", found.Steers)
		}
	})
}

// causedJob writes one job the session running another declared, and answers with its identifier.
func causedJob(t *testing.T, s store.Store, workspace, project, cause, title string) string {
	t.Helper()
	declared := &job.Job{
		ID: store.NewID(), Workspace: workspace, Project: project,
		Title: title, Brief: "do it", Cause: cause,
		Version: 1, Phase: job.PhasePending,
	}
	if err := s.CreateJob(context.Background(), declared, declaredEvent(declared)); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	return declared.ID
}

func texts(steers []*job.Steer) []string {
	said := make([]string, 0, len(steers))
	for _, one := range steers {
		said = append(said, one.Text)
	}
	return said
}
