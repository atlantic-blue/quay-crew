package storetest

import (
	"context"
	"errors"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// runJobPlanConformance holds both stores to what a plan and its approval mean.
//
// Two movements, each with a compare and set around it, and neither store may be the lenient one. A
// store that let a job propose a plan while it was not running would put a question about work
// nobody is doing. A store that let a plan be approved twice would start the work twice and pay for
// it twice, which is the same failure a second answer causes and the reason the answer is
// conditional too.
func runJobPlanConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	const plan = "Step 1: read the design\nStep 2: build the address that takes a link"
	const question = "Does this plan get that sentence?"

	t.Run("a running job proposes its plan, and the row carries both halves", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "the transcript page")
		if _, err := s.StartJob(ctx, id, aLease("controller-1"),
			[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}

		asking, err := s.ProposeJobPlan(ctx, id, plan, question,
			askedEvent(id, workspace, project, question))
		if err != nil {
			t.Fatalf("ProposeJobPlan: %v", err)
		}
		// One movement, so a reader never finds a job asking with no plan on it.
		if asking.Phase != job.PhaseAsking || asking.Plan != plan || asking.Question != question {
			t.Fatalf("the row is %q with plan %q and question %q",
				asking.Phase, asking.Plan, asking.Question)
		}
		if asking.PlanApproved {
			t.Fatal("a plan reads as approved before anybody answered")
		}
		// Nobody holds it, for the reason nobody holds any asking job: nothing comes back until a
		// person answers.
		if asking.LeaseOwner != "" || asking.LeaseUntil != nil {
			t.Fatalf("a job waiting for its plan is still held by %q", asking.LeaseOwner)
		}
		// And it reads back the same, which is what says the columns are written rather than kept in
		// whatever the call happened to answer with.
		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if kept.Plan != plan || kept.PlanApproved {
			t.Fatalf("the plan reads back as %q, approved %t", kept.Plan, kept.PlanApproved)
		}
	})

	t.Run("a job that is not running proposes nothing", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "the transcript page")

		_, err := s.ProposeJobPlan(ctx, id, plan, question, askedEvent(id, workspace, project, question))
		if !errors.Is(err, job.ErrNotRunning) {
			t.Fatalf("a pending job proposed a plan: %v", err)
		}
		kept, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if kept.Plan != "" || kept.Phase != job.PhasePending {
			t.Fatalf("the row moved to %q carrying %q", kept.Phase, kept.Plan)
		}
	})

	t.Run("an approval starts the work, and a second approval changes nothing", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingForItsPlan(t, s, workspace, project, plan, question)

		approved, err := s.ApproveJobPlan(ctx, id, toldEvent(id, workspace, project))
		if err != nil {
			t.Fatalf("ApproveJobPlan: %v", err)
		}
		if !approved.PlanApproved || approved.Phase != job.PhasePending {
			t.Fatalf("the approved job is %q, approved %t", approved.Phase, approved.PlanApproved)
		}
		// What it was told is cleared, which is the difference between an approval and an ordinary
		// answer: an approval is no instruction to anybody, so what the session is given is the work
		// rather than the word yes.
		if approved.Told != "" {
			t.Fatalf("an approved job carries %q as the thing it was told", approved.Told)
		}
		if approved.Plan != plan {
			t.Fatalf("the approved plan is %q", approved.Plan)
		}

		// Two people approving at once leave one approval and one task.
		if _, err := s.ApproveJobPlan(ctx, id, toldEvent(id, workspace, project)); !errors.Is(err, job.ErrNotAsking) {
			t.Fatalf("a plan was approved twice: %v", err)
		}
	})

	t.Run("an ordinary answer leaves the plan unapproved", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := waitingForItsPlan(t, s, workspace, project, plan, question)

		correction := "a reader pastes a link, so do not make them find an identifier first"
		answered, err := s.AnswerJob(ctx, id, correction, toldEvent(id, workspace, project))
		if err != nil {
			t.Fatalf("AnswerJob: %v", err)
		}
		if answered.PlanApproved {
			t.Fatal("an answer that was not an approval approved the plan")
		}
		// The job goes on rather than ending, carrying what the person said and the plan they refused,
		// which is what the session writes the next plan from.
		if answered.Phase != job.PhasePending || answered.Told != correction || answered.Plan != plan {
			t.Fatalf("the row is %q, told %q, plan %q", answered.Phase, answered.Told, answered.Plan)
		}
	})
}

// waitingForItsPlan is a job that has written its plan and is waiting for a person to approve it.
func waitingForItsPlan(t *testing.T, s store.Store, workspace, project, plan, question string) string {
	t.Helper()
	ctx := context.Background()
	id := declaredJob(t, s, workspace, project, "the transcript page")
	if _, err := s.StartJob(ctx, id, aLease("controller-1"),
		[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	if _, err := s.ProposeJobPlan(ctx, id, plan, question,
		askedEvent(id, workspace, project, question)); err != nil {
		t.Fatalf("ProposeJobPlan: %v", err)
	}
	return id
}
