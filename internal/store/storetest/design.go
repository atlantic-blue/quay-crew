package storetest

import (
	"context"
	"errors"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// theList is a list of verticals in the shape the system keeps one, used by every case below and by
// the stage suite next door.
const theList = "Vertical 1: a person pastes a link on the command line and gets the text back\n" +
	"Shown 1: the transcript prints in the terminal for a link the person chooses\n" +
	"Vertical 2: a person opens the same transcript in a browser and shares the address\n" +
	"Shown 2: the page renders that transcript at an address the person can send to somebody"

// theListQuestion is what a person is asked about that list.
const theListQuestion = "Here is what it would build. Does this list get that sentence?"

// runJobDesignConformance holds both stores to what a list and its acceptance mean.
//
// The pair of compare and set movements the plan is already held to, one stage earlier, and neither
// store may be the lenient one. A store that let a job put a list up while it was not running would
// put a list from a session nobody is paying for. A store that took a second acceptance would start
// the planning twice on one list.
//
// It reads every case back through GetJob rather than trusting what the write answered with. A column
// written and never selected, or selected and never scanned, reads back empty in the real engine
// while the memory store passes, and the job then plans with no list at all.
func runJobDesignConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a running job puts its list up, and the row carries it unaccepted", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "the transcript page")
		if _, err := s.StartJob(ctx, id, aLease("controller-1"),
			[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}

		asking, err := s.ProposeJobDesign(ctx, id, theList, theListQuestion,
			askedEvent(id, workspace, project, theListQuestion))
		if err != nil {
			t.Fatalf("ProposeJobDesign: %v", err)
		}
		if asking.Phase != job.PhaseAsking || asking.Design != theList ||
			asking.Question != theListQuestion {
			t.Fatalf("the row is %q with a list of %q and the question %q",
				asking.Phase, asking.Design, asking.Question)
		}
		if asking.DesignAccepted {
			t.Fatalf("the list reads as accepted, and nobody accepted it")
		}
		// Nobody holds it, for the reason nobody holds any asking job: nothing comes back until a
		// person answers.
		if asking.LeaseOwner != "" || asking.LeaseUntil != nil {
			t.Fatalf("a job waiting to be answered is still held by %q", asking.LeaseOwner)
		}
		// And it reads back the same, which is what says the columns are written rather than kept in
		// whatever the call happened to answer with. This is the check the memory store cannot fail
		// and the real engine can: a column written and never read reads back empty here.
		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if kept.Design != theList || kept.DesignAccepted {
			t.Fatalf("the list reads back as %q, accepted %t", kept.Design, kept.DesignAccepted)
		}
	})

	t.Run("a job that is not running puts up nothing", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "the transcript page")

		_, err := s.ProposeJobDesign(ctx, id, theList, theListQuestion,
			askedEvent(id, workspace, project, theListQuestion))
		if !errors.Is(err, job.ErrNotRunning) {
			t.Fatalf("a pending job put its list up: %v", err)
		}
		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if kept.Design != "" || kept.Phase != job.PhasePending {
			t.Fatalf("the row moved to %q carrying %q", kept.Phase, kept.Design)
		}
	})

	t.Run("an acceptance starts the planning, and a second one changes nothing", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingToAcceptTheList(t, s, workspace, project)

		accepted, err := s.AcceptJobDesign(ctx, id, toldEvent(id, workspace, project))
		if err != nil {
			t.Fatalf("AcceptJobDesign: %v", err)
		}
		if accepted.Phase != job.PhasePending || !accepted.DesignAccepted {
			t.Fatalf("the accepted job is %q, accepted %t", accepted.Phase, accepted.DesignAccepted)
		}
		// What it was told is cleared, the way an approval clears it. An acceptance is not an
		// instruction to anybody: what the session is given next is the list and the ask for a plan.
		if accepted.Told != "" {
			t.Fatalf("an accepted job carries %q as the thing it was told", accepted.Told)
		}
		if accepted.Design != theList {
			t.Fatalf("the list changed to %q when it was accepted", accepted.Design)
		}
		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if !kept.DesignAccepted || kept.Design != theList {
			t.Fatalf("the list reads back as %q, accepted %t", kept.Design, kept.DesignAccepted)
		}

		// A second acceptance moves nothing. By then the job is pending and a controller is about to
		// start it, and a second one would start the planning twice on one list.
		if _, err := s.AcceptJobDesign(ctx, id,
			toldEvent(id, workspace, project)); !errors.Is(err, job.ErrNotAsking) {
			t.Fatalf("a list was accepted twice: %v", err)
		}
		again, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if again.Phase != job.PhasePending {
			t.Fatalf("the second acceptance moved the job to %q", again.Phase)
		}
	})

	t.Run("an answer that is not the acceptance leaves the list unaccepted", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingToAcceptTheList(t, s, workspace, project)

		// The ordinary road a correction takes: the job goes back to pending carrying what the person
		// said, and the session writes the list again from it.
		sent, err := s.AnswerJob(ctx, id, "the browser one is not needed",
			toldEvent(id, workspace, project))
		if err != nil {
			t.Fatalf("AnswerJob: %v", err)
		}
		if sent.Phase != job.PhasePending || sent.DesignAccepted {
			t.Fatalf("a list somebody sent back is %q, accepted %t", sent.Phase, sent.DesignAccepted)
		}
		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if kept.Told != "the browser one is not needed" || kept.DesignAccepted {
			t.Fatalf("the row was told %q, accepted %t", kept.Told, kept.DesignAccepted)
		}
	})

	t.Run("a job nobody asked accepts nothing", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "the transcript page")

		_, err := s.AcceptJobDesign(ctx, id, toldEvent(id, workspace, project))
		if !errors.Is(err, job.ErrNotAsking) {
			t.Fatalf("a pending job took an acceptance: %v", err)
		}
	})
}

// waitingToAcceptTheList is a job that has answered what it understood, proposed its list, and is
// waiting for a person to accept it.
func waitingToAcceptTheList(t *testing.T, s store.Store, workspace, project string) string {
	t.Helper()
	ctx := context.Background()
	id := jobWithASentence(t, s, workspace, project, "")
	if _, err := s.StartJob(ctx, id, aLease("controller-1"),
		[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	if _, err := s.ProposeJobIdeation(ctx, id, theReading, theReadingQuestion,
		askedEvent(id, workspace, project, theReadingQuestion)); err != nil {
		t.Fatalf("ProposeJobIdeation: %v", err)
	}
	if _, err := s.AnswerJobIdeation(ctx, id, "1: on the command line first",
		toldEvent(id, workspace, project)); err != nil {
		t.Fatalf("AnswerJobIdeation: %v", err)
	}
	if _, err := s.StartJob(ctx, id, aLease("controller-1"),
		[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	if _, err := s.ProposeJobDesign(ctx, id, theList, theListQuestion,
		askedEvent(id, workspace, project, theListQuestion)); err != nil {
		t.Fatalf("ProposeJobDesign: %v", err)
	}
	return id
}

// theReading and theReadingQuestion are a reading of the work and the question put with it, so a job
// can be walked to the design gate the way the system walks one there.
const theReading = "Understood: a page that takes a link and gives back the text\n" +
	"Not: a page that takes an identifier\n" +
	"Told: the person pastes a link\n" +
	"Assumed: the transcript is already stored\n" +
	"Unknown: which surface this is read on\n" +
	"Confidence: fairly sure of the shape\n" +
	"Question 1: which surface does a person read this on"

const theReadingQuestion = "Here is what it understands the work to be."
