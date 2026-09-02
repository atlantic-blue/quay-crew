package storetest

import (
	"context"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// theBuiltWithPictures is the record of a job's verticals being built, with the picture of each one
// running and the label that says where the picture came from.
const theBuiltWithPictures = theBuild + "\n" +
	"Picture 1: paste.png\n" +
	"Taken 1: the command line after ./transcript paste, captured with tmux capture-pane\n" +
	"Picture 2: page.png\n" +
	"Taken 2: the page at http://localhost:3000, drawn with krewe render"

// runJobAcceptanceConformance holds both stores to what the acceptance stage writes.
//
// Two movements and one column, and neither store may be the lenient one. A store that let a job
// land done without the flag would leave a done job with nothing on it saying who ended it. A store
// that accepted a job twice would count one person's word as two. A store that sent an accepted job
// back would put work a person accepted back in front of the machine.
//
// Every case reads back through GetJob rather than trusting what the write answered with. This is
// the failure the column itself invites: accepted written and never selected, or selected and never
// scanned, reads back false in the real engine while the memory store passes, and the job then stops
// on its own acceptance gate for ever.
func runJobAcceptanceConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a person's word lands the job and nothing else does", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := heldForAcceptance(t, s, workspace, project)

		// Answered, which is what puts the row back to pending carrying what the person said. Nothing
		// has landed yet: the answer is on the record and the job is still nobody's word.
		if _, err := s.AnswerJob(ctx, id, "yes", toldEvent(id, workspace, project)); err != nil {
			t.Fatalf("AnswerJob: %v", err)
		}
		answered, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if answered.Accepted {
			t.Fatal("answering a job accepted it before anything read the answer")
		}

		got, err := s.AcceptJob(ctx, id, "yes", acceptedEvent(id, workspace, project))
		if err != nil {
			t.Fatalf("AcceptJob: %v", err)
		}
		if !got.Accepted {
			t.Fatalf("the job a person accepted reads %q, accepted %v", got.Phase, got.Accepted)
		}

		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		// The column is the whole of this case. Read back through the ordinary query rather than off
		// what the write returned, because a column the real engine never selects reads back false here
		// and true in memory.
		if !kept.Accepted {
			t.Fatal("the job reads back unaccepted, so the column is written and never read")
		}
		// Permission rather than an ending. The row stays pending and keeps its question and what the
		// person said, so the ordinary road picks it up and carries it to its pull request.
		if kept.Phase != job.PhasePending {
			t.Fatalf("an accepted job is %q, want pending so it can be carried to its ending", kept.Phase)
		}
		if kept.Told != "yes" || !job.AskedToAccept(kept.Question) {
			t.Fatalf("an accepted job carries told %q against question %q", kept.Told, kept.Question)
		}
		// The record of what was built, with its pictures, stays. That is what the person accepted, and
		// it is what anybody reading this job afterwards has to be able to see.
		if len(job.PicturesIn(kept.Build)) != 2 {
			t.Fatalf("the accepted job carries %d pictures", len(job.PicturesIn(kept.Build)))
		}
	})

	t.Run("one job is accepted once, however many controllers read it", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := heldForAcceptance(t, s, workspace, project)
		if _, err := s.AnswerJob(ctx, id, "yes", toldEvent(id, workspace, project)); err != nil {
			t.Fatalf("AnswerJob: %v", err)
		}
		if _, err := s.AcceptJob(ctx, id, "yes", acceptedEvent(id, workspace, project)); err != nil {
			t.Fatalf("AcceptJob: %v", err)
		}

		if _, err := s.AcceptJob(ctx, id, "yes", acceptedEvent(id, workspace, project)); err == nil {
			t.Fatal("a job was accepted twice, so one person's word counted as two")
		}
	})

	t.Run("a job nobody built cannot be accepted", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		// Waiting to build: the plan is approved, the suite is red, and nothing has been built yet, so
		// there is no picture and nothing for anybody to have looked at.
		id := waitingToBuild(t, s, workspace, project)

		if _, err := s.AcceptJob(ctx, id, "yes", acceptedEvent(id, workspace, project)); err == nil {
			t.Fatal("a job with nothing built was accepted")
		}
		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if kept.Accepted || kept.Phase == job.PhaseDone {
			t.Fatalf("the job is %q, accepted %v", kept.Phase, kept.Accepted)
		}
	})

	t.Run("an answer that is not the acceptance sends the verticals back", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := heldForAcceptance(t, s, workspace, project)
		const said = "the second picture shows an empty page, the link is not read"
		if _, err := s.AnswerJob(ctx, id, said, toldEvent(id, workspace, project)); err != nil {
			t.Fatalf("AnswerJob: %v", err)
		}

		sent, err := s.SendJobBackToBuild(ctx, id, sentBackEvent(id, workspace, project))
		if err != nil {
			t.Fatalf("SendJobBackToBuild: %v", err)
		}
		if sent.Phase != job.PhasePending {
			t.Fatalf("a job sent back is %q, want pending so the build stage takes it", sent.Phase)
		}

		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if kept.Build != "" {
			t.Fatalf("a job sent back still carries the record %q", kept.Build)
		}
		if kept.Accepted {
			t.Fatal("a job that was sent back says a person accepted it")
		}
		// What the person said stays, because that is what the next fan out is built against. A row
		// that forgot it would build the same thing again and show them the same picture.
		if kept.Told != said {
			t.Fatalf("what the person said reads back %q", kept.Told)
		}
	})

	t.Run("an accepted job is never sent back over its own acceptance", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := heldForAcceptance(t, s, workspace, project)
		if _, err := s.AnswerJob(ctx, id, "yes", toldEvent(id, workspace, project)); err != nil {
			t.Fatalf("AnswerJob: %v", err)
		}
		if _, err := s.AcceptJob(ctx, id, "yes", acceptedEvent(id, workspace, project)); err != nil {
			t.Fatalf("AcceptJob: %v", err)
		}

		if _, err := s.SendJobBackToBuild(ctx, id, sentBackEvent(id, workspace, project)); err == nil {
			t.Fatal("a job a person accepted was sent back to be built again")
		}
		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if !kept.Accepted || kept.Build == "" {
			t.Fatalf("the accepted job reads back accepted %v with build %q", kept.Accepted, kept.Build)
		}
	})

	t.Run("a job nobody accepted reads back unaccepted", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := heldForAcceptance(t, s, workspace, project)

		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		// The other direction of the same column. A store that answered true here would let every job
		// past the gate, which is the failure reading false everywhere cannot show.
		if kept.Accepted {
			t.Fatal("a job nobody answered reads back accepted")
		}
		if kept.Phase != job.PhaseAsking {
			t.Fatalf("a job held for acceptance is %q", kept.Phase)
		}
	})
}

// heldForAcceptance is a job whose verticals are built, with a picture of each one running, holding
// for a person to look at them.
func heldForAcceptance(t *testing.T, s store.Store, workspace, project string) string {
	t.Helper()
	ctx := context.Background()
	id := waitingToBuild(t, s, workspace, project)
	if _, err := s.HoldJobForAcceptance(ctx, id, theBuiltWithPictures, job.TheAcceptanceAsk,
		builtEvent(id, workspace, project),
		askedEvent(id, workspace, project, job.TheAcceptanceAsk)); err != nil {
		t.Fatalf("HoldJobForAcceptance: %v", err)
	}
	return id
}

// acceptedEvent is the record that lands with a person's acceptance, and sentBackEvent the one that
// lands when they said the value did not arrive.
func acceptedEvent(id, workspace, project string) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventAccepted, Job: id, Workspace: workspace, Project: project,
		Detail: "a person looked at 2 pictures of this job's 2 verticals and said the value arrived",
	}
}

func sentBackEvent(id, workspace, project string) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventSentBack, Job: id, Workspace: workspace, Project: project,
		Detail: "a person looked at what was built and it is not accepted",
	}
}

// A declaration carrying the flag keeps it, which is the third list the column has to reach.
//
// Two lists are read about constantly, the row a select asks for and the fields a scan fills, and a
// column missing from either is the failure this whole file exists to catch. The insert is the third
// and it is the quiet one, because nothing on the ordinary road declares a job that a person has
// already accepted: the flag is written by an update, so a column absent from the insert costs
// nothing until something hands the store a whole record. The store keeps what it is handed, and two
// stores that disagree about one field of one call is how a double comes to accept more than the
// real thing.
func runJobAcceptedSurvivesADeclaration(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a job declared as accepted reads back accepted", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		declared := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "the transcript page", Brief: "build what the design describes",
			Phase: job.PhasePending, Build: theBuiltWithPictures, Accepted: true,
		}
		if err := s.CreateJob(ctx, declared, declaredEvent(declared)); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}

		kept, err := s.GetJob(ctx, declared.ID)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if !kept.Accepted {
			t.Fatal("a job handed to the store with a person's acceptance on it reads back " +
				"unaccepted: the column is written by the update and dropped by the insert")
		}
	})
}
