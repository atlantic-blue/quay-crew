package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/store"
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
			ID: store.NewID(), Job: root, Root: root, Workspace: workspace, Project: project,
			Text: "the workspace has no secrets", OccurredAt: at,
		}
		if err := s.RecordSteer(ctx, steer, []string{root}); err != nil {
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
		if listed[0].Job != root || listed[0].Root != root {
			t.Fatalf("the steer landed on %q under %q", listed[0].Job, listed[0].Root)
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

	t.Run("a steer on a child counts on the child and on the job at the top", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		root := declaredJob(t, s, workspace, project, "build the transcripts page")
		child := childJob(t, s, workspace, project, root, "fetch the captions")
		beside := declaredJob(t, s, workspace, project, "a job of its own")

		steer := &job.Steer{
			ID: store.NewID(), Job: child, Root: root, Workspace: workspace, Project: project,
			Text: "it chose a store that bills while idle", OccurredAt: time.Now().UTC(),
		}
		if err := s.RecordSteer(ctx, steer, []string{child, root}); err != nil {
			t.Fatalf("RecordSteer: %v", err)
		}

		for _, counted := range []struct {
			name string
			id   string
			want int
		}{
			{name: "the child it landed on", id: child, want: 1},
			{name: "the job at the top", id: root, want: 1},
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

		// Read against the job at the top, because the count is the whole tree's and a reader holding
		// the root should not have to walk the children to find what landed on them.
		listed, err := s.ListSteers(ctx, root)
		if err != nil {
			t.Fatalf("ListSteers: %v", err)
		}
		if len(listed) != 1 || listed[0].Job != child {
			t.Fatalf("the job at the top reads back %d steers, and the first landed on %q", len(listed), listed[0].Job)
		}
	})

	t.Run("steers read back oldest first, whichever order they were written in", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		root := declaredJob(t, s, workspace, project, "build the transcripts page")

		at := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
		later := &job.Steer{
			ID: store.NewID(), Job: root, Root: root, Workspace: workspace, Project: project,
			Text: "second", OccurredAt: at.Add(time.Minute),
		}
		if err := s.RecordSteer(ctx, later, []string{root}); err != nil {
			t.Fatalf("RecordSteer: %v", err)
		}
		earlier := &job.Steer{
			ID: store.NewID(), Job: root, Root: root, Workspace: workspace, Project: project,
			Text: "first", OccurredAt: at,
		}
		if err := s.RecordSteer(ctx, earlier, []string{root}); err != nil {
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

// childJob writes one job under another and answers with its identifier.
func childJob(t *testing.T, s store.Store, workspace, project, parent, title string) string {
	t.Helper()
	declared := &job.Job{
		ID: store.NewID(), Workspace: workspace, Project: project,
		Title: title, Brief: "do it", Parent: parent, Depth: 1,
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
